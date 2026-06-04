package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/costa92/llm-agent-comm"
)

// --- jsonrpc round-trip ---------------------------------------------------

func TestEncodeDecodeRequest(t *testing.T) {
	env := comm.Envelope{ID: "1", Method: "ping", Params: json.RawMessage(`{"x":1}`)}
	wire, err := EncodeRequest(env)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(wire), `"jsonrpc":"2.0"`) {
		t.Errorf("missing jsonrpc 2.0 marker: %s", wire)
	}
	got, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != env.ID || got.Method != env.Method || string(got.Params) != string(env.Params) {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestEncodeDecodeResponse_Result(t *testing.T) {
	r := comm.Response{ID: "1", Result: json.RawMessage(`{"ok":true}`)}
	wire, _ := EncodeResponse(r)
	got, err := DecodeResponse(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(got.Result) != string(r.Result) {
		t.Errorf("Result = %s", string(got.Result))
	}
}

func TestEncodeDecodeResponse_Error(t *testing.T) {
	r := comm.Response{ID: "1", Error: &comm.RPCError{Code: -32601, Message: "method not found"}}
	wire, _ := EncodeResponse(r)
	got, _ := DecodeResponse(wire)
	if got.Error == nil || got.Error.Code != -32601 || got.Error.Message != "method not found" {
		t.Errorf("Error = %+v", got.Error)
	}
}

func TestDecodeRequest_RejectsMissingMethod(t *testing.T) {
	if _, err := DecodeRequest([]byte(`{"jsonrpc":"2.0","id":"1"}`)); err == nil {
		t.Error("expected error for missing method")
	}
}

// --- Server + Client (in-memory transport) -------------------------------

func newServerWithTools() *Server {
	s := NewServer("test-server", "0.1")
	s.RegisterTool(
		ToolDescriptor{Name: "echo", Description: "echo", Schema: json.RawMessage(`{"type":"object"}`)},
		func(_ context.Context, args json.RawMessage) (CallResult, error) {
			return CallResult{Content: "echo:" + string(args)}, nil
		},
	)
	s.RegisterResource(
		Resource{URI: "mem://hello", Name: "Hello", MimeType: "text/plain"},
		func(_ context.Context) (ResourceContent, error) {
			return ResourceContent{URI: "mem://hello", Text: "hello world"}, nil
		},
	)
	return s
}

func newClientToServer(s *Server) *Client {
	tr := comm.NewInMemoryTransport(s.Handler())
	return NewClient(tr)
}

func TestClient_InitializeAndServerInfo(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if c.ServerInfo().Name != "test-server" {
		t.Errorf("ServerInfo.Name = %q", c.ServerInfo().Name)
	}
}

func TestClient_RequiresInitializeBeforeRPC(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_, err := c.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Initialize") {
		t.Errorf("err = %v, want 'Initialize must be called first'", err)
	}
}

func TestClient_ListTools(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_ = c.Initialize(context.Background())
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Errorf("tools = %+v", tools)
	}
}

func TestClient_CallTool(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_ = c.Initialize(context.Background())
	res, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{"x":42}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(res.Content, "42") {
		t.Errorf("content = %q", res.Content)
	}
}

func TestClient_CallTool_UnknownToolError(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_ = c.Initialize(context.Background())
	_, err := c.CallTool(context.Background(), "does_not_exist", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("err = %v, want unknown-tool error", err)
	}
}

func TestClient_ListResources(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_ = c.Initialize(context.Background())
	rs, err := c.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(rs) != 1 || rs[0].URI != "mem://hello" {
		t.Errorf("resources = %+v", rs)
	}
}

func TestClient_ReadResource(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_ = c.Initialize(context.Background())
	rc, err := c.ReadResource(context.Background(), "mem://hello")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if rc.Text != "hello world" {
		t.Errorf("Text = %q", rc.Text)
	}
}

func TestServer_UnknownMethodReturnsRPCError(t *testing.T) {
	s := newServerWithTools()
	tr := comm.NewInMemoryTransport(s.Handler())
	resp, err := tr.Call(context.Background(), comm.Envelope{ID: "x", Method: "no_such_method"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("Error = %+v", resp.Error)
	}
}

// --- AsAgentTools ---------------------------------------------------------

func TestAsAgentTools_WrapsAndDelegates(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_ = c.Initialize(context.Background())
	tools, err := AsAgentTools(context.Background(), c, "test")
	if err != nil {
		t.Fatalf("AsAgentTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	if tools[0].Name() != "test_echo" {
		t.Errorf("Name = %q, want test_echo (prefix applied)", tools[0].Name())
	}
	out, err := tools[0].Execute(context.Background(), json.RawMessage(`{"hi":1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("Execute output = %q", out)
	}
}

func TestAsAgentTools_NoPrefix(t *testing.T) {
	s := newServerWithTools()
	c := newClientToServer(s)
	_ = c.Initialize(context.Background())
	tools, _ := AsAgentTools(context.Background(), c, "")
	if tools[0].Name() != "echo" {
		t.Errorf("Name = %q, want echo (no prefix)", tools[0].Name())
	}
}
