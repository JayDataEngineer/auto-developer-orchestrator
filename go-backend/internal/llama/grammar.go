package llama

import (
	"fmt"
	"strings"
)

// PersonaGrammar returns a GBNF grammar string that constrains tool calls
// to valid names for the given persona. Returns empty string if no tools.
//
// The grammar allows:
//   - Free text (thinking, explanations, special tokens like <|channel|>thought)
//   - Constrained tool calls: when the model outputs <|tool_call>, the tool name
//     MUST be from the persona's whitelist. Invalid tool names become impossible.
func PersonaGrammar(t PersonaType) string {
	tools := PersonaToolNames(t)
	if len(tools) == 0 {
		return ""
	}
	return BuildToolGrammar(tools)
}

// BuildToolGrammar generates a GBNF grammar that constrains tool calls to the
// given tool names while allowing free text.
//
// How it works:
//   - The "text" rule allows any characters EXCEPT the start of <|tool_call>.
//     Special tokens like <|channel|>thought, <end_of_turn>, <|"|> all pass through.
//   - The "toolcall" rule forces valid tool names from the whitelist.
//   - The model can output any mix of text and tool calls.
//
// This eliminates tool name hallucination — the model physically cannot output
// <|tool_call>call:math_calculator{...} because "math_calculator" is not in
// the toolname alternatives. Only whitelisted names have non-zero probability.
func BuildToolGrammar(toolNames []string) string {
	var sb strings.Builder

	// root: any mix of text and constrained tool calls
	sb.WriteString("root ::= (text | toolcall)*\n")

	// text: allow any characters except the start of <|tool_call>
	// Layer by layer: [^<] | <[^|] | <|[^t] | <|t[^o] | <|to[^o] | <|too[^l] | <|tool[^_]
	// This lets <end_of_turn>, <|channel|>thought, <|"|>, etc. pass through freely.
	// Only <|tool_call> gets routed to the toolcall rule.
	sb.WriteString("text ::= [^<] | \"<\" [^|] | \"<|\" [^t] | \"<|t\" [^o] | \"<|to\" [^o] | \"<|too\" [^l] | \"<|tool\" [^_]\n")

	// toolcall: <|tool_call>call:TOOL_NAME{JSON}<tool_call|>
	// label* handles namespace prefixes like "call:" or "call:test-repo:"
	sb.WriteString("toolcall ::= \"<|tool_call>\" label* toolname space* \"{\" json \"}\" space* endtag\n")

	// Label prefix (e.g., "call:", "call:test-repo:")
	sb.WriteString("label ::= [a-zA-Z][a-zA-Z0-9_-]* \":\"\n")

	// Tool name: ONLY whitelisted names have non-zero probability
	sb.WriteString("toolname ::= ")
	for i, name := range toolNames {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(fmt.Sprintf("%q", name))
	}
	sb.WriteString("\n")

	// Whitespace
	sb.WriteString("space ::= [ \\t\\n]\n")

	// JSON: balanced braces with strings, escapes, and nesting
	// Permissive — validates structure but not schema (the tool parser handles schema)
	sb.WriteString("json ::= ( [^{}\"\\\\] | \"\\\\\" [^\\n] | string | \"{\" json \"}\" )*\n")
	sb.WriteString("string ::= \"\\\"\" [^\"\\\\]* (\"\\\\\" [^\\n] [^\"\\\\]*)* \"\\\"\"\n")

	// End tag variants (Gemma 4 uses both)
	sb.WriteString("endtag ::= \"<tool_call|>\" | \"<|tool_call|>\"\n")

	return sb.String()
}
