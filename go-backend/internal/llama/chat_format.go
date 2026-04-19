package llama

import "strings"

// Gemma 4 chat format tokens.
const (
	startUserTurn   = "<start_of_turn>user\n"
	startModelTurn  = "<start_of_turn>model\n"
	startSystemTurn = "<start_of_turn>system\n"
	endTurn         = "<end_of_turn>\n"
	endModelTurn    = "<end_of_turn>\n"
)

// formatSystemPrompt wraps a system message in Gemma 4 tags.
func formatSystemPrompt(content string) string {
	var sb strings.Builder
	sb.WriteString(startSystemTurn)
	sb.WriteString(content)
	sb.WriteString(endTurn)
	return sb.String()
}

// formatUserTurn wraps a user message in Gemma 4 tags and starts the model's turn.
func formatUserTurn(content string) string {
	var sb strings.Builder
	sb.WriteString(startUserTurn)
	sb.WriteString(content)
	sb.WriteString(endTurn)
	sb.WriteString(startModelTurn)
	return sb.String()
}

// formatUserTurnWithResult wraps a tool result + continuation nudge in a user turn.
// The nudge is minimal — sub-agents take small steps, the orchestrator handles planning.
func formatUserTurnWithResult(toolResult string, nextUserMsg string) string {
	var sb strings.Builder
	sb.WriteString(startUserTurn)
	sb.WriteString("[tool_result]\n")
	sb.WriteString(toolResult)
	sb.WriteString("\n[/tool_result]\n\n")
	sb.WriteString("Call the next tool.")
	if nextUserMsg != "" {
		sb.WriteString("\n")
		sb.WriteString(nextUserMsg)
	}
	sb.WriteString(endTurn)
	sb.WriteString(startModelTurn)
	return sb.String()
}

// BuildFullPrompt builds a complete prompt from system message and conversation history.
// This is used for naive/full-context mode (not incremental).
func BuildFullPrompt(system string, turns []Turn) string {
	var sb strings.Builder
	if system != "" {
		sb.WriteString(startSystemTurn)
		sb.WriteString(system)
		sb.WriteString(endTurn)
	}
	for _, turn := range turns {
		switch turn.Role {
		case "user":
			sb.WriteString(startUserTurn)
			sb.WriteString(turn.Content)
			sb.WriteString(endTurn)
		case "model":
			sb.WriteString(startModelTurn)
			sb.WriteString(turn.Content)
			sb.WriteString(endTurn)
		case "tool":
			// Tool results are formatted as user messages
			sb.WriteString(startUserTurn)
			sb.WriteString(turn.Content)
			sb.WriteString(endTurn)
		}
	}
	// Start model's turn
	sb.WriteString(startModelTurn)
	return sb.String()
}
