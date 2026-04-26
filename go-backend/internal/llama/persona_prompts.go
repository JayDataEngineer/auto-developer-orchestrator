package llama

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// buildPersonaPrompt builds the system prompt for the orchestrator using its template.
func buildPersonaPrompt(t PersonaType, pcfg PersonaConfig) string {
	sandboxID := pcfg.SandboxID
	if sandboxID == "" {
		sandboxID = "sandbox-" + filepath.Base(pcfg.ProjectDir)
	}

	// Get tool specs and format as prompt block
	specs := PersonaToolSpecs(t)
	toolsBlock := FormatToolList(specs)
	examples := PersonaExamples(t)

	mcpRef := MCPToolReference()

	data := PromptData{
		SandboxID:    sandboxID,
		Tools:        toolsBlock,
		MCPReference: mcpRef,
		Examples:     examples,
	}

	prompt, err := RenderTemplate("orchestrator", data)
	if err != nil {
		// Fall back to a minimal prompt rather than crashing
		return fmt.Sprintf("You are Pux, an autonomous agent. Tools: %s", toolsBlock)
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

// buildSubAgentPrompt builds the system prompt for a dynamically-created sub-agent.
// The orchestrator writes the instructions; we add the tool reference and sandbox info.
func buildSubAgentPrompt(instructions string, toolsBlock string, pcfg PersonaConfig) string {
	sandboxID := pcfg.SandboxID
	if sandboxID == "" {
		sandboxID = "sandbox-" + filepath.Base(pcfg.ProjectDir)
	}

	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\n\n# Tools\n\n")
	b.WriteString(toolsBlock)
	b.WriteString("\n\nSandbox ID: ")
	b.WriteString(sandboxID)

	return b.String()
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
