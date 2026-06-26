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

// AllTools returns every stateless tool in the ask package.
// Tools that need runtime deps are not included here — wire them via the
// orchestrator's tool composer instead.
func AllTools() []core.Tool {
	return []core.Tool{
		NewAskUserTool(),
	}
}

func (t *AskUserTool) Name() string { return "ask_user" }

// TimeoutHint returns 0 — ask_user waits indefinitely for user response.
func (t *AskUserTool) TimeoutHint() time.Duration { return 0 }
func (t *AskUserTool) Description() string {
	return "Ask the user a question with multiple-choice options and wait for their response. Use this when you need a decision or clarification from the user. You MUST provide at least 2 specific, meaningful options. If none of the options fit, the user can type a custom answer."
}

func (t *AskUserTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"question": {"type": "string", "description": "The question to ask the user"},
			"options": {"type": "array", "items": {"type": "string"}, "minItems": 2, "description": "REQUIRED: At least 2 specific, meaningful choices. The user selects one or types a custom answer if none fit."},
			"default": {"type": "string", "description": "Default answer if user just presses Enter"}
		},
		"required": ["question", "options"]
	}`)
}

func (t *AskUserTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	question, _ := args["question"].(string)
	if question == "" {
		return nil, fmt.Errorf("ask_user: question is required")
	}

	// Extract and validate options — must have at least 2
	var options []string
	if raw, ok := args["options"].([]interface{}); ok {
		for _, o := range raw {
			if s, ok := o.(string); ok {
				options = append(options, s)
			}
		}
	}
	if len(options) < 2 {
		return nil, fmt.Errorf("ask_user: must provide at least 2 options, got %d. Provide specific, meaningful choices so the user can make a decision", len(options))
	}

	allowFreeText := true

	questionID := fmt.Sprintf("q_%d", time.Now().UnixNano())

	subscriber, _ := ctx.Value(core.SubscriberKey{}).(chan<- core.AgentEvent)
	resp, err := core.GlobalDecisions.WaitForDecision(ctx, core.DecisionRequest{
		ID:            questionID,
		SourceTool:    "ask_user",
		Title:         question,
		Hint:          core.HintQuestion,
		Options:       options,
		AllowFreeText: allowFreeText,
	}, subscriber, 0)
	if err != nil {
		return nil, fmt.Errorf("ask_user: %w", err)
	}

	return map[string]any{"response": resp.Value}, nil
}
