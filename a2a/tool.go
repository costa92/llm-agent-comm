package a2a

import (
	"context"
	"encoding/json"
	"fmt"

	agents "github.com/costa92/llm-agent-contract/agents"
	"github.com/costa92/llm-agent-comm/internal/functool"
)

// AsAgentTool wraps a remote skill as an agents.Tool. The schema is
// permissive ({"type":"object"}) — A2A doesn't standardize per-skill
// schemas in our minimal version. Override via NewFuncTool if you need
// stricter contracts.
func AsAgentTool(client *Client, skillName, prefix string) agents.Tool {
	name := skillName
	if prefix != "" {
		name = prefix + "_" + skillName
	}
	desc := fmt.Sprintf("Remote A2A skill %q at %s.", skillName, client.endpoint)
	return functool.New(
		name,
		desc,
		json.RawMessage(`{"type":"object"}`),
		func(ctx context.Context, args json.RawMessage) (string, error) {
			out, err := client.ExecuteSkill(ctx, skillName, args)
			if err != nil {
				return "", err
			}
			return string(out), nil
		},
	)
}
