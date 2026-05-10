package ask

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// PendingQuestions is a global registry of questions waiting for user responses.
// The ask_user tool registers here, and the HTTP handler for /api/pux/user-response resolves.
var PendingQuestions = &pendingRegistry{
	entries: make(map[string]chan string),
}

type pendingRegistry struct {
	mu      sync.Mutex
	entries map[string]chan string // key = questionID, value = response channel
}

func (r *pendingRegistry) Register(id string) chan string {
	ch := make(chan string, 1)
	r.mu.Lock()
	r.entries[id] = ch
	r.mu.Unlock()
	return ch
}

func (r *pendingRegistry) Resolve(id, response string) bool {
	r.mu.Lock()
	ch, ok := r.entries[id]
	if ok {
		delete(r.entries, id)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	ch <- response
	return true
}

// AskUserTool lets the AI ask the user a question and wait for a response.
// Supports multiple choice options or free-text input.
// Blocks until the user responds via the /api/pux/user-response endpoint.
type AskUserTool struct {
	subscriber chan<- core.AgentEvent // injected at creation time
}

func NewAskUserTool(subscriber chan<- core.AgentEvent) *AskUserTool {
	return &AskUserTool{subscriber: subscriber}
}

func (t *AskUserTool) Name() string { return "ask_user" }
func (t *AskUserTool) Description() string {
	return "Ask the user a question and wait for their response. Use this when you need clarification, a decision, or input from the user before proceeding. Supports multiple-choice options or free-text."
}

func (t *AskUserTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"question": {"type": "string", "description": "The question to ask the user"},
			"options": {"type": "array", "items": {"type": "string"}, "description": "Multiple choice options. If empty, the user can type a free-text response."},
			"allow_free_text": {"type": "boolean", "description": "If true, user can type a custom answer even when options are provided (default: true)"},
			"default": {"type": "string", "description": "Default answer if user just presses Enter"}
		},
		"required": ["question"]
	}`)
}

func (t *AskUserTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	question, _ := args["question"].(string)
	if question == "" {
		return nil, fmt.Errorf("ask_user: question is required")
	}

	// Extract options
	var options []string
	if raw, ok := args["options"].([]interface{}); ok {
		for _, o := range raw {
			if s, ok := o.(string); ok {
				options = append(options, s)
			}
		}
	}

	allowFreeText := true
	if v, ok := args["allow_free_text"].(bool); ok {
		allowFreeText = v
	}
	defaultAnswer, _ := args["default"].(string)

	// Generate unique question ID
	questionID := fmt.Sprintf("q_%d", time.Now().UnixNano())

	// Register pending question
	responseCh := PendingQuestions.Register(questionID)

	// Emit SSE event to the TUI
	core.SendEvent(t.subscriber, core.AgentEvent{
		Type: "user_question",
		Data: core.AgentEventData{
			ToolID:   questionID,
			ToolName: "ask_user",
			ToolArgs: map[string]any{
				"questionId":      questionID,
				"question":        question,
				"options":         options,
				"allowFreeText":   allowFreeText,
				"default":         defaultAnswer,
			},
		},
	})

	// Block until response arrives or context cancels (5 minute timeout)
	select {
	case response := <-responseCh:
		return map[string]any{
			"response": response,
		}, nil
	case <-ctx.Done():
		PendingQuestions.Resolve(questionID, "") // cleanup
		return nil, fmt.Errorf("ask_user: cancelled (user did not respond)")
	case <-time.After(5 * time.Minute):
		PendingQuestions.Resolve(questionID, "") // cleanup
		return nil, fmt.Errorf("ask_user: timed out after 5 minutes waiting for user response")
	}
}
