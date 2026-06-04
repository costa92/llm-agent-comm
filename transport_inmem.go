package comm

import (
	"context"
	"sync"
)

// InMemoryTransport invokes a server-side Handler in-process. Useful
// for unit tests and same-process Agent-to-Agent demos. Goroutine-safe.
type InMemoryTransport struct {
	handler Handler
	mu      sync.RWMutex
	closed  bool
}

// NewInMemoryTransport wraps handler as a Transport.
func NewInMemoryTransport(handler Handler) *InMemoryTransport {
	return &InMemoryTransport{handler: handler}
}

// Call implements Transport.
func (t *InMemoryTransport) Call(ctx context.Context, env Envelope) (Response, error) {
	t.mu.RLock()
	if t.closed {
		t.mu.RUnlock()
		return Response{}, ErrTransportClosed
	}
	h := t.handler
	t.mu.RUnlock()

	if h == nil {
		return Response{ID: env.ID, Error: &RPCError{Code: -32601, Message: "no handler"}}, nil
	}
	return h(ctx, env)
}

// Close implements Transport.
func (t *InMemoryTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}
