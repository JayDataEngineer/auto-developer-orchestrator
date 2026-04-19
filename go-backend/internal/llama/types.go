package llama

// Turn represents a single conversation turn.
type Turn struct {
	Role    string // "user", "model", "system", "tool"
	Content string
}

// TokenEvent represents a streaming token from generation.
type TokenEvent struct {
	Token string
	Err   error
	Done  bool
}

// GenerateOptions controls generation parameters.
type GenerateOptions struct {
	MaxTokens   int
	Temperature float32
	TopP        float32
	TopK        int
}

// DefaultGenerateOptions returns sensible defaults for Gemma 4.
func DefaultGenerateOptions() GenerateOptions {
	return GenerateOptions{
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		TopK:        cfg.TopK,
	}
}
