[English](./README.md) | [简体中文](./README.zh-CN.md)

# llm-agent-comm

Agent communication for the [llm-agent ecosystem](https://github.com/costa92/llm-agent-ecosystem): a shared transport + message-envelope layer plus two protocol implementations — A2A (agent-to-agent) and MCP (Model Context Protocol).

## Install

```bash
go get github.com/costa92/llm-agent-comm
```

```go
import (
    comm "github.com/costa92/llm-agent-comm"
    "github.com/costa92/llm-agent-comm/a2a"
    "github.com/costa92/llm-agent-comm/mcp"
)
```

The only dependency is `github.com/costa92/llm-agent-contract` (for the `agents.Tool` interface). The `a2a` and `mcp` subpackages can expose remote skills/tools as `agents.Tool`, so an agent in the ecosystem can call them directly.

## What it provides

### Package `comm` (base)

The root package defines the protocol-agnostic message types and the transport abstraction that the subpackages build on (`doc.go`, `envelope.go`, `transport.go`).

- **Message types** — `Envelope` (a request: `ID`, `Method`, `Params`, `Metadata`), `Response` (`ID`, `Result`, `Error`), and `RPCError` (`Code`, `Message`, `Data`, implements `error`).
- **`Transport` interface** — `Call(ctx, Envelope) (Response, error)` + `Close() error`. Implementations are safe for concurrent calls.
- **`Handler`** — the server-side function type `func(ctx, Envelope) (Response, error)` used by the in-memory transport.
- **Three transports:**
  - `InMemoryTransport` / `NewInMemoryTransport(handler)` — invokes a `Handler` in-process; useful for tests and same-process demos (`transport_inmem.go`).
  - `HTTPTransport` / `NewHTTPTransport(endpoint, HTTPTransportOptions)` — POSTs the envelope as JSON, parses a JSON `Response` from the body (`transport_http.go`).
  - `StdioTransport` / `NewStdioTransport(command, args, StdioTransportOptions)` — launches a child process and exchanges one JSON document per line over stdin/stdout, the convention real MCP stdio servers (e.g. `npx @modelcontextprotocol/server-filesystem`) follow (`transport_stdio.go`).
- **Sentinel errors** — `ErrTransportClosed`, `ErrTimeout`, `ErrUnsupported`, `ErrServerError`.

### Package `a2a` (agent-to-agent)

A simplified Agent-to-Agent protocol over HTTP (`a2a/server.go`, `a2a/client.go`, `a2a/task.go`, `a2a/tool.go`). It is **not** wire-compatible with Google's a2a-sdk — the schema is deliberately custom-and-tiny.

- **`Server`** / `NewServer(name, description)` — register skills with `RegisterSkill(name, description, SkillHandler)` and serve them via `HTTPHandler()`, which exposes `GET /skills`, `POST /tasks`, `GET /tasks/{id}`, and `DELETE /tasks/{id}` (cancel).
- **Task lifecycle** — `Task` moves through `TaskPending` → `TaskRunning` → `TaskCompleted` / `TaskFailed` (cancel reuses `TaskFailed`). Tasks run asynchronously; the caller polls. State is in-memory only.
- **`Client`** / `NewClient(endpoint, ClientOptions)` — `ListSkills(ctx)` returns `[]SkillDescriptor`; `ExecuteSkill(ctx, skill, input)` creates a task and polls until it produces an artifact; `GetTask(ctx, id)` fetches one task.
- **`AsAgentTool(client, skillName, prefix)`** — wraps a remote skill as an `agents.Tool`.
- **Sentinel errors** — `ErrSkillNotFound`, `ErrTaskNotFound`.

### Package `mcp` (Model Context Protocol)

A minimal MCP client + toy server over JSON-RPC 2.0 (`mcp/client.go`, `mcp/server.go`, `mcp/tool.go`, `mcp/types.go`, `mcp/jsonrpc.go`). It covers four essential RPCs plus the handshake — `initialize`, `list_tools`, `call_tool`, `list_resources`, `read_resource` — and leaves capability negotiation, notifications, sampling, progress, and cancellation out of scope.

- **`Client`** / `NewClient(transport)` — wraps any `comm.Transport`. `Initialize(ctx)` performs the handshake (idempotent); then `ListTools`, `CallTool(ctx, name, args)` (returning `CallResult`), `ListResources`, and `ReadResource(ctx, uri)`. `ServerInfo()` returns the captured server identity; `Close()` releases the transport.
- **`Server`** / `NewServer(name, version)` — register with `RegisterTool(ToolDescriptor, handler)` and `RegisterResource(Resource, read)`; expose `Handler()` (a `comm.Handler`) over any transport, e.g. `comm.InMemoryTransport`.
- **`AsAgentTools(ctx, client, prefix)`** — fetches the server's tool catalog and returns one `agents.Tool` per remote tool (names prefixed to avoid collisions). The client must be initialized first.
- **JSON-RPC codec** — `EncodeRequest`, `DecodeRequest`, `EncodeResponse`, `DecodeResponse` translate between `comm.Envelope`/`comm.Response` and JSON-RPC 2.0 lines.
- **Types** — `ServerInfo`, `ToolDescriptor`, `CallResult`, `Resource`, `ResourceContent`.

## Minimal usage

Run an MCP server in-process and call it through the in-memory transport:

```go
srv := mcp.NewServer("demo", "0.1")
srv.RegisterTool(
    mcp.ToolDescriptor{Name: "echo", Description: "echo back the input"},
    func(ctx context.Context, args json.RawMessage) (mcp.CallResult, error) {
        return mcp.CallResult{Content: string(args)}, nil
    },
)

transport := comm.NewInMemoryTransport(srv.Handler())
client := mcp.NewClient(transport)
defer client.Close()

if err := client.Initialize(ctx); err != nil {
    log.Fatal(err)
}
tools, err := client.ListTools(ctx)
// ... or expose them to an agent:
agentTools, err := mcp.AsAgentTools(ctx, client, "demo")
```

For a real MCP stdio server, swap the transport: `comm.NewStdioTransport("npx", []string{"@modelcontextprotocol/server-filesystem", "/some/dir"}, comm.StdioTransportOptions{})`.

## Relationship to the ecosystem

`llm-agent-comm` was extracted from the core `llm-agent` module (Phase A extraction, `v0.1.0`). It depends only on `llm-agent-contract` for the `agents.Tool` interface, so any consumer in the ecosystem can wrap remote A2A skills or MCP tools as agent tools without pulling in core. The ANP (Agent Network Protocol) prototype was intentionally kept in core and is not part of this module.

## Development

```bash
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go test ./... -count=1
```
