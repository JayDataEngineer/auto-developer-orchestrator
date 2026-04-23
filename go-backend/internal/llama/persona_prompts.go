package llama

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// buildPersonaPrompt builds a system prompt for any persona using the template + tool registry.
// This is the single entry point — all four personas go through here.
func buildPersonaPrompt(t PersonaType, pcfg PersonaConfig) string {
	sandboxID := pcfg.SandboxID
	if sandboxID == "" {
		sandboxID = "sandbox-" + filepath.Base(pcfg.ProjectDir)
	}

	// Get tool specs and format as prompt block
	specs := PersonaToolSpecs(t)
	toolsBlock := FormatToolList(specs)
	examples := PersonaExamples(t)

	// Map persona type to template name
	tmplName := personaTemplateName(t)

	data := PromptData{
		SandboxID: sandboxID,
		Tools:     toolsBlock,
		Examples:  examples,
	}

	prompt, err := RenderTemplate(tmplName, data)
	if err != nil {
		// Fall back to a minimal prompt rather than crashing
		return fmt.Sprintf("You are Pi's %s agent. Tools: %s", t, toolsBlock)
	}

	// Append AGENTS.md project context if present (like CLAUDE.md for Claude Code)
	agentsMD := readProjectContextFile(pcfg.ProjectDir, "AGENTS.md")
	if agentsMD == "" {
		agentsMD = readProjectContextFile(pcfg.ProjectDir, "CLAUDE.md")
	}
	if agentsMD != "" {
		prompt += "\n\n--- Project Context (AGENTS.md) ---\n" + agentsMD
	}

	return prompt
}

// readProjectContextFile reads a project-level context file (AGENTS.md, CLAUDE.md)
// from the project directory or its parent. Returns empty string if not found.
func readProjectContextFile(projectDir, filename string) string {
	if projectDir == "" {
		return ""
	}

	// Try project dir first, then parent
	candidates := []string{
		filepath.Join(projectDir, filename),
		filepath.Join(projectDir, "..", filename),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			content := strings.TrimSpace(string(data))
			// Cap at 4K chars to avoid bloating the system prompt
			if len(content) > 4096 {
				content = content[:4093] + "\n..."
			}
			return content
		}
	}
	return ""
}

// personaTemplateName maps PersonaType to the short template filename (without path/extension).
func personaTemplateName(t PersonaType) string {
	switch t {
	case PersonaOrchestrator:
		return "orchestrator"
	case PersonaWeb:
		return "web"
	case PersonaCode:
		return "code"
	case PersonaDesktop:
		return "desktop"
	case PersonaMCP:
		return "mcp"
	case PersonaResearch:
		return "research"
	default:
		return "code" // fallback for unknown personas
	}
}
