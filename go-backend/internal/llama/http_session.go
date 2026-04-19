package llama

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Session manages an inference session via the HTTP engine.
// It tracks conversation history and maintains a session_id for KV cache
// persistence on the llama-server side.
//
// Each agent gets its own Session with a unique session_id.
// llama-server maps session_id → slot, keeping the KV cache warm between calls.
type Session struct {
	engine    *HTTPEngine
	sessionID string
	ctxSize   int
	grammar   string // GBNF grammar for constrained generation (per-session)

	history []Turn
	mu      sync.Mutex
	closed  bool

	// Token count tracking
	totalInputTokens  int
	totalOutputTokens int
}

// Chat sends a user message and streams the model's response.
// Returns a channel of TokenEvent for streaming. The channel closes when done.
func (s *Session) Chat(userMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	prompt := formatUserTurn(userMsg)
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
func (s *Session) FeedResult(modelResponse string, toolResult string, nextUserMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

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
func (s *Session) AppendUserTurn(modelResponse string, userMsg string, opts GenerateOptions) (<-chan TokenEvent, error) {
	if s.closed {
		return nil, fmt.Errorf("session is closed")
	}

	var prompt strings.Builder
	prompt.WriteString(modelResponse)
	prompt.WriteString(endModelTurn)
	prompt.WriteString(formatUserTurn(userMsg))

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
			opts.MaxTokens = cfg.MaxTokens
		}

		var output strings.Builder
		tokenCount := 0
		t0 := time.Now()

		req := CompletionRequest{
			Prompt:       prompt,
			MaxTokens:    opts.MaxTokens,
			Temperature:  opts.Temperature,
			TopP:         opts.TopP,
			TopK:         opts.TopK,
			RepeatPenalty: 1.1,
			CachePrompt:  true,
			SessionID:    s.sessionID,
			Grammar:      s.grammar,
			Stream:       true,
		}

		err := s.engine.completeStream(req, func(token string) bool {
			output.WriteString(token)
			tokenCount++
			ch <- TokenEvent{Token: token}
			return true // continue generating
		})

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

// Close releases the session slot on llama-server.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	// Best-effort cleanup — free the slot's KV cache
	_ = s.freeSlot()

	s.engine.logger.Debug("Session closed, KV cache freed",
		zap.String("sessionId", s.sessionID),
	)
	return nil
}

// freeSlot sends a minimal request to free the slot's KV cache.
func (s *Session) freeSlot() error {
	req := CompletionRequest{
		Prompt:      "",
		MaxTokens:   0,
		CachePrompt: false,
		SessionID:   s.sessionID,
	}
	_, err := s.engine.complete(req)
	return err
}

// SetGrammar sets the GBNF grammar for this session.
func (s *Session) SetGrammar(grammar string) {
	s.grammar = grammar
}
