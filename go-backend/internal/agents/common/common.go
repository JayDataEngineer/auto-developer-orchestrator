package common

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

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

var (
	cachedPrompt     string
	cachedPromptOnce sync.Once
)

// loadPromptTemplate loads the system prompt from config/prompt.md.
// Falls back to an embedded default if the file is missing.
func loadPromptTemplate() string {
	cachedPromptOnce.Do(func() {
		// Try PROJECT_ROOT/config/prompt.md first
		root := os.Getenv("PROJECT_ROOT")
		if root != "" {
			data, err := os.ReadFile(root + "/config/prompt.md")
			if err == nil {
				cachedPrompt = string(data)
				return
			}
		}
		// Try relative path
		data, err := os.ReadFile("config/prompt.md")
		if err == nil {
			cachedPrompt = string(data)
			return
		}
		// Embedded fallback
		cachedPrompt = defaultPrompt
	})
	return cachedPrompt
}

// ReloadPromptTemplate forces a reload of the prompt template (for development).
func ReloadPromptTemplate() {
	cachedPromptOnce = sync.Once{}
}

// BuildOrchestratorPrompt builds the full orchestrator system prompt.
// Loads from config/prompt.md if available, otherwise uses embedded default.
func BuildOrchestratorPrompt(tools []core.Tool, sandboxID string, projectContext string, examples string) string {
	tmpl := loadPromptTemplate()

	// Build tool list
	var toolSection strings.Builder
	for _, t := range tools {
		schema := formatSchema(t.Schema())
		fmt.Fprintf(&toolSection, "## %s — %s\n%s\n\n", t.Name(), t.Description(), schema)
	}

	// Replace {{tools}} placeholder
	prompt := strings.Replace(tmpl, "{{tools}}", toolSection.String(), 1)

	// Append project context and examples
	var b strings.Builder
	b.WriteString(prompt)

	if projectContext != "" {
		b.WriteString("\n--- Project Context ---\n")
		b.WriteString(projectContext + "\n")
	}

	if examples != "" {
		b.WriteString("\n--- Examples ---\n")
		b.WriteString(examples + "\n")
	}

	b.WriteString("\nSandbox ID: " + sandboxID + "\n")

	return b.String()
}

// formatSchema formats a JSON Schema as a readable string.
func formatSchema(schema json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		return string(schema)
	}

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

// Embedded default prompt — used when config/prompt.md is not found.
const defaultPrompt = `You are Pux — an autonomous agent that can code, browse the web, control the desktop, and research.
You handle tasks directly using your tools.

## BEHAVIOR
- BE PROACTIVE: Complete tasks without asking for permission
- MAKE DECISIONS: Use context and sensible defaults when details are missing
- NO HEDGING: State facts clearly without unnecessary qualifiers
- UNDERSTAND -> ACT -> ANSWER: Just do the work and report results
- KEEP RESPONSES SHORT: Be concise. Use bullet points and code blocks.

# Tools

` + "{{tools}}" + `

# Rules
1. Handle most tasks DIRECTLY with your tools
2. DELEGATE complex research/scraping to sub-agents with delegate_to
3. After each action, check: did I make progress?
4. Do NOT repeat the same action if it failed
5. Make decisions autonomously
6. When done, call synthesize if you're the orchestrator, or yield_artifact if you're a sub-agent

# Tool Tips
- **bash**: Use curl to interact with sb_server (localhost:9876) for browsing
- **analyze_image**: Pass image URLs. For local files, get data URI via sb_server /file/ endpoint
- **scrape** returns cleaned markdown but strips images. Use browser for image search.
`
