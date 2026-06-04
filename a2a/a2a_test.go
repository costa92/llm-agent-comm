package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/costa92/llm-agent-comm/internal/testenv"
)

// helper: stand up a server with one happy skill + one error skill.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	if err := testenv.CanStartHTTPServer(); err != nil {
		t.Skipf("local HTTP server unavailable in this environment: %v", err)
	}
	s := NewServer("test", "test server")
	s.RegisterSkill("echo", "echoes input", func(_ context.Context, in json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"echoed":` + string(in) + `}`), nil
	})
	s.RegisterSkill("fail", "always fails", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("intentional failure")
	})
	return httptest.NewServer(s.HTTPHandler())
}

func newClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return NewClient(srv.URL, ClientOptions{
		PollInterval: 10 * time.Millisecond,
		PollMax:      2 * time.Second,
		Timeout:      5 * time.Second,
	})
}

func TestServer_ListSkills(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newClient(t, srv)
	skills, err := c.ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("got %d skills, want 2", len(skills))
	}
}

func TestServer_ExecuteSkill_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newClient(t, srv)
	out, err := c.ExecuteSkill(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("ExecuteSkill: %v", err)
	}
	if !strings.Contains(string(out), "echoed") {
		t.Errorf("output = %s", string(out))
	}
}

func TestServer_ExecuteSkill_FailingTask(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newClient(t, srv)
	_, err := c.ExecuteSkill(context.Background(), "fail", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "intentional failure") {
		t.Errorf("err = %v, want intentional failure", err)
	}
}

func TestServer_ExecuteSkill_UnknownSkill(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newClient(t, srv)
	_, err := c.ExecuteSkill(context.Background(), "ghost", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "skill not found") {
		t.Errorf("err = %v, want 'skill not found'", err)
	}
}

func TestClient_GetTaskNotFound(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newClient(t, srv)
	_, err := c.GetTask(context.Background(), "nonexistent")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("err = %v, want ErrTaskNotFound", err)
	}
}

func TestClient_BadJSONRejected(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	// Use the underlying HTTP to send malformed body
	resp, err := srv.Client().Post(srv.URL+"/tasks", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAsAgentTool_RoundTrip(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newClient(t, srv)
	tool := AsAgentTool(c, "echo", "remote")
	if tool.Name() != "remote_echo" {
		t.Errorf("Name = %q, want remote_echo", tool.Name())
	}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{"hi":1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("Execute output = %q", out)
	}
}

func TestServer_TaskStateTransitions(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newClient(t, srv)
	// Create task directly + poll to observe state transitions.
	task, err := c.createTask(context.Background(), "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("createTask: %v", err)
	}
	if task.State != TaskPending && task.State != TaskRunning {
		t.Errorf("initial state = %q, want pending or running", task.State)
	}
	// Poll until completion (skill is fast enough)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		t2, _ := c.GetTask(context.Background(), task.ID)
		if t2.State == TaskCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task did not reach Completed within 2s")
}

func TestClient_PollTimeoutReturnsError(t *testing.T) {
	if err := testenv.CanStartHTTPServer(); err != nil {
		t.Skipf("local HTTP server unavailable in this environment: %v", err)
	}
	// Slow skill to force the client to time out.
	s := NewServer("slow", "")
	s.RegisterSkill("slow", "", func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return json.RawMessage(`{}`), nil
		}
	})
	srv := httptest.NewServer(s.HTTPHandler())
	defer srv.Close()
	c := NewClient(srv.URL, ClientOptions{PollInterval: 20 * time.Millisecond, PollMax: 100 * time.Millisecond})
	_, err := c.ExecuteSkill(context.Background(), "slow", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Errorf("err = %v, want poll deadline", err)
	}
}
