// Package mcp implements a minimal Model Context Protocol client +
// toy server. Wire format: JSON-RPC 2.0. Self-written (no third-party
// library) because MCP layers Initialize/Notifications on top.
//
// What's covered: 4 essential RPCs (list_tools / call_tool /
// list_resources / read_resource) + Initialize handshake. Capabilities
// negotiation, sampling, notifications, progress, cancellation are
// out of scope (Phase 5 minimal-credible scope).
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/costa92/llm-agent-comm"
)

// JSON-RPC 2.0 wire envelopes. Notifications (no id) are accepted but
// our minimal Initialize protocol doesn't need them.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// EncodeRequest serializes an Envelope into a JSON-RPC 2.0 request line.
func EncodeRequest(env comm.Envelope) ([]byte, error) {
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      env.ID,
		Method:  env.Method,
		Params:  env.Params,
	}
	return json.Marshal(req)
}

// DecodeResponse parses a JSON-RPC 2.0 response line into a comm.Response.
func DecodeResponse(line []byte) (comm.Response, error) {
	var r rpcResponse
	if err := json.Unmarshal(line, &r); err != nil {
		return comm.Response{}, fmt.Errorf("mcp: decode response: %w", err)
	}
	if r.JSONRPC != "" && r.JSONRPC != "2.0" {
		return comm.Response{}, fmt.Errorf("mcp: unexpected jsonrpc version %q", r.JSONRPC)
	}
	out := comm.Response{ID: r.ID, Result: r.Result}
	if r.Error != nil {
		out.Error = &comm.RPCError{Code: r.Error.Code, Message: r.Error.Message, Data: r.Error.Data}
	}
	return out, nil
}

// EncodeResponse serializes a comm.Response into a JSON-RPC 2.0 response line.
func EncodeResponse(r comm.Response) ([]byte, error) {
	out := rpcResponse{JSONRPC: "2.0", ID: r.ID, Result: r.Result}
	if r.Error != nil {
		out.Error = &rpcError{Code: r.Error.Code, Message: r.Error.Message, Data: r.Error.Data}
	}
	return json.Marshal(out)
}

// DecodeRequest parses one JSON-RPC 2.0 request line into an Envelope.
func DecodeRequest(line []byte) (comm.Envelope, error) {
	var r rpcRequest
	if err := json.Unmarshal(line, &r); err != nil {
		return comm.Envelope{}, fmt.Errorf("mcp: decode request: %w", err)
	}
	if r.JSONRPC != "" && r.JSONRPC != "2.0" {
		return comm.Envelope{}, fmt.Errorf("mcp: unexpected jsonrpc version %q", r.JSONRPC)
	}
	if r.Method == "" {
		return comm.Envelope{}, errors.New("mcp: request missing method")
	}
	return comm.Envelope{ID: r.ID, Method: r.Method, Params: r.Params}, nil
}
