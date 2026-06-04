// Package comm is the shared transport + message-envelope layer for
// the agent-to-agent (A2A), Model-Context-Protocol (MCP), and Agent-
// Network-Protocol (ANP) sub-protocols. Each sub-protocol lives in
// its own subpackage and reuses comm.Transport + comm.Envelope.
//
// # Portability
//
// comm inherits the agents/pkg/llm portability contract — no
// internal/*, no project pkg/*, no business vocabulary.
package comm

import (
	"encoding/json"
	"errors"
)

// Envelope is the in-process representation of a request. Sub-protocols
// translate Envelope <-> their wire format (JSON-RPC 2.0 for MCP,
// task-shape JSON for A2A, etc.).
type Envelope struct {
	ID       string          // request-response correlation
	Method   string          // RPC method / handler name
	Params   json.RawMessage // arbitrary payload
	Metadata map[string]string
}

// Response pairs with Envelope.ID. Either Result or Error is set; never both.
type Response struct {
	ID     string
	Result json.RawMessage
	Error  *RPCError
}

// RPCError carries protocol-level errors back to the caller. Code
// follows JSON-RPC 2.0 conventions where applicable; Data is opaque.
type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

// Error implements error so RPCError can be returned from Transport
// helpers when convenient.
func (e *RPCError) Error() string {
	if e == nil {
		return "<nil rpc error>"
	}
	return e.Message
}

// Common sentinel errors. Sub-protocols may wrap these or define their
// own; keep top-level set small.
var (
	ErrTransportClosed = errors.New("comm: transport closed")
	ErrTimeout         = errors.New("comm: timeout")
	ErrUnsupported     = errors.New("comm: method unsupported")
	ErrServerError     = errors.New("comm: server error")
)
