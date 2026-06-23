package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	mcpclient "github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/tools"
)

// MCPTool wraps an MCP server tool as a core.Tool for the agent registry.
// The Name is prefixed with mcp__{server}__ to match naming in skill prompts
// (e.g. mcp__web__research, mcp__media__analyze_image) and to prevent name
// collisions between MCP servers that expose same-named tools.
type MCPTool struct {
	prefixedName string          // mcp__web__research
	rawName      string          // research (passed to MultiClient.CallTool)
	description  string
	schema       json.RawMessage
	client       *mcpclient.MultiClient
}

// NewMCPTool creates a core.Tool wrapper for an MCP tool.
// The tool name is prefixed with mcp__{prefix}__{rawName} so skill prompts
// that reference tools by their MCP-prefixed names (e.g. mcp__web__research)
// will match the registered tool names.
func NewMCPTool(tool mcpclient.MCPTool, client *mcpclient.MultiClient, prefix string) *MCPTool {
	prefixedName := tool.Name
	if prefix != "" {
		prefixedName = fmt.Sprintf("mcp__%s__%s", prefix, tool.Name)
	}
	return &MCPTool{
		prefixedName: prefixedName,
		rawName:      tool.Name,
		description:  tool.Description,
		schema:       tool.InputSchema,
		client:       client,
	}
}

func (t *MCPTool) Name() string               { return t.prefixedName }
func (t *MCPTool) Description() string         { return t.description }
func (t *MCPTool) Schema() json.RawMessage     { return t.schema }
func (t *MCPTool) RawName() string             { return t.rawName }

func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// Use the raw (unprefixed) name when calling the MultiClient — it routes
	// via its internal toolMap which uses raw server tool names.
	result, err := t.client.CallTool(ctx, t.rawName, args)
	if err != nil {
		return nil, fmt.Errorf("MCP %s: %w", t.prefixedName, err)
	}
	// Quarantine prompt-injection patterns before the result reaches the model.
	// MCP tools (web-research, media-analysis, equibles) return untrusted
	// text from arbitrary external sources; this is the instruction_following_on_untrusted_input
	// defense from the Fable/Mythos taxonomy.
	return tools.QuarantineResult(result), nil
}

// RegisterAll creates core.Tool wrappers for all tools discovered by the MultiClient
// and appends them to the tools slice. Each tool is named mcp__{prefix}__{name}
// so skill prompts that reference tools by MCP-prefixed names will match.
// Returns the updated slice.
func RegisterAll(tools []core.Tool, client *mcpclient.MultiClient) []core.Tool {
	if client == nil {
		return tools
	}

	mcpTools := client.AllTools()
	for _, t := range mcpTools {
		prefix := ""
		if c := client.ClientForTool(t.Name); c != nil {
			prefix = c.Prefix()
		}
		tools = append(tools, NewMCPTool(t, client, prefix))
	}

	return tools
}
