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
	tools   []llama.OpenAITool
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

	llamaTools := make([]llama.OpenAITool, len(tools))
	for i, t := range tools {
		llamaTools[i] = llama.OpenAITool{
			Type: t.Type,
			Function: llama.FunctionDef{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		}
	}

	llamaOpts := llama.GenerateOptions{
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
		TopP:        opts.TopP,
		TopK:        opts.TopK,
	}

	// Create session if needed
	if a.session == nil {
		sess, err := a.engine.NewSession(a.ctxSize)
		if err != nil {
			return nil, fmt.Errorf("adapter: failed to create session: %w", err)
		}
		a.session = sess
		a.tools = llamaTools
	}

	// Convert all core messages to llama messages.
	// This ensures the session always has the exact same view as the core session,
	// including after compaction or any other modification.
	llamaMsgs := make([]llama.Message, 0, len(messages))
	for _, m := range messages {
		lm := llama.Message{
			Role:    m.Role,
			Content: m.Content,
		}
		if m.ToolCallID != "" {
			lm.ToolCallID = m.ToolCallID
		}
		if m.Name != "" {
			lm.Name = m.Name
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				lm.ToolCalls = append(lm.ToolCalls, llama.ToolCallResponse{
					ID:   tc.ID,
					Type: tc.Type,
					Function: llama.FunctionCallData{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		llamaMsgs = append(llamaMsgs, lm)
	}

	// Ensure system messages are at the beginning.
	// Session rehydration can load messages out of order (e.g. user messages
	// from history before the system prompt). Many GGUF chat templates
	// (including Qwen, Gemma) enforce "system message must be first".
	llamaMsgs = reorderSystemFirst(llamaMsgs)

	// Always set messages from core and trigger generation.
	// For cloud providers this is required (no KV cache).
	// For local llama-server, the session_id still enables KV cache reuse.
	a.session.SetMessages(llamaMsgs)
	a.session.SetTools(llamaTools)
	ch := a.session.GenerateStream(llamaOpts)

	return convertEvents(ch), nil
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

func convertEvents(ch <-chan llama.ChatEvent) <-chan core.ChatEvent {
	out := make(chan core.ChatEvent, 256)
	go func() {
		defer close(out)
		for evt := range ch {
			coreEvt := core.ChatEvent{
				Type:    core.ChatEventType(evt.Type),
				Content: evt.Content,
				Finish:  core.FinishReason(evt.Finish),
				Err:     evt.Err,
			}
			if evt.Usage != nil {
				coreEvt.Usage = &core.StreamUsage{
					PromptTokens:     evt.Usage.PromptTokens,
					CompletionTokens: evt.Usage.CompletionTokens,
				}
			}
			if evt.Delta != nil {
				coreEvt.Deltas = []core.ToolCallDelta{{
					Index:    evt.Delta.Index,
					ID:       evt.Delta.ID,
					Type:     evt.Delta.Type,
					Function: core.FunctionCallDelta{
						Name:      evt.Delta.Function.Name,
						Arguments: evt.Delta.Function.Arguments,
					},
				}}
			}
			// The session accumulates tool calls internally and sends the
			// serialized JSON in the ChatEventDone's Content field.
			// Parse them back into Deltas so the core agent loop can see them.
			// Note: Gemini sends finish_reason="stop" even with tool calls,
			// so we check for Content (serialized calls) regardless of finish reason.
			if evt.Type == llama.ChatEventDone && evt.Content != "" {
				var calls []llama.ToolCallResponse
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
					coreEvt.Deltas = deltas
				}
			}
			out <- coreEvt
		}
	}()
	return out
}

// reorderSystemFirst moves all system-role messages to the front of the slice.
// Many GGUF chat templates (Qwen, Gemma, etc.) require the system message at
// position 0. Session rehydration from SQL history can load messages where a
// user message appears before the system prompt, breaking strict templates.
func reorderSystemFirst(msgs []llama.Message) []llama.Message {
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
	var system, rest []llama.Message
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
