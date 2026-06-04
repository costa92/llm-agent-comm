package comm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPTransportOptions configures HTTPTransport.
type HTTPTransportOptions struct {
	HTTPClient *http.Client      // nil → http.DefaultClient with Timeout
	Timeout    time.Duration     // default 30s
	Headers    map[string]string // additional request headers (auth, tracing)
}

// HTTPTransport POSTs the Envelope as JSON and parses the Response
// from the HTTP body. Wire shape: request body is the envelope JSON;
// response body is a JSON Response document.
type HTTPTransport struct {
	endpoint string
	client   *http.Client
	headers  map[string]string
	mu       sync.RWMutex
	closed   bool
}

// NewHTTPTransport constructs an HTTPTransport.
func NewHTTPTransport(endpoint string, opts HTTPTransportOptions) *HTTPTransport {
	c := opts.HTTPClient
	if c == nil {
		t := opts.Timeout
		if t == 0 {
			t = 30 * time.Second
		}
		c = &http.Client{Timeout: t}
	}
	return &HTTPTransport{endpoint: endpoint, client: c, headers: opts.Headers}
}

// Call implements Transport.
func (t *HTTPTransport) Call(ctx context.Context, env Envelope) (Response, error) {
	t.mu.RLock()
	closed := t.closed
	t.mu.RUnlock()
	if closed {
		return Response{}, ErrTransportClosed
	}

	body, err := json.Marshal(env)
	if err != nil {
		return Response{}, fmt.Errorf("comm/http: marshal envelope: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("comm/http: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	for k, v := range env.Metadata {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Response{}, ErrTimeout
		}
		return Response{}, fmt.Errorf("comm/http: do: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("comm/http: read body: %w", err)
	}
	if resp.StatusCode >= 500 {
		return Response{}, fmt.Errorf("%w: status %d body=%s", ErrServerError, resp.StatusCode, truncate(raw, 200))
	}
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		return Response{}, fmt.Errorf("comm/http: unmarshal response: %w (body=%s)", err, truncate(raw, 200))
	}
	if r.ID == "" {
		r.ID = env.ID
	}
	return r, nil
}

// Close implements Transport. The underlying http.Client is shared and
// not closed (transport-level, not connection-level).
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
