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
// or with extra prefixes (including hyphenated names like test-repo):
//   <|tool_call>call:test-repo:bash{json_args}<tool_call|>
// or the simpler form:
//   <|tool_call>TOOL_NAME{json_args}<tool_call|>
// Note: the closing tag may be either <tool_call|> or <|tool_call|>.
// Prefixes allow hyphens ([\w-]+); the final tool name is word-only (\w+).
var toolCallRe = regexp.MustCompile(
	`<\|tool_call\>(?:[\w-]+:)*(\w+)\s*(\{[^}]*\})\s*<?\|?tool_call\|>`,
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

		args := parseToolArgs(argsStr)

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

	// Fallback: if no tagged tool calls found, try plain-text format
	// The 26B model sometimes outputs create_plan{...} without <|tool_call|> tags
	if len(calls) == 0 {
		return parsePlainTextToolCalls(output)
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

// sanitizeGemmaTokens replaces Gemma 4 special quote/apostrophe tokens
// with their literal equivalents so JSON parsing can succeed.
// Gemma 4 outputs <|"|> instead of " and <|'|> instead of '.
func sanitizeGemmaTokens(s string) string {
	s = strings.ReplaceAll(s, `<|"|>`, `"`)
	s = strings.ReplaceAll(s, `<|'|>`, `'`)
	return s
}

// fixUnquotedKeys quotes unquoted JSON object keys.
// Handles patterns like {url: "x"} → {"url": "x"}
var unquotedKeyRe = regexp.MustCompile(`([{,]\s*)(\w+)\s*:`)

func fixUnquotedKeys(s string) string {
	return unquotedKeyRe.ReplaceAllString(s, `$1"$2":`)
}

// parseToolArgs parses JSON args from a tool call, with fallbacks for malformed JSON.
func parseToolArgs(argsStr string) map[string]interface{} {
	sanitized := sanitizeGemmaTokens(argsStr)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(sanitized), &args); err != nil {
		fixed := fixUnquotedKeys(sanitized)
		if err2 := json.Unmarshal([]byte(fixed), &args); err2 != nil {
			args = map[string]interface{}{"raw": sanitized}
		}
	}
	return args
}

// plainToolCallRe matches plain-text tool calls without special tokens.
// Matches patterns like: create_plan{"steps":["step 1","step 2"]}
// or: delegate_to{"persona":"code","task":"do something"}
var plainToolCallRe = regexp.MustCompile(`(\w+)\s*(\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\})`)

// knownToolNames is the set of valid tool names for plain-text matching.
var knownToolNames = map[string]bool{
	"create_plan": true, "delegate_to": true, "update_plan": true, "synthesize": true,
	"bash": true, "search_web": true, "browse_to": true, "click_element": true,
	"type_text": true, "read_page": true, "yield_artifact": true,
	"computer_use_enable": true, "computer_use_screenshot": true,
	"computer_use_snapshot": true, "computer_use_act": true,
	"desktop_screenshot": true, "desktop_click": true, "desktop_type": true, "desktop_key": true,
}

// parsePlainTextToolCalls is a fallback for when the model outputs tool calls
// as plain text (e.g., create_plan{...}) without the <|tool_call|> wrapper tags.
func parsePlainTextToolCalls(output string) ([]ToolCall, string) {
	var calls []ToolCall
	var cleaned string

	matches := plainToolCallRe.FindAllStringSubmatchIndex(output, -1)
	for _, match := range matches {
		toolName := output[match[2]:match[3]]
		if !knownToolNames[toolName] {
			continue
		}
		argsStr := output[match[4]:match[5]]
		rawMatch := output[match[0]:match[1]]

		args := parseToolArgs(argsStr)

		calls = append(calls, ToolCall{
			ID:   generateToolCallID(len(calls)),
			Name: toolName,
			Args: args,
			Raw:  rawMatch,
		})
	}

	if len(calls) > 0 {
		// Remove matched tool calls from output
		cleaned = plainToolCallRe.ReplaceAllStringFunc(output, func(s string) string {
			// Check if this match is a known tool
			sub := plainToolCallRe.FindStringSubmatch(s)
			if len(sub) >= 2 && knownToolNames[sub[1]] {
				return ""
			}
			return s
		})
		cleaned = strings.TrimSpace(cleaned)
	} else {
		cleaned = output
	}

	return calls, cleaned
}
