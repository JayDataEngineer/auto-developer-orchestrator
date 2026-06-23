package tools

import (
	"encoding/json"
	"regexp"
	"strings"
)

// InjectionPattern matches lines that look like prompt-injection attempts
// embedded in tool output. The Fable/Mythos taxonomy calls this failure
// mode "instruction_following_on_untrusted_input" — baseline 0.5%.
//
// We don't try to catch every injection; we just tag the obvious ones so
// the model has a fighting chance to recognize "this is data, not instructions."
// The wrapper wraps the line in <suspicious_input> tags, which the model
// can recognize as quarantined content.
var injectionLinePatterns = []*regexp.Regexp{
	// Imperative override at start of line.
	regexp.MustCompile(`(?i)^(ignore|disregard|forget)\s+(all\s+)?(previous|prior|above|earlier)\s+`),
	// System-role impersonation.
	regexp.MustCompile(`(?i)^(system|assistant|admin|developer|root)\s*:\s*`),
	// New-instructions framing.
	regexp.MustCompile(`(?i)^(new|updated|important)\s+instructions?\s*:`),
	// Bare "important:" prefix that then issues a command.
	regexp.MustCompile(`(?i)^important\s*:\s*(do|execute|run|call|delete|rm|send|write|update)\b`),
	// "/..." slash commands often injected to trick agent frameworks.
	regexp.MustCompile(`(?i)^/(quit|exit|cancel|abort|clear|reset|sudo|admin|jailbreak|dan)\b`),
}

// QuarantineUntrustedInstructions scans text for prompt-injection patterns
// at the start of lines and wraps the offending lines in <suspicious_input>
// tags. Returns the original text if no patterns match.
//
// Why a regex pre-pass and not an LLM classifier: cost and latency. Every
// MCP/browser call would otherwise cost an extra model invocation. The
// regex catches the high-confidence patterns; the diligence prompts (PR1)
// handle the rest by training the model to treat tool output with suspicion.
func QuarantineUntrustedInstructions(text string) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	matched := false
	for i, line := range lines {
		// Strip a small amount of leading whitespace so leading-space
		// injections don't dodge the anchor.
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) == 0 {
			continue
		}
		// Apply the patterns against the trimmed line.
		tri := false
		for _, re := range injectionLinePatterns {
			if re.MatchString(trimmed) {
				tri = true
				break
			}
		}
		if tri {
			lines[i] = "<suspicious_input>" + line + "</suspicious_input>"
			matched = true
		}
	}
	if !matched {
		return text
	}
	return strings.Join(lines, "\n")
}

// QuarantineResult walks a tool result and applies the injection wrapper to
// every string it finds, in place. Handles maps (recursively) and slices;
// leaves other types alone. This is the entry point for tool wrappers.
//
// Type preservation: if no patterns match anywhere, the original value is
// returned unchanged so callers that type-assert (e.g. browser tests checking
// *PageContext) keep working. When patterns DO match, the result is returned
// as a generic map[string]any (JSON-roundtripped from the original).
//
// The walk is depth-bounded to 4 levels to avoid pathological deeply-nested
// payloads (and to keep the cost predictable for very large tool outputs).
func QuarantineResult(result any) any {
	// Cheap pre-check: marshal to JSON, run regexes over the blob. If clean,
	// return the original — preserves the Go type for downstream consumers.
	if result == nil {
		return result
	}
	if s, ok := result.(string); ok {
		return QuarantineUntrustedInstructions(s)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return result
	}
	if !anyInjectionPatternMatches(string(data)) {
		return result
	}
	// Suspicion detected — convert to a generic map and walk it.
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return result
	}
	return quarantineWalk(generic, 0)
}

// anyInjectionPatternMatches returns true if any injection pattern matches
// anywhere in the text (not just line-anchored). Used as a fast pre-check
// before walking; if false, we know the walk won't find anything.
func anyInjectionPatternMatches(text string) bool {
	// We check the line-anchored patterns over each line, plus a relaxed
	// mid-line check for "ignore previous" / "new instructions:" since those
	// are the high-confidence markers worth the false-positive risk.
	for _, line := range strings.Split(text, "\\n") {
		trimmed := strings.TrimLeft(line, " \\t")
		for _, re := range injectionLinePatterns {
			if re.MatchString(trimmed) {
				return true
			}
		}
	}
	// Relaxed mid-line check for the two highest-confidence patterns.
	if strings.Contains(strings.ToLower(text), "ignore previous") {
		return true
	}
	if strings.Contains(strings.ToLower(text), "ignore all previous") {
		return true
	}
	return false
}

func quarantineWalk(v any, depth int) any {
	if depth > 4 {
		return v
	}
	switch x := v.(type) {
	case string:
		return QuarantineUntrustedInstructions(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = quarantineWalk(vv, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = quarantineWalk(vv, depth+1)
		}
		return out
	default:
		// Structs (e.g. browser.PageContext) and other typed values: round-trip
		// through JSON to coerce into a generic map, then quarantine recursively.
		// If JSON marshaling fails (unmarshalable types), leave unchanged.
		if v == nil {
			return v
		}
		data, err := json.Marshal(v)
		if err != nil {
			return v
		}
		// Fast path: scalars (numbers, bools) round-trip to non-map JSON; skip.
		first := strings.TrimSpace(string(data))
		if len(first) == 0 || first[0] != '{' && first[0] != '[' {
			return v
		}
		var generic any
		if err := json.Unmarshal(data, &generic); err != nil {
			return v
		}
		return quarantineWalk(generic, depth+1)
	}
}

// MarshalForLog serializes a result for debug logging without modifying it.
// Helper for tool wrappers that want to log pre-quarantine content.
func MarshalForLog(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "<unmarshalable>"
	}
	if len(data) > 200 {
		return string(data[:200]) + "..."
	}
	return string(data)
}
