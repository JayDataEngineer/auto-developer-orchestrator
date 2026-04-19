package llama

// PersonaType identifies a specific agent persona.
type PersonaType string

const (
	PersonaOrchestrator PersonaType = "orchestrator"
	PersonaWeb          PersonaType = "web"
	PersonaCode         PersonaType = "code"
	PersonaDesktop      PersonaType = "desktop"
)

// Persona defines an agent's identity, capabilities, and generation config.
// Each persona gets a focused system prompt and a restricted tool whitelist
// so the 26B model only needs to reason about the tools relevant to its task.
type Persona struct {
	Type          PersonaType
	SystemPrompt  string
	Tools         []string // whitelist of tool names this persona can call
	MaxToolRounds int
	MaxTokens     int
	Temperature   float32
}

// PersonaConfig holds parameters for building persona prompts.
type PersonaConfig struct {
	ProjectDir string
	SandboxID  string
}

// NewPersona creates a persona of the given type with the provided config.
func NewPersona(t PersonaType, cfg PersonaConfig) *Persona {
	switch t {
	case PersonaOrchestrator:
		return &Persona{
			Type:          PersonaOrchestrator,
			SystemPrompt:  buildOrchestratorPrompt(cfg),
			Tools:         []string{"delegate_to", "create_plan", "update_plan", "synthesize"},
			MaxToolRounds: 15, // orchestrator does planning, not execution
			MaxTokens:     2048,
			Temperature:   0.7,
		}
	case PersonaWeb:
		return &Persona{
			Type:          PersonaWeb,
			SystemPrompt:  buildWebPersonaPrompt(cfg),
			Tools:         []string{"search_web", "browse_to", "click_element", "type_text", "read_page", "bash"},
			MaxToolRounds: 20,
			MaxTokens:     2048,
			Temperature:   0.4, // lower temp for focused web tasks
		}
	case PersonaCode:
		return &Persona{
			Type:          PersonaCode,
			SystemPrompt:  buildCodePersonaPrompt(cfg),
			Tools:         []string{"bash"},
			MaxToolRounds: 20,
			MaxTokens:     2048,
			Temperature:   0.3, // low temp for precise code execution
		}
	case PersonaDesktop:
		return &Persona{
			Type:          PersonaDesktop,
			SystemPrompt:  buildDesktopPersonaPrompt(cfg),
			Tools:         []string{"computer_use_enable", "computer_use_screenshot", "computer_use_snapshot", "computer_use_act", "desktop_screenshot", "desktop_click", "desktop_type", "desktop_key", "bash"},
			MaxToolRounds: 30, // desktop automation needs many rounds
			MaxTokens:     2048,
			Temperature:   0.4,
		}
	default:
		return nil
	}
}

// HasTool returns whether the persona's tool whitelist includes the given tool.
func (p *Persona) HasTool(name string) bool {
	for _, t := range p.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// InferArtifactType returns the default artifact type for a persona's output.
func (p *Persona) InferArtifactType() ArtifactType {
	switch p.Type {
	case PersonaWeb:
		return ArtifactSummary
	case PersonaCode:
		return ArtifactCode
	case PersonaDesktop:
		return ArtifactSummary
	default:
		return ArtifactSummary
	}
}
