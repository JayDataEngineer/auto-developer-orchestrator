package llama

// ModelConfig holds all tunable parameters for the agent system.
// Every magic number in the llama package should trace back to these constants.
//
// Gemma 4 26B-A4B specs (IQ4_NL quant, RTX 4090 24GB):
//   - Model VRAM: ~12.5 GB
//   - Max supported context: 256K tokens
//   - Recommended starting context: 32K (unsloth docs)
//   - Available VRAM after model: ~12 GB
//   - MoE with 4B active params → KV cache is smaller than dense 26B
//
// Do NOT scatter hardcoded numbers — reference these constants everywhere.
type ModelConfig struct {
	// Context window
	DefaultContextSize int // KV cache context size for orchestrator/persistent sessions (32K recommended)
	SubAgentContextSize int // KV cache context size for ephemeral sub-agents (4K — smaller = less VRAM)
	MaxContextSize     int // Hard upper limit (256K for Gemma 4 26B-A4B)

	// Generation defaults (model-specific sampling params)
	MaxTokens        int
	Temperature      float32
	TopP             float32
	TopK             int
	RepeatPenalty    float32 // 1.0 = disabled. Qwen3.6 recommends 1.0, Gemma 4 used 1.1
	PresencePenalty  float32 // 0.0 = disabled. Qwen3.6 recommends 1.5 for thinking mode
	MinP             float32 // 0.0 = disabled. Qwen3.6 recommends 0.0

	// Agent loop
	ThinkingBudgetTokens  int // Max thinking/reasoning tokens per turn (0 = unlimited). Small models benefit from ~2048.
	DefaultMaxToolRounds  int // Standard max tool rounds
	BrowserMaxToolRounds  int // Browser/desktop automation needs more rounds
	MaxRetriesPerTool     int // Max retries for transient errors
	ToolExecTimeoutSec    int // Per-tool execution timeout in seconds (0 = no timeout)
	RepetitionWindow      int // Character window for repetition detection
	ToolResultMaxChars    int // Truncate tool results to this many chars
	SynthesisMaxChars     int // Max chars for synthesized orchestrator output

	// Compaction
	CompactionTriggerTurns int // Compact after this many model+tool turn pairs (legacy, replaced by size-based)
	CompactionKeepTurns    int // Preserve this many full turns at the end
	CompactionMaxChars     int // Max chars for the compacted summary block

	// Size-based context management (replaces turn-based triggers)
	MicroCompactThreshold float64 // Trigger micro-compact at this fraction of context (default: 0.70)
	FullCompactThreshold  float64 // Trigger full compact at this fraction of context (default: 0.87)
	MaxCompactionFailures int     // Circuit breaker: stop after N consecutive failures (default: 3)

	// Plan-first workflow
	PlanApprovalEnabled bool // When true, create_plan pauses for user approval before execution

	// VRAM budget (RTX 4090 24GB)
	MaxConcurrentAgents int // Max concurrent agent KV sessions
}

// DefaultModelConfig returns the standard configuration for Gemma 4 26B on RTX 4090.
func DefaultModelConfig() ModelConfig {
	return ModelConfig{
		// Context window — 32K is the sweet spot for responsiveness + capacity
		DefaultContextSize: 32768,
		SubAgentContextSize: 16384, // 16K — research sub-agents need room for large scrape results
		MaxContextSize:     262144, // 256K — Gemma 4 26B-A4B max

		// Generation — defaults work for both Gemma 4 and Qwen 3.6
		// Qwen 3.6 thinking mode: temp=1.0, top_p=0.95, top_k=20, presence_penalty=1.5, repeat_penalty=1.0
		// Gemma 4 thinking mode: temp=1.0, top_p=0.95, top_k=64, presence_penalty=0, repeat_penalty=1.1
		MaxTokens:       4096,
		Temperature:     1.0,
		TopP:            0.95,
		TopK:            20,
		RepeatPenalty:   1.0,
		PresencePenalty: 1.5,
		MinP:            0.0,

		// Agent loop
		ThinkingBudgetTokens:  0, // 0 = unlimited — trust the model to self-regulate
		DefaultMaxToolRounds:  20,
		BrowserMaxToolRounds:  30,
		MaxRetriesPerTool:     3,
		ToolExecTimeoutSec:    300, // 5 minutes per tool — MCP research + browser scraping can be slow
		RepetitionWindow:      100,
		ToolResultMaxChars:    6000,
		SynthesisMaxChars:     4000,

		// Compaction — legacy turn-based (kept as fallback)
		CompactionTriggerTurns: 16,
		CompactionKeepTurns:    4,
		CompactionMaxChars:     3000,

		// Size-based context management
		MicroCompactThreshold: 0.55, // 55% → clear old tool results early (16K: ~9K, 32K: ~18K)
		FullCompactThreshold:  0.75, // 75% → LLM summary + trim (16K: ~12K, 32K: ~24K)
		MaxCompactionFailures: 3,    // circuit breaker after 3 consecutive failures

		// VRAM — 24GB RTX 4090: 12.5GB model + 32K orchestrator KV ≈ 3-4GB + 4K sub-agent KV ≈ 0.5GB
		MaxConcurrentAgents: 3, // orchestrator + 1 sub-agent comfortably, 2 sub-agents possible
	}
}

// cfg is the active model configuration (package-level singleton).
// Loaded once at startup, read-only after that.
var cfg = DefaultModelConfig()

// SetModelConfig replaces the active configuration. Call before any agent creation.
func SetModelConfig(c ModelConfig) {
	cfg = c
}

// GetModelConfig returns the active configuration.
func GetModelConfig() ModelConfig {
	return cfg
}

// PersonaDefaults returns per-persona overrides on top of the base ModelConfig.
// Temperatures are persona-specific: code needs precision, orchestrator needs creativity.
type PersonaDefaults struct {
	MaxToolRounds int
	MaxTokens     int
	Temperature   float32
}

// PersonaConfig returns the generation defaults for a given persona type.
func (c ModelConfig) PersonaConfig(t PersonaType) PersonaDefaults {
	switch t {
	case PersonaOrchestrator:
		return PersonaDefaults{
			MaxToolRounds: 0, // 0 = unlimited, let the model work until done
			MaxTokens:     16384,
			Temperature:   0.7,
		}
	case PersonaWeb:
		return PersonaDefaults{
			MaxToolRounds: c.DefaultMaxToolRounds,
			MaxTokens:     2048,
			Temperature:   0.4, // focused for search/browse tasks
		}
	case PersonaCode:
		return PersonaDefaults{
			MaxToolRounds: c.DefaultMaxToolRounds,
			MaxTokens:     2048,
			Temperature:   0.3, // precise for code execution
		}
	case PersonaDesktop:
		return PersonaDefaults{
			MaxToolRounds: c.BrowserMaxToolRounds, // desktop needs many rounds
			MaxTokens:     2048,
			Temperature:   0.4,
		}
	case PersonaMCP:
		return PersonaDefaults{
			MaxToolRounds: c.DefaultMaxToolRounds,
			MaxTokens:     4096, // MCP results can be large
			Temperature:   0.3,  // precise for structured data
		}
	case PersonaResearch:
		return PersonaDefaults{
			MaxToolRounds: 0,    // 0 = unlimited, deep research needs room
			MaxTokens:     8192, // Long reports with citations
			Temperature:   0.4,  // Factual, not creative
		}
	default:
		return PersonaDefaults{
			MaxToolRounds: c.DefaultMaxToolRounds,
			MaxTokens:     c.MaxTokens,
			Temperature:   c.Temperature,
		}
	}
}
