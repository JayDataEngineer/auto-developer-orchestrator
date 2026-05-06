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

	b.WriteString("# Tool Tips\n")
	b.WriteString("## sb_agent.py — Stealth Browser (bypasses Cloudflare, CAPTCHAs)\n")
	b.WriteString("All commands return JSON. Add `--stealth` for maximum anti-bot bypass (UC Mode).\n")
	b.WriteString("- `sb_agent.py navigate <url>` — page text + image URLs + links\n")
	b.WriteString("- `sb_agent.py search <query>` — Google search results\n")
	b.WriteString("- `sb_agent.py extract_images <url>` — only image URLs (fastest for finding images)\n")
	b.WriteString("- `sb_agent.py interact <url>` — page data + interactive elements (buttons, inputs, links)\n")
	b.WriteString("- `sb_agent.py screenshot <url> <path>` — take screenshot\n")
	b.WriteString("- `sb_agent.py run <code>` — multi-step browsing. `sb` is pre-initialized SeleniumBase CDP instance.\n")
	b.WriteString("  - `sb.get(url)`, `sb.click(selector)`, `sb.type(selector, text)`, `sb.select_all(selector)`\n")
	b.WriteString("  - `sb.get_text(selector)`, `sb.get_title()`, `sb.get_current_url()`, `sb.sleep(seconds)`\n")
	b.WriteString("  - `sb.go_back()`, `sb.scroll_down()`, `sb.solve_captcha()`, `sb.save_screenshot(path)`\n")
	b.WriteString("  - Set `result` dict to return custom JSON. Otherwise page data is returned.\n")
	b.WriteString("## Other Tools\n")
	b.WriteString("- **analyze_image**: Pass any direct image URL (jpg, png, webp). Describes what's in the image.\n")
	b.WriteString("- **Downloading**: `curl -L -o /path/file URL`. Use `file /path/file` to check format.\n")
	b.WriteString("- **Image conversion**: `python3 -c \"from PIL import Image; Image.open('in.webp').convert('RGB').save('out.jpg','JPEG')\"`\n")
	b.WriteString("- **scrape** returns cleaned markdown — strips <img> tags. Use sb_agent.py for images.\n")
	b.WriteString("## Image Search Flow\n")
	b.WriteString("sb_agent.py search → sb_agent.py extract_images on result pages → curl download → analyze_image\n\n")

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
