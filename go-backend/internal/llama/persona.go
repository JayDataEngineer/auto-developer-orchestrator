package llama

// PersonaType identifies a specific agent persona.
type PersonaType string

const (
	PersonaOrchestrator PersonaType = "orchestrator"
	PersonaWeb          PersonaType = "web"
	PersonaCode         PersonaType = "code"
	PersonaDesktop      PersonaType = "desktop"
	PersonaMCP          PersonaType = "mcp"
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
func NewPersona(t PersonaType, pcfg PersonaConfig) *Persona {
	defaults := cfg.PersonaConfig(t)
	tools := PersonaToolNames(t)
	if tools == nil {
		return nil
	}

	return &Persona{
		Type:          t,
		SystemPrompt:  buildPersonaPrompt(t, pcfg),
		Tools:         tools,
		MaxToolRounds: defaults.MaxToolRounds,
		MaxTokens:     defaults.MaxTokens,
		Temperature:   defaults.Temperature,
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
	case PersonaMCP:
		return ArtifactSummary
	default:
		return ArtifactSummary
	}
}
