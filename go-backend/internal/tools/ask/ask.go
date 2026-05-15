package ask

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// AskUserTool lets the AI ask the user a question and wait for a response.
// Supports multiple choice options or free-text input.
// Blocks until the user responds via the POST /api/pux/decision endpoint.
//
// Contract 3 compliance: does NOT take an SSE subscriber in the constructor.
// Instead, retrieves it from the context (set by AgentLoop) when needed.
// Tools must not have direct dependencies on the event stream.
type AskUserTool struct{}

func NewAskUserTool() *AskUserTool {
	return &AskUserTool{}
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

	questionID := fmt.Sprintf("q_%d", time.Now().UnixNano())

	subscriber, _ := ctx.Value(core.SubscriberKey{}).(chan core.AgentEvent)
	resp, err := core.GlobalDecisions.WaitForDecision(ctx, core.DecisionRequest{
		ID:            questionID,
		SourceTool:    "ask_user",
		Title:         question,
		Hint:          core.HintQuestion,
		Options:       options,
		AllowFreeText: allowFreeText,
	}, subscriber, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("ask_user: %w", err)
	}

	return map[string]any{"response": resp.Value}, nil
}
