package comm

import "context"

// Transport sends an Envelope and waits for the matching Response.
// Implementations must be safe for concurrent calls (in-process
// implementations may serialize internally).
type Transport interface {
	Call(ctx context.Context, env Envelope) (Response, error)
	Close() error
}

// Handler is the server-side function an InMemoryTransport calls.
type Handler func(ctx context.Context, env Envelope) (Response, error)
