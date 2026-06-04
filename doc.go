// Package comm provides the shared transport + envelope layer for the
// agent communication protocols (MCP, A2A, ANP). Each protocol lives
// in its own subpackage and reuses comm.Transport + comm.Envelope.
//
// # What's here
//
//   - Envelope / Response / RPCError — protocol-agnostic message types
//   - Transport interface
//   - 3 transports: InMemory (test/same-process), HTTP (A2A default),
//     Stdio (MCP default — runs a subprocess and exchanges JSON lines)
//   - Sentinel errors: ErrTransportClosed / ErrTimeout / ErrUnsupported / ErrServerError
//
// # Subpackages
//
//   - mcp — Model Context Protocol client/server (JSON-RPC 2.0, 4 RPCs + Initialize)
//   - a2a — Agent-to-Agent skill/task HTTP protocol (custom-and-tiny)
//   - anp — Agent Network Protocol service registry (in-memory, no DID)
//
// # Portability
//
// comm inherits the agents/pkg/llm portability contract — no
// internal/*, no project pkg/*, no business vocabulary.
package comm
