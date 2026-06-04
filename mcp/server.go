package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/costa92/llm-agent-comm"
)

// Server is a toy in-memory MCP server: callers register tools +
// resources, then expose Server.Handler() as a comm.Handler that any
// transport can serve. NOT production — Phase 5 minimal scope.
type Server struct {
	name      string
	version   string
	mu        sync.RWMutex
	tools     map[string]toolEntry
	resources map[string]resourceEntry
}

type toolEntry struct {
	descriptor ToolDescriptor
	handler    func(ctx context.Context, args json.RawMessage) (CallResult, error)
}

type resourceEntry struct {
	resource Resource
	read     func(ctx context.Context) (ResourceContent, error)
}

// NewServer constructs a Server.
func NewServer(name, version string) *Server {
	return &Server{
		name:      name,
		version:   version,
		tools:     make(map[string]toolEntry),
		resources: make(map[string]resourceEntry),
	}
}

// RegisterTool adds one tool to the catalog.
func (s *Server) RegisterTool(d ToolDescriptor, handler func(ctx context.Context, args json.RawMessage) (CallResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[d.Name] = toolEntry{descriptor: d, handler: handler}
}

// RegisterResource adds one resource to the catalog.
func (s *Server) RegisterResource(r Resource, read func(ctx context.Context) (ResourceContent, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[r.URI] = resourceEntry{resource: r, read: read}
}

// Handler returns a comm.Handler suitable for InMemoryTransport (or
// for a custom HTTP wrapper). Routes the 5 supported methods.
func (s *Server) Handler() comm.Handler {
	return func(ctx context.Context, env comm.Envelope) (comm.Response, error) {
		switch env.Method {
		case "initialize":
			return s.handleInitialize(env), nil
		case "list_tools":
			return s.handleListTools(env), nil
		case "call_tool":
			return s.handleCallTool(ctx, env), nil
		case "list_resources":
			return s.handleListResources(env), nil
		case "read_resource":
			return s.handleReadResource(ctx, env), nil
		default:
			return comm.Response{
				ID: env.ID,
				Error: &comm.RPCError{Code: -32601, Message: "method not found: " + env.Method},
			}, nil
		}
	}
}

func (s *Server) handleInitialize(env comm.Envelope) comm.Response {
	out := initializeResult{ServerInfo: ServerInfo{Name: s.name, Version: s.version}}
	body, _ := json.Marshal(out)
	return comm.Response{ID: env.ID, Result: body}
}

func (s *Server) handleListTools(env comm.Envelope) comm.Response {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := struct {
		Tools []ToolDescriptor `json:"tools"`
	}{Tools: make([]ToolDescriptor, 0, len(s.tools))}
	for _, t := range s.tools {
		out.Tools = append(out.Tools, t.descriptor)
	}
	body, _ := json.Marshal(out)
	return comm.Response{ID: env.ID, Result: body}
}

func (s *Server) handleCallTool(ctx context.Context, env comm.Envelope) comm.Response {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(env.Params, &p); err != nil {
		return errResp(env.ID, -32602, "invalid params: "+err.Error())
	}
	s.mu.RLock()
	entry, ok := s.tools[p.Name]
	s.mu.RUnlock()
	if !ok {
		return errResp(env.ID, -32601, "unknown tool: "+p.Name)
	}
	out, err := entry.handler(ctx, p.Arguments)
	if err != nil {
		return errResp(env.ID, -32000, err.Error())
	}
	body, _ := json.Marshal(out)
	return comm.Response{ID: env.ID, Result: body}
}

func (s *Server) handleListResources(env comm.Envelope) comm.Response {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := struct {
		Resources []Resource `json:"resources"`
	}{Resources: make([]Resource, 0, len(s.resources))}
	for _, r := range s.resources {
		out.Resources = append(out.Resources, r.resource)
	}
	body, _ := json.Marshal(out)
	return comm.Response{ID: env.ID, Result: body}
}

func (s *Server) handleReadResource(ctx context.Context, env comm.Envelope) comm.Response {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(env.Params, &p); err != nil {
		return errResp(env.ID, -32602, "invalid params: "+err.Error())
	}
	s.mu.RLock()
	entry, ok := s.resources[p.URI]
	s.mu.RUnlock()
	if !ok {
		return errResp(env.ID, -32601, "unknown resource: "+p.URI)
	}
	out, err := entry.read(ctx)
	if err != nil {
		return errResp(env.ID, -32000, err.Error())
	}
	body, _ := json.Marshal(out)
	return comm.Response{ID: env.ID, Result: body}
}

func errResp(id string, code int, msg string) comm.Response {
	return comm.Response{ID: id, Error: &comm.RPCError{Code: code, Message: msg}}
}

// for compile-time use in tests
var _ = fmt.Sprintf
