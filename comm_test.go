package comm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/costa92/llm-agent-comm/internal/testenv"
)

// --- envelope -------------------------------------------------------------

func TestRPCError_AsError(t *testing.T) {
	var e error = &RPCError{Code: -1, Message: "boom"}
	if e.Error() != "boom" {
		t.Errorf("Error() = %q, want boom", e.Error())
	}
	var nilErr *RPCError
	if nilErr.Error() != "<nil rpc error>" {
		t.Error("nil RPCError should produce safe sentinel string")
	}
}

// --- in-memory transport --------------------------------------------------

func TestInMemoryTransport_RoundTrip(t *testing.T) {
	called := atomicBool{}
	tr := NewInMemoryTransport(func(_ context.Context, env Envelope) (Response, error) {
		called.set(true)
		return Response{ID: env.ID, Result: json.RawMessage(`"ok"`)}, nil
	})
	r, err := tr.Call(context.Background(), Envelope{ID: "1", Method: "ping"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !called.get() {
		t.Error("handler not invoked")
	}
	if string(r.Result) != `"ok"` {
		t.Errorf("Result = %q", string(r.Result))
	}
}

func TestInMemoryTransport_NilHandlerReturnsRPCError(t *testing.T) {
	tr := NewInMemoryTransport(nil)
	r, err := tr.Call(context.Background(), Envelope{ID: "1"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if r.Error == nil {
		t.Fatal("expected RPCError when handler is nil")
	}
}

func TestInMemoryTransport_ClosedRejects(t *testing.T) {
	tr := NewInMemoryTransport(func(_ context.Context, _ Envelope) (Response, error) {
		return Response{}, nil
	})
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := tr.Call(context.Background(), Envelope{ID: "1"})
	if !errors.Is(err, ErrTransportClosed) {
		t.Errorf("err = %v, want ErrTransportClosed", err)
	}
}

// --- HTTP transport -------------------------------------------------------

func TestHTTPTransport_RoundTrip(t *testing.T) {
	if err := testenv.CanStartHTTPServer(); err != nil {
		t.Skipf("local HTTP server unavailable in this environment: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env Envelope
		_ = json.NewDecoder(r.Body).Decode(&env)
		resp := Response{ID: env.ID, Result: json.RawMessage(`{"echoed":"` + env.Method + `"}`)}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, HTTPTransportOptions{Timeout: 2 * time.Second})
	r, err := tr.Call(context.Background(), Envelope{ID: "abc", Method: "test"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(string(r.Result), "test") {
		t.Errorf("Result = %s", string(r.Result))
	}
}

func TestHTTPTransport_5xxBecomesServerError(t *testing.T) {
	if err := testenv.CanStartHTTPServer(); err != nil {
		t.Skipf("local HTTP server unavailable in this environment: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	tr := NewHTTPTransport(srv.URL, HTTPTransportOptions{Timeout: 2 * time.Second})
	_, err := tr.Call(context.Background(), Envelope{ID: "1"})
	if !errors.Is(err, ErrServerError) {
		t.Errorf("err = %v, want ErrServerError", err)
	}
}

func TestHTTPTransport_ContextCancel(t *testing.T) {
	if err := testenv.CanStartHTTPServer(); err != nil {
		t.Skipf("local HTTP server unavailable in this environment: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	tr := NewHTTPTransport(srv.URL, HTTPTransportOptions{Timeout: 5 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := tr.Call(ctx, Envelope{ID: "1"})
	if err == nil {
		t.Fatal("expected timeout / cancel error")
	}
}

func TestHTTPTransport_HeadersForwarded(t *testing.T) {
	if err := testenv.CanStartHTTPServer(); err != nil {
		t.Skipf("local HTTP server unavailable in this environment: %v", err)
	}
	gotHeader := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Trace-Id")
		_ = json.NewEncoder(w).Encode(Response{ID: "1", Result: json.RawMessage(`{}`)})
	}))
	defer srv.Close()
	tr := NewHTTPTransport(srv.URL, HTTPTransportOptions{
		Headers: map[string]string{"X-Trace-Id": "trace-from-opts"},
	})
	_, _ = tr.Call(context.Background(), Envelope{ID: "1", Metadata: map[string]string{"X-Trace-Id": "trace-from-meta"}})
	// Metadata wins (set after opts.Headers).
	if gotHeader != "trace-from-meta" {
		t.Errorf("X-Trace-Id = %q, want trace-from-meta", gotHeader)
	}
}

func TestHTTPTransport_ClosedRejects(t *testing.T) {
	tr := NewHTTPTransport("http://localhost:0", HTTPTransportOptions{})
	_ = tr.Close()
	_, err := tr.Call(context.Background(), Envelope{ID: "1"})
	if !errors.Is(err, ErrTransportClosed) {
		t.Errorf("err = %v, want ErrTransportClosed", err)
	}
}

// --- Stdio transport (POSIX-only) -----------------------------------------

func TestStdioTransport_EchoLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdio test relies on POSIX cat")
	}
	if _, err := os.Stat("/bin/cat"); err != nil {
		t.Skip("/bin/cat not available")
	}
	// `cat` echoes stdin → stdout line by line; we send a JSON Response
	// shape so unmarshal succeeds.
	tr, err := NewStdioTransport("cat", nil, StdioTransportOptions{})
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer tr.Close()
	_, err = tr.Call(context.Background(), Envelope{
		ID:     "abc",
		Method: "anything",
		Params: json.RawMessage(`{}`),
	})
	// Cat will just echo our request bytes — that's not a valid Response
	// shape (Method/Params don't fit). We expect a parse error, but the
	// write+read round-trip should have succeeded.
	if err == nil {
		t.Log("stdio round-trip + parse worked (cat happened to echo a parsable shape)")
	} else if !strings.Contains(err.Error(), "unmarshal") && !strings.Contains(err.Error(), "comm/stdio") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStdioTransport_Closed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdio test relies on POSIX cat")
	}
	if _, err := os.Stat("/bin/cat"); err != nil {
		t.Skip("/bin/cat not available")
	}
	tr, err := NewStdioTransport("cat", nil, StdioTransportOptions{})
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	_ = tr.Close()
	_, err = tr.Call(context.Background(), Envelope{ID: "1"})
	if !errors.Is(err, ErrTransportClosed) {
		t.Errorf("err = %v, want ErrTransportClosed", err)
	}
}

// --- helpers --------------------------------------------------------------

type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicBool) set(v bool) {
	a.mu.Lock()
	a.v = v
	a.mu.Unlock()
}
func (a *atomicBool) get() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

// Verify Transport interface satisfaction at compile time.
var (
	_ Transport = (*InMemoryTransport)(nil)
	_ Transport = (*HTTPTransport)(nil)
	_ Transport = (*StdioTransport)(nil)
)
