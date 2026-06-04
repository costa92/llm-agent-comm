// Package mcp implements a minimal MCP (Model Context Protocol) client
// + toy server. Wire format: JSON-RPC 2.0 (self-written, no third-
// party library).
//
// What's covered (4 essential RPCs + handshake):
//
//   - initialize  — capability handshake
//   - list_tools / call_tool
//   - list_resources / read_resource
//
// What's NOT covered (out of scope for Phase 5 minimal):
//
//   - capabilities negotiation
//   - notifications / sampling / progress / cancellation
//   - prompts surface
//
// Real MCP servers (e.g. `npx @modelcontextprotocol/server-filesystem`)
// can be reached via comm.NewStdioTransport — the 4 RPCs above are
// enough to discover + invoke the server's tools.
//
// AsAgentTools exposes a remote MCP server's catalog as []agents.Tool
// so a Phase 1 ReActAgent can use them directly.
//
// # Portability
//
// mcp inherits the agents/pkg/llm portability contract.
package mcp
