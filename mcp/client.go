package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/costa92/llm-agent-comm"
)

// Client speaks MCP to a server through an underlying comm.Transport.
// One Initialize call per Client; subsequent RPCs reuse the same
// transport.
type Client struct {
	transport  comm.Transport
	serverInfo ServerInfo
	idCounter  atomic.Uint64
	initOK     atomic.Bool
}

// NewClient wraps transport.
func NewClient(transport comm.Transport) *Client {
	return &Client{transport: transport}
}

// ServerInfo returns the server-side identity captured during Initialize.
// Empty if Initialize hasn't run.
func (c *Client) ServerInfo() ServerInfo { return c.serverInfo }

// Initialize performs the MCP handshake. Returns the server's identity
// captured for ServerInfo. Idempotent — second call is a no-op.
func (c *Client) Initialize(ctx context.Context) error {
	if c.initOK.Load() {
		return nil
	}
	params := initializeParams{ProtocolVersion: "2024-11-05"}
	params.ClientInfo.Name = "aics-mcp-client"
	params.ClientInfo.Version = "0.1"
	raw, _ := json.Marshal(params)
	resp, err := c.call(ctx, "initialize", raw)
	if err != nil {
		return err
	}
	var out initializeResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return fmt.Errorf("mcp/client: parse initialize result: %w", err)
	}
	c.serverInfo = out.ServerInfo
	c.initOK.Store(true)
	return nil
}

// ListTools fetches the server's tool catalog.
func (c *Client) ListTools(ctx context.Context) ([]ToolDescriptor, error) {
	if err := c.requireInit(); err != nil {
		return nil, err
	}
	resp, err := c.call(ctx, "list_tools", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []ToolDescriptor `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return nil, fmt.Errorf("mcp/client: parse list_tools: %w", err)
	}
	return out.Tools, nil
}

// CallTool invokes a tool by name with raw JSON args. Returns
// CallResult; tool-side errors become CallResult.IsError=true (not Go errors).
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	if err := c.requireInit(); err != nil {
		return CallResult{}, err
	}
	params := struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Name: name, Arguments: args}
	raw, _ := json.Marshal(params)
	resp, err := c.call(ctx, "call_tool", raw)
	if err != nil {
		return CallResult{}, err
	}
	var out CallResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return CallResult{}, fmt.Errorf("mcp/client: parse call_tool: %w", err)
	}
	return out, nil
}

// ListResources fetches the server's resource list.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	if err := c.requireInit(); err != nil {
		return nil, err
	}
	resp, err := c.call(ctx, "list_resources", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return nil, fmt.Errorf("mcp/client: parse list_resources: %w", err)
	}
	return out.Resources, nil
}

// ReadResource fetches a single resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (ResourceContent, error) {
	if err := c.requireInit(); err != nil {
		return ResourceContent{}, err
	}
	params := struct {
		URI string `json:"uri"`
	}{URI: uri}
	raw, _ := json.Marshal(params)
	resp, err := c.call(ctx, "read_resource", raw)
	if err != nil {
		return ResourceContent{}, err
	}
	var out ResourceContent
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return ResourceContent{}, fmt.Errorf("mcp/client: parse read_resource: %w", err)
	}
	return out, nil
}

// Close releases the underlying transport.
func (c *Client) Close() error { return c.transport.Close() }

// --- internals ------------------------------------------------------------

func (c *Client) call(ctx context.Context, method string, params json.RawMessage) (comm.Response, error) {
	id := strconv.FormatUint(c.idCounter.Add(1), 10)
	resp, err := c.transport.Call(ctx, comm.Envelope{
		ID:     id,
		Method: method,
		Params: params,
	})
	if err != nil {
		return comm.Response{}, err
	}
	if resp.Error != nil {
		return comm.Response{}, fmt.Errorf("mcp/client: server error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp, nil
}

func (c *Client) requireInit() error {
	if !c.initOK.Load() {
		return fmt.Errorf("mcp/client: Initialize must be called first")
	}
	return nil
}
