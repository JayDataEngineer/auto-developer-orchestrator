package llama

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ToolCall represents a parsed tool call from the model's output.
type ToolCall struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Args   map[string]interface{} `json:"args"`
	Raw    string                 `json:"raw"`    // The raw matched text
	Start  int                    `json:"start"`  // Byte offset in output
	End    int                    `json:"end"`    // Byte offset in output
}

// toolCallRe matches Gemma 4's native tool call format:
//   <|tool_call>call:TOOL_NAME{json_args}<tool_call|>
// or with extra prefixes:
//   <|tool_call>call:prefix:TOOL_NAME{json_args}<tool_call|>
// or the simpler form:
//   <|tool_call>TOOL_NAME{json_args}<tool_call|>
// Note: the closing tag may be either <tool_call|> or <|tool_call|>.
var toolCallRe = regexp.MustCompile(
	`<\|tool_call\>(?:\w+:)*(\w+)\s*(\{[^}]*\})\s*<?\|?tool_call\|>`,
)

// toolCallStartRe matches the opening of a tool call (for streaming detection).
var toolCallStartRe = regexp.MustCompile(`<\|tool_call\>`)

// ParseToolCalls extracts all tool calls from the model's output text.
// Returns the tool calls found and the remaining text (with tool calls removed).
func ParseToolCalls(output string) ([]ToolCall, string) {
	var calls []ToolCall
	var cleanText strings.Builder
	lastEnd := 0

	matches := toolCallRe.FindAllStringSubmatchIndex(output, -1)
	for _, match := range matches {
		// Append text before this match
		cleanText.WriteString(output[lastEnd:match[0]])

		// match[2]/match[3] = tool name group
		// match[4]/match[5] = JSON args group
		toolName := output[match[2]:match[3]]
		argsStr := output[match[4]:match[5]]
		rawMatch := output[match[0]:match[1]]

		var args map[string]interface{}
		if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
			// If JSON parse fails, store raw args string
			args = map[string]interface{}{
				"raw": argsStr,
			}
		}

		calls = append(calls, ToolCall{
			ID:    generateToolCallID(len(calls)),
			Name:  toolName,
			Args:  args,
			Raw:   rawMatch,
			Start: match[0],
			End:   match[1],
		})

		lastEnd = match[1]
	}

	// Append remaining text after last match
	if lastEnd < len(output) {
		cleanText.WriteString(output[lastEnd:])
	}

	return calls, cleanText.String()
}

// HasPartialToolCall checks if the output contains an unclosed tool call tag.
// Used during streaming to buffer until the complete tool call is received.
func HasPartialToolCall(output string) bool {
	startCount := strings.Count(output, "<|tool_call>")
	// Count both closing tag variants
	endCount := strings.Count(output, "<tool_call|>") + strings.Count(output, "<|tool_call|>")
	return startCount > endCount
}

// StripToolCallTags removes all tool call tags from text.
// Useful for cleaning up the model's output before displaying to user.
func StripToolCallTags(output string) string {
	output = strings.ReplaceAll(output, "<|tool_call>", "")
	output = strings.ReplaceAll(output, "<|tool_call|>", "")
	output = strings.ReplaceAll(output, "<tool_call|>", "")
	return strings.TrimSpace(output)
}

// generateToolCallID creates a simple unique ID for a tool call.
func generateToolCallID(idx int) string {
	return fmt.Sprintf("tc_%d", idx)
}
