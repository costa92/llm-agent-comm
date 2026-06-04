package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	agents "github.com/costa92/llm-agent-contract/agents"
	"github.com/costa92/llm-agent-comm/internal/functool"
)

// AsAgentTools fetches the MCP server's tool catalog and returns one
// agents.Tool per remote tool. Names are prefixed with prefix + "_"
// so multiple servers can register without collisions.
//
// Caller must have called client.Initialize first.
func AsAgentTools(ctx context.Context, client *Client, prefix string) ([]agents.Tool, error) {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp/tool: list_tools: %w", err)
	}
	out := make([]agents.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, wrapMCPTool(client, t, prefix))
	}
	return out, nil
}

// wrapMCPTool returns an agents.Tool that proxies Execute → CallTool.
func wrapMCPTool(client *Client, td ToolDescriptor, prefix string) agents.Tool {
	name := td.Name
	if prefix != "" {
		name = prefix + "_" + td.Name
	}
	desc := td.Description
	if desc == "" {
		desc = "MCP tool: " + td.Name
	}
	schema := td.Schema
	if len(schema) == 0 {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	return functool.New(
		name,
		desc,
		schema,
		func(ctx context.Context, args json.RawMessage) (string, error) {
			res, err := client.CallTool(ctx, td.Name, args)
			if err != nil {
				return "", err
			}
			if res.IsError {
				return "", fmt.Errorf("mcp tool %s reported error: %s", td.Name, res.Content)
			}
			return res.Content, nil
		},
	)
}
