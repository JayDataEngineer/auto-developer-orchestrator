package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/llama"
)

// Adapter bridges llama.LLMClient (old ChatProvider) to core.LLMProvider.
// Maintains a persistent session for KV cache warmth on local llama-server.
type Adapter struct {
	engine  *llama.LLMClient
	ctxSize int
	mu      sync.Mutex
	session *llama.Session
}

// NewAdapter creates an adapter wrapping an LLMClient.
func NewAdapter(engine *llama.LLMClient, ctxSize int) *Adapter {
	return &Adapter{
		engine:  engine,
		ctxSize: ctxSize,
	}
}

// StreamChat implements core.LLMProvider.
func (a *Adapter) StreamChat(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Create session if needed
	if a.session == nil {
		sess, err := a.engine.NewSession(a.ctxSize)
		if err != nil {
			return nil, fmt.Errorf("adapter: failed to create session: %w", err)
		}
		a.session = sess
	}

	// Ensure system messages are at the beginning.
	// Session rehydration can load messages out of order (e.g. user messages
	// from history before the system prompt). Many GGUF chat templates
	// (including Qwen, Gemma) enforce "system message must be first".
	reordered := reorderSystemFirst(messages)

	// Always set messages from core and trigger generation.
	// For cloud providers this is required (no KV cache).
	// For local llama-server, the session_id still enables KV cache reuse.
	a.session.SetMessages(reordered)
	a.session.SetTools(tools)
	ch := a.session.GenerateStream(ctx, opts)

	return convertEvents(ctx, ch), nil
}

func (a *Adapter) ModelName() string {
	return a.engine.ModelName()
}

func (a *Adapter) ContextSize() int {
	// Use dynamic value from engine if available, fall back to static config
	if cw := a.engine.ContextWindow(); cw > 0 {
		return cw
	}
	return a.ctxSize
}

func (a *Adapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		a.session.Close()
		a.session = nil
	}
}

func convertEvents(ctx context.Context, ch <-chan core.ChatEvent) <-chan core.ChatEvent {
	out := make(chan core.ChatEvent, 256)
	go func() {
		defer close(out)
		for evt := range ch {
			// The session accumulates tool calls internally and sends the
			// serialized JSON in the ChatEventDone's Content field.
			// Parse them back into Deltas so the core agent loop can see them.
			// Note: Gemini sends finish_reason="stop" even with tool calls,
			// so we check for Content (serialized calls) regardless of finish reason.
			if evt.Type == core.ChatEventDone && evt.Content != "" {
				var calls []core.ToolCallResponse
				if err := json.Unmarshal([]byte(evt.Content), &calls); err == nil {
					deltas := make([]core.ToolCallDelta, len(calls))
					for i, tc := range calls {
						deltas[i] = core.ToolCallDelta{
							Index: i,
							ID:    tc.ID,
							Type:  tc.Type,
							Function: core.FunctionCallDelta{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						}
					}
					evt.Deltas = deltas
				}
			}
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// reorderSystemFirst moves all system-role messages to the front of the slice.
// Many GGUF chat templates (Qwen, Gemma, etc.) require the system message at
// position 0. Session rehydration from SQL history can load messages where a
// user message appears before the system prompt, breaking strict templates.
func reorderSystemFirst(msgs []core.Message) []core.Message {
	if len(msgs) <= 1 {
		return msgs
	}
	// Fast path: already correct
	if msgs[0].Role == "system" {
		// Check if any other system messages are out of place
		hasMisplaced := false
		for i := 1; i < len(msgs); i++ {
			if msgs[i].Role == "system" {
				hasMisplaced = true
				break
			}
		}
		if !hasMisplaced {
			return msgs
		}
	}

	// Collect system messages first, then everything else
	var system, rest []core.Message
	for _, m := range msgs {
		if m.Role == "system" {
			system = append(system, m)
		} else {
			rest = append(rest, m)
		}
	}
	if len(system) == 0 {
		return msgs
	}
	return append(system, rest...)
}
