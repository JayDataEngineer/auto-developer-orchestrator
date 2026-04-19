package llama

import (
	"fmt"
	"strings"
	"sync"
	"time"

	llamago "github.com/tcpipuk/llama-go"
	"go.uber.org/zap"
)

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

// Session wraps a llama.Context with chat history and streaming generation.
// Each agent gets its own Session with a persistent KV cache in VRAM.
// Session is NOT thread-safe — use from a single goroutine.
type Session struct {
	ctx     *llamago.Context
	engine  *Engine
	history []Turn
	mu      sync.Mutex
	closed  bool

	// Token count tracking
	totalInputTokens  int
	totalOutputTokens int
}

// Chat sends a user message and streams the model's response.
// Returns a channel of TokenEvent for streaming. The channel closes when done.
// The KV cache persists — subsequent calls only process new tokens.
func (s *Session) Chat(userMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	// Format the new tokens to append to the context
	prompt := formatUserTurn(userMsg)

	// Track in history
	s.history = append(s.history, Turn{Role: "user", Content: userMsg})

	ch := s.generateStream(prompt, opts)
	return ch, nil
}

// ChatWithSystem sends a system + first user message. Used for the initial prompt.
func (s *Session) ChatWithSystem(system string, userMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	prompt := formatSystemPrompt(system) + formatUserTurn(userMsg)

	s.history = append(s.history,
		Turn{Role: "system", Content: system},
		Turn{Role: "user", Content: userMsg},
	)

	ch := s.generateStream(prompt, opts)
	return ch, nil
}

// FeedResult appends a tool result and continues generation.
// The KV cache from the previous turn is still warm — only new tokens are processed.
func (s *Session) FeedResult(modelResponse string, toolResult string, nextUserMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	// Append: model response end tag + user turn with tool result
	var prompt strings.Builder
	prompt.WriteString(modelResponse)
	prompt.WriteString(endModelTurn)
	prompt.WriteString(formatUserTurnWithResult(toolResult, nextUserMsg))

	s.history = append(s.history,
		Turn{Role: "model", Content: modelResponse},
		Turn{Role: "user", Content: toolResult},
	)

	ch := s.generateStream(prompt.String(), opts)
	return ch, nil
}

// FeedContinue appends the model response end tag + a new user message.
// Used when the model finishes generating and the user sends another message.
// NOTE: This adds both the model response and user message to history.
// If the model response was already tracked in history, use AppendUserTurn instead.
func (s *Session) FeedContinue(modelResponse string, userMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	var prompt strings.Builder
	prompt.WriteString(modelResponse)
	prompt.WriteString(endModelTurn)
	prompt.WriteString(formatUserTurn(userMsg))

	s.history = append(s.history,
		Turn{Role: "model", Content: modelResponse},
		Turn{Role: "user", Content: userMsg},
	)

	ch := s.generateStream(prompt.String(), opts)
	return ch, nil
}

// AppendUserTurn closes the previous model turn and appends a new user message.
// Unlike FeedContinue, this does NOT add the model response to history again —
// it assumes the model response was already tracked during generation.
func (s *Session) AppendUserTurn(modelResponse string, userMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	var prompt strings.Builder
	prompt.WriteString(modelResponse)
	prompt.WriteString(endModelTurn)
	prompt.WriteString(formatUserTurn(userMsg))

	// Only add the user turn to history — model response was already tracked
	s.history = append(s.history,
		Turn{Role: "user", Content: userMsg},
	)

	ch := s.generateStream(prompt.String(), opts)
	return ch, nil
}

// generateStream runs the model and returns a channel of tokens.
func (s *Session) generateStream(prompt string, opts GenerateOptions) <-chan TokenEvent {
	ch := make(chan TokenEvent, 256)

	go func() {
		defer close(ch)

		if opts.MaxTokens == 0 {
			opts.MaxTokens = 4096
		}

		var output strings.Builder
		tokenCount := 0
		t0 := time.Now()

		err := s.ctx.GenerateStream(prompt, func(token string) bool {
			output.WriteString(token)
			tokenCount++
			ch <- TokenEvent{Token: token}
			return true // continue generating
		},
			llamago.WithMaxTokens(opts.MaxTokens),
			llamago.WithTemperature(opts.Temperature),
			llamago.WithTopP(opts.TopP),
			llamago.WithTopK(opts.TopK),
		)

		elapsed := time.Since(t0)
		s.totalOutputTokens += tokenCount

		s.engine.logger.Debug("Generation complete",
			zap.Int("tokens", tokenCount),
			zap.Duration("duration", elapsed),
			zap.Float64("tok_per_sec", float64(tokenCount)/elapsed.Seconds()),
		)

		if err != nil {
			ch <- TokenEvent{Err: err, Done: true}
			return
		}

		ch <- TokenEvent{Done: true}
	}()

	return ch
}

// TrackModelResponse adds a model response to history.
// Called after generation completes (no tool calls) so that Continue() can
// properly close the model turn before appending the next user message.
func (s *Session) TrackModelResponse(content string) {
	s.history = append(s.history, Turn{Role: "model", Content: content})
}

// History returns the conversation history.
func (s *Session) History() []Turn {
	return s.history
}

// HistoryLen returns the number of turns in history.
func (s *Session) HistoryLen() int {
	return len(s.history)
}

// TokenCounts returns total input and output token counts.
func (s *Session) TokenCounts() (input, output int) {
	return s.totalInputTokens, s.totalOutputTokens
}

// Close releases the context and frees VRAM.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.ctx != nil {
		s.ctx.Close()
	}
	s.engine.logger.Debug("Session closed, KV cache freed")
	return nil
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
		MaxTokens:   4096,
		Temperature: 0.7,
		TopP:        0.95,
		TopK:        64,
	}
}
