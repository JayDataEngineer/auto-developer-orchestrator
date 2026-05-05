package common

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ToOpenAITools converts Tool list to OpenAI format.
func ToOpenAITools(tools []core.Tool) []core.OpenAITool {
	result := make([]core.OpenAITool, 0, len(tools))
	for _, t := range tools {
		result = append(result, core.OpenAITool{
			Type: "function",
			Function: core.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return result
}

// BuildOrchestratorPrompt builds the full orchestrator system prompt.
func BuildOrchestratorPrompt(tools []core.Tool, sandboxID string, projectContext string, examples string) string {
	var b strings.Builder

	b.WriteString("You are Pux — an autonomous agent that can code, browse the web, control the desktop, and research.\n")
	b.WriteString("You handle tasks directly using your tools.\n\n")

	b.WriteString("## BEHAVIOR\n")
	b.WriteString("- BE PROACTIVE: Complete tasks without asking for permission\n")
	b.WriteString("- MAKE DECISIONS: Use context and sensible defaults when details are missing\n")
	b.WriteString("- NO HEDGING: State facts clearly without unnecessary qualifiers\n")
	b.WriteString("- UNDERSTAND -> ACT -> ANSWER: Just do the work and report results\n")
	b.WriteString("- KEEP RESPONSES SHORT: Be concise. Use bullet points and code blocks.\n\n")

	b.WriteString("# Tools\n\n")
	for _, t := range tools {
		schema := formatSchema(t.Schema())
		fmt.Fprintf(&b, "## %s — %s\n%s\n\n", t.Name(), t.Description(), schema)
	}

	b.WriteString("# Rules\n")
	b.WriteString("1. Handle most tasks DIRECTLY with your tools\n")
	b.WriteString("2. DELEGATE complex research/scraping to sub-agents with delegate_to\n")
	b.WriteString("3. After each action, check: did I make progress?\n")
	b.WriteString("4. Do NOT repeat the same action if it failed\n")
	b.WriteString("5. Make decisions autonomously\n")
	b.WriteString("6. When done, call synthesize if you're the orchestrator, or yield_artifact if you're a sub-agent\n\n")

	if projectContext != "" {
		b.WriteString("--- Project Context ---\n")
		b.WriteString(projectContext + "\n\n")
	}

	if examples != "" {
		b.WriteString("--- Examples ---\n")
		b.WriteString(examples + "\n\n")
	}

	b.WriteString("Sandbox ID: " + sandboxID + "\n")

	return b.String()
}

// formatSchema formats a JSON Schema as a readable string.
func formatSchema(schema json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		return string(schema)
	}

	// Format as JSON example with args
	props, _ := m["properties"].(map[string]any)
	if props == nil {
		return string(schema)
	}

	var args []string
	for name, prop := range props {
		pm, _ := prop.(map[string]any)
		desc := ""
		if d, ok := pm["description"].(string); ok {
			desc = d
		}
		typ, _ := pm["type"].(string)
		example := formatExample(typ)
		args = append(args, fmt.Sprintf("%s=%s (%s)", name, example, desc))
	}

	return fmt.Sprintf("Args: %s", strings.Join(args, ", "))
}

func formatExample(typ string) string {
	switch typ {
	case "string":
		return "\"...\""
	case "integer", "number":
		return "0"
	case "boolean":
		return "true"
	case "array":
		return "[]"
	default:
		return "\"...\""
	}
}
