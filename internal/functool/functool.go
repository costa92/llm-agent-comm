// Package functool adapts an ExecuteFunc into an agents.Tool. It vendors the
// tiny constructor that core's agents.NewFuncTool provides, so this module
// depends only on llm-agent-contract — never on core llm-agent.
package functool

import (
	"context"
	"encoding/json"

	agents "github.com/costa92/llm-agent-contract/agents"
)

// New returns an agents.Tool backed by fn. Mirrors core agents.NewFuncTool.
func New(name, description string, schema json.RawMessage, fn agents.ExecuteFunc) agents.Tool {
	return &funcTool{name: name, description: description, schema: schema, fn: fn}
}

type funcTool struct {
	name        string
	description string
	schema      json.RawMessage
	fn          agents.ExecuteFunc
}

func (t *funcTool) Name() string            { return t.name }
func (t *funcTool) Description() string     { return t.description }
func (t *funcTool) Schema() json.RawMessage { return t.schema }
func (t *funcTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return t.fn(ctx, args)
}
