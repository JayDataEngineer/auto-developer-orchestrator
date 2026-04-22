package llama

import (
	"fmt"
	"path/filepath"
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
	return prompt
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
	default:
		return "code" // fallback for unknown personas
	}
}
