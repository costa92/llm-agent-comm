package mcp

import "encoding/json"

// ServerInfo is what Initialize returns about the server side.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ToolDescriptor is the schema-bearing description of one MCP tool.
type ToolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"input_schema,omitempty"`
}

// CallResult is the unified result from CallTool. Content is the tool
// output (text payload); IsError flags errors surfaced by the tool
// itself (vs transport-level RPC errors).
type CallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Resource is one item from list_resources.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

// ResourceContent is what read_resource returns.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mime_type,omitempty"`
	Text     string `json:"text"`
}

// initializeParams / initializeResult shape the MCP initialize call.
type initializeParams struct {
	ProtocolVersion string `json:"protocol_version"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"client_info"`
}

type initializeResult struct {
	ServerInfo ServerInfo `json:"server_info"`
}
