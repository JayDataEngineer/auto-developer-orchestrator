package sensitive

import (
	"regexp"
	"strings"
)

// ScrubPatterns defines regex patterns for common secrets that should be
// redacted from SSE responses before they reach the frontend.
// Each pattern maps to a human-readable label for the redaction marker.
var ScrubPatterns = []struct {
	Pattern *regexp.Regexp
	Label   string
}{
	// AWS keys (AKIA... 20 chars after prefix)
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), "AWS_ACCESS_KEY"},
	// AWS secret keys (40-char base64 after known prefixes)
	{regexp.MustCompile(`(?i)(?:aws_secret_access_key|aws_secret)\s*[=:]\s*["']?([A-Za-z0-9/+=]{40})["']?`), "AWS_SECRET_KEY"},
	// GitHub tokens (ghp_, gho_, ghu_, ghs_, ghr_)
	{regexp.MustCompile(`gh[posur]_[A-Za-z0-9_]{36,}`), "GITHUB_TOKEN"},
	// Generic API keys — catches common patterns like "api_key: sk-..."
	{regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|access[_-]?key)\s*[=:]\s*["']?([A-Za-z0-9_\-]{20,})["']?`), "API_KEY"},
	// OpenRouter / OpenAI keys (sk-...)
	{regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`), "API_KEY"},
	// Anthropic keys (sk-ant-...)
	{regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-]{20,}`), "ANTHROPIC_KEY"},
	// Bearer tokens in Authorization headers
	{regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9\-._~+/]+=*`), "BEARER_TOKEN"},
	// Private keys (PEM block content — the key lines, not the markers)
	{regexp.MustCompile(`-----\s*BEGIN\s+(?:RSA\s+)?PRIVATE\s+KEY-----[\s\S]*?-----\s*END\s+(?:RSA\s+)?PRIVATE\s+KEY-----`), "PRIVATE_KEY"},
	// Connection strings with passwords
	{regexp.MustCompile(`(?i)(?:mongodb|postgres|mysql|redis|amqp)://[^:@\s]+:([^@\s]{8,})@`), "DB_PASSWORD"},
	// Generic password in env/config assignments
	{regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[=:]\s*["']([^\s"']{8,})["']?`), "PASSWORD"},
	// Slack tokens/bot tokens
	{regexp.MustCompile(`xox[bpsa]-[0-9a-zA-Z\-]{10,}`), "SLACK_TOKEN"},
	// Telegram bot tokens (123456:ABC-DEF...)
	{regexp.MustCompile(`[0-9]{8,10}:[A-Za-z0-9_\-]{35}`), "TELEGRAM_TOKEN"},
	// Discord bot tokens
	{regexp.MustCompile(`[A-Za-z0-9_\-]{24}\.[A-Za-z0-9_\-]{6}\.[A-Za-z0-9_\-]{27}`), "DISCORD_TOKEN"},
	// Google API keys (AIza...)
	{regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`), "GOOGLE_API_KEY"},
	// Gemini API keys
	{regexp.MustCompile(`(?i)(?:gemini|google)\s*(?:api[_-]?key)\s*[=:]\s*["']?([A-Za-z0-9_\-]{30,})["']?`), "GEMINI_KEY"},
}

// ScrubText redacts known secret patterns from a string, replacing them
// with [REDACTED_<label>] markers. Returns the scrubbed string.
func ScrubText(text string) string {
	for _, sp := range ScrubPatterns {
		text = sp.Pattern.ReplaceAllStringFunc(text, func(match string) string {
			return "[REDACTED_" + sp.Label + "]"
		})
	}
	return text
}

// ScrubMap recursively scrubs all string values in a map.
// Returns a new map — does not modify the original.
func ScrubMap(data map[string]any) map[string]any {
	result := make(map[string]any, len(data))
	for k, v := range data {
		result[k] = scrubValue(v)
	}
	return result
}

func scrubValue(v any) any {
	switch val := v.(type) {
	case string:
		return ScrubText(val)
	case map[string]any:
		return ScrubMap(val)
	case []any:
		result := make([]any, len(val))
		for i, elem := range val {
			result[i] = scrubValue(elem)
		}
		return result
	default:
		return v
	}
}

// ShouldScrubEvent returns true if an SSE event type typically contains
// user-visible text that should be scrubbed. Not all events need scrubbing
// (e.g., agent_start has no text content).
func ShouldScrubEvent(eventType string) bool {
	switch eventType {
	case "text_delta", "thinking_delta", "tool_execution_end",
		"tool_update", "error", "subagent_end":
		return true
	default:
		return false
	}
}

// IsSecretPlaceholder checks if a string is one of our redaction markers.
// Used to avoid double-scrubbing.
func IsSecretPlaceholder(s string) bool {
	return strings.HasPrefix(s, "[REDACTED_") && strings.HasSuffix(s, "]")
}
