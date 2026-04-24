package llama

// PersonaType identifies the orchestrator persona.
// Sub-agents no longer have fixed persona types — the orchestrator creates
// them dynamically with custom instructions and tool selections.
type PersonaType string

const PersonaOrchestrator PersonaType = "orchestrator"

// Persona defines the orchestrator's identity, capabilities, and generation config.
type Persona struct {
	Type          PersonaType
	SystemPrompt  string
	Tools         []string // whitelist of tool names
	MaxToolRounds int
	MaxTokens     int
	Temperature   float32
}

// PersonaConfig holds parameters for building persona prompts.
type PersonaConfig struct {
	ProjectDir string
	SandboxID  string
}

// NewOrchestratorPersona creates the orchestrator persona with all tools.
func NewOrchestratorPersona(pcfg PersonaConfig) *Persona {
	defaults := cfg.OrchestratorDefaults()
	tools := PersonaToolNames(PersonaOrchestrator)

	return &Persona{
		Type:          PersonaOrchestrator,
		SystemPrompt:  buildPersonaPrompt(PersonaOrchestrator, pcfg),
		Tools:         tools,
		MaxToolRounds: defaults.MaxToolRounds,
		MaxTokens:     defaults.MaxTokens,
		Temperature:   defaults.Temperature,
	}
}
