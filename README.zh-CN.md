[English](./README.md) | [简体中文](./README.zh-CN.md)

# llm-agent-comm

面向 [llm-agent 生态](https://github.com/costa92/llm-agent-ecosystem) 的智能体通信模块：提供一个共享的传输层 + 消息封套（envelope）层，以及两种协议实现 —— A2A（智能体到智能体）与 MCP（Model Context Protocol）。

## 安装

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

唯一的依赖是 `github.com/costa92/llm-agent-contract`（提供 `agents.Tool` 接口）。`a2a` 与 `mcp` 子包可以把远端技能 / 工具暴露为 `agents.Tool`，因此生态中的智能体（agent）可以直接调用它们。

## 它提供什么

### 包 `comm`（base）

根包定义了与协议无关的消息类型，以及子包所依赖的传输层抽象（`doc.go`、`envelope.go`、`transport.go`）。

- **消息类型** —— `Envelope`（一个请求：`ID`、`Method`、`Params`、`Metadata`）、`Response`（`ID`、`Result`、`Error`），以及 `RPCError`（`Code`、`Message`、`Data`，实现 `error`）。
- **`Transport` 接口** —— `Call(ctx, Envelope) (Response, error)` + `Close() error`。实现对并发调用是安全的。
- **`Handler`** —— 服务端处理函数类型 `func(ctx, Envelope) (Response, error)`，供内存传输使用。
- **三种传输实现：**
  - `InMemoryTransport` / `NewInMemoryTransport(handler)` —— 在进程内调用一个 `Handler`；适用于测试与同进程演示（`transport_inmem.go`）。
  - `HTTPTransport` / `NewHTTPTransport(endpoint, HTTPTransportOptions)` —— 将 envelope 以 JSON 形式 POST，再从响应体中解析出 JSON `Response`（`transport_http.go`）。
  - `StdioTransport` / `NewStdioTransport(command, args, StdioTransportOptions)` —— 启动一个子进程，通过其 stdin/stdout 按「每行一个 JSON 文档」交换数据，这正是真实的 MCP stdio 服务器（例如 `npx @modelcontextprotocol/server-filesystem`）所遵循的约定（`transport_stdio.go`）。
- **哨兵错误** —— `ErrTransportClosed`、`ErrTimeout`、`ErrUnsupported`、`ErrServerError`。

### 包 `a2a`（智能体到智能体）

一个基于 HTTP 的简化版 Agent-to-Agent 协议（`a2a/server.go`、`a2a/client.go`、`a2a/task.go`、`a2a/tool.go`）。它与 Google 的 a2a-sdk **并不**在线格式上兼容 —— 其 schema 是刻意做小做简的自定义格式。

- **`Server`** / `NewServer(name, description)` —— 用 `RegisterSkill(name, description, SkillHandler)` 注册技能，并通过 `HTTPHandler()` 对外提供服务，后者暴露 `GET /skills`、`POST /tasks`、`GET /tasks/{id}` 与 `DELETE /tasks/{id}`（取消）。
- **任务生命周期** —— `Task` 经历 `TaskPending` → `TaskRunning` → `TaskCompleted` / `TaskFailed`（取消复用 `TaskFailed`）。任务异步运行，由调用方轮询。状态仅保存在内存中。
- **`Client`** / `NewClient(endpoint, ClientOptions)` —— `ListSkills(ctx)` 返回 `[]SkillDescriptor`；`ExecuteSkill(ctx, skill, input)` 创建一个任务并轮询直至产出 artifact；`GetTask(ctx, id)` 获取单个任务。
- **`AsAgentTool(client, skillName, prefix)`** —— 把一个远端技能包装为 `agents.Tool`。
- **哨兵错误** —— `ErrSkillNotFound`、`ErrTaskNotFound`。

### 包 `mcp`（Model Context Protocol）

一个基于 JSON-RPC 2.0 的最小化 MCP 客户端 + 玩具服务器（`mcp/client.go`、`mcp/server.go`、`mcp/tool.go`、`mcp/types.go`、`mcp/jsonrpc.go`）。它覆盖四个核心 RPC 加上握手 —— `initialize`、`list_tools`、`call_tool`、`list_resources`、`read_resource` —— 而把能力协商、notifications、sampling、progress 与 cancellation 排除在范围之外。

- **`Client`** / `NewClient(transport)` —— 包装任意 `comm.Transport`。`Initialize(ctx)` 执行握手（幂等）；随后可调用 `ListTools`、`CallTool(ctx, name, args)`（返回 `CallResult`）、`ListResources` 与 `ReadResource(ctx, uri)`。`ServerInfo()` 返回握手时捕获的服务端身份；`Close()` 释放传输。
- **`Server`** / `NewServer(name, version)` —— 用 `RegisterTool(ToolDescriptor, handler)` 与 `RegisterResource(Resource, read)` 注册；通过 `Handler()`（一个 `comm.Handler`）在任意传输上对外提供服务，例如 `comm.InMemoryTransport`。
- **`AsAgentTools(ctx, client, prefix)`** —— 拉取服务器的工具目录，为每个远端工具返回一个 `agents.Tool`（名字加前缀以避免冲突）。调用前客户端必须先完成初始化。
- **JSON-RPC 编解码** —— `EncodeRequest`、`DecodeRequest`、`EncodeResponse`、`DecodeResponse` 负责在 `comm.Envelope`/`comm.Response` 与 JSON-RPC 2.0 文本行之间互转。
- **类型** —— `ServerInfo`、`ToolDescriptor`、`CallResult`、`Resource`、`ResourceContent`。

## 最小用法

在进程内运行一个 MCP 服务器，并通过内存传输调用它：

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
// ... 或把它们暴露给一个智能体：
agentTools, err := mcp.AsAgentTools(ctx, client, "demo")
```

要连接真实的 MCP stdio 服务器，替换传输即可：`comm.NewStdioTransport("npx", []string{"@modelcontextprotocol/server-filesystem", "/some/dir"}, comm.StdioTransportOptions{})`。

## 与生态的关系

`llm-agent-comm` 从核心 `llm-agent` 模块中抽取而来（Phase A extraction，`v0.1.0`）。它仅依赖 `llm-agent-contract` 提供的 `agents.Tool` 接口，因此生态中任何消费方都可以把远端 A2A 技能或 MCP 工具包装为智能体工具，而无需引入 core。ANP（Agent Network Protocol）原型被刻意保留在 core 中，不属于本模块。

## 开发

```bash
GOWORK=off go vet ./...
GOWORK=off go build ./...
GOWORK=off go test ./... -count=1
```
