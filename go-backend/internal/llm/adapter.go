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
// Maintains a persistent old-style Session for KV cache warmth.
type Adapter struct {
	engine        *llama.LLMClient
	ctxSize       int
	mu            sync.Mutex
	session       *llama.Session
	tools         []llama.OpenAITool
	isNewSession  bool
}

// NewAdapter creates an adapter wrapping an LLMClient.
func NewAdapter(engine *llama.LLMClient, ctxSize int) *Adapter {
	return &Adapter{
		engine:       engine,
		ctxSize:      ctxSize,
		isNewSession: true,
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

	// Create or reuse session
	if a.session == nil {
		sess, err := a.engine.NewSession(a.ctxSize)
		if err != nil {
			return nil, fmt.Errorf("adapter: failed to create session: %w", err)
		}
		a.session = sess
		a.tools = llamaTools
		a.isNewSession = true
	}

	if a.isNewSession {
		// Find system prompt and user message
		var system string
		var userMessages []string
		for _, m := range messages {
			if m.Role == "system" {
				system = m.Content
			} else if m.Role == "user" {
				userMessages = append(userMessages, m.Content)
			}
		}
		userMsg := ""
		if len(userMessages) > 0 {
			userMsg = userMessages[len(userMessages)-1]
		}

		ch, err := a.session.ChatWithTools(system, userMsg, llamaTools, llamaOpts)
		if err != nil {
			a.session.Close()
			a.session = nil
			return nil, fmt.Errorf("adapter: chat failed: %w", err)
		}
		a.isNewSession = false
		return convertEvents(ch), nil
	}

	// Continuation — find new messages since last call
	// Tool results: send via FeedToolResults
	// User messages: send via FeedUserMessage

	// Check the last message role
	if len(messages) > 0 {
		lastRole := messages[len(messages)-1].Role

		if lastRole == "user" {
			userMsg := messages[len(messages)-1].Content
			ch, err := a.session.FeedUserMessage(userMsg, llamaOpts)
			if err != nil {
				a.session.Close()
				a.session = nil
				return nil, fmt.Errorf("adapter: feed user failed: %w", err)
			}
			return convertEvents(ch), nil
		}

		// Tool results + optional nudge
		// Find last assistant message for tool call IDs
		var assistantMsg llama.Message
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
				assistantMsg = llama.Message{
					Role:    messages[i].Role,
					Content: messages[i].Content,
				}
				for _, tc := range messages[i].ToolCalls {
					assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, llama.ToolCallResponse{
						ID:   tc.ID,
						Type: tc.Type,
						Function: llama.FunctionCallData{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					})
				}
				break
			}
		}

		// Collect tool results and detect goal nudges (user messages injected
		// between the initial prompt and the final assistant response).
		var toolResults []llama.ToolResult
		var goalNudge string
		for i, m := range messages {
			if m.Role == "tool" {
				toolResults = append(toolResults, llama.ToolResult{
					ToolCallID: m.ToolCallID,
					ToolName:   m.Name,
					Content:    m.Content,
				})
			} else if m.Role == "user" {
				lastMsg := messages[len(messages)-1]
				if lastMsg.Role != "user" || i != len(messages)-1 {
					goalNudge = m.Content
				}
			}
		}

		if len(toolResults) > 0 {
			ch, err := a.session.FeedToolResults(assistantMsg, toolResults, goalNudge, llamaOpts)
			if err != nil {
				a.session.Close()
				a.session = nil
				return nil, fmt.Errorf("adapter: feed tool results failed: %w", err)
			}
			return convertEvents(ch), nil
		}
	}

	// Fallback: empty user message (shouldn't happen)
	ch, err := a.session.FeedUserMessage("", llamaOpts)
	if err != nil {
		a.session.Close()
		a.session = nil
		return nil, err
	}
	return convertEvents(ch), nil
}

func (a *Adapter) ModelName() string {
	return a.engine.ModelName()
}

func (a *Adapter) ContextSize() int {
	return a.ctxSize
}

func (a *Adapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		a.session.Close()
		a.session = nil
	}
	a.isNewSession = true
}

func (a *Adapter) Reset() {
	a.Close()
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
			if evt.Type == llama.ChatEventDone && evt.Content != "" && coreEvt.Finish == core.FinishToolCalls {
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
