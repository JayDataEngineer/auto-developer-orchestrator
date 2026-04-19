package llama

import (
	"fmt"
	"path/filepath"
)

// LibraryPromptConfig holds parameters for building the library mode system prompt.
type LibraryPromptConfig struct {
	ProjectDir string
	SandboxID  string
}

// BuildLibraryModeSystemPrompt returns the system prompt for the llama-go agent.
// Uses the tool registry for tool definitions and templates for rendering.
func BuildLibraryModeSystemPrompt(cfg LibraryPromptConfig) string {
	sandboxID := cfg.SandboxID
	if sandboxID == "" {
		sandboxID = "sandbox-" + filepath.Base(cfg.ProjectDir)
	}

	// Monolithic agent has browser + execution tools
	tools := ToolsByCategory(CategoryBrowser, CategoryExecution)
	toolsBlock := FormatToolList(tools)

	// Identity + rules (kept inline since monolithic is the fallback path)
	identity := fmt.Sprintf(`You are Pi. You call tools to accomplish tasks.

RULES:
- Always call a tool. Never respond with only text.
- Use search_web for any search. Use browse_to for URLs.
- When task is done, output the result as text (no tool call).
- Before risky actions (posting, emailing, payments), output: ??APPROVAL: <description>
- For questions, output: ??QUESTION: <your question>
- Credentials: /sandbox/workspace/passwords.txt
- Persistent files: /sandbox/persist
- Sandbox ID: %s`, sandboxID)

	// Examples from registry
	examples := PersonaExamples(PersonaWeb) // monolithic uses web-style examples

	data := PromptData{
		SandboxID: sandboxID,
		Tools:     toolsBlock,
		Identity:  identity,
		Examples:  examples,
	}

	prompt, err := RenderTemplate("monolithic", data)
	if err != nil {
		// Fall back to simple concatenation
		return identity + "\n\n" + toolsBlock
	}
	return prompt
}
