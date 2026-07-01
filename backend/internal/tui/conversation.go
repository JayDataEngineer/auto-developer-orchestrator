// conversation.go holds the pure-function core of the chat state: the turn
// accumulator + the markdown renderer that flattens the conversation into
// the single task_description string the dispatch surface expects.
//
// These functions have no tea.Model / lipgloss / IO dependencies on purpose
// — they're the only bits of the package that are unit-testable in isolation.

package tui

import (
	"strings"
)

// Role is who produced a given turn. The string values are part of the
// contract with renderConversation: "user" and "assistant".
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Turn is one entry in the client-side conversation accumulation. The TUI
// owns this slice; the MCP server is stateless per-task.
type Turn struct {
	Role    Role
	Content string
}

// renderConversation flattens the accumulated turns into a single markdown
// blob that gets passed as task_description to dispatch_task. The CTO sees
// this verbatim, so the format is what gives the agent multi-turn context
// across what are technically independent dispatches.
//
// Format:
//
//	**User:**
//	<content>
//
//	**Assistant:**
//	<content>
//
//	...
//
// The trailing "User:" block (added by the caller via appendUserTurn) is
// the new message — the agent sees the full prior context plus the new ask
// and replies accordingly.
func renderConversation(turns []Turn) string {
	if len(turns) == 0 {
		return ""
	}
	var b strings.Builder
	for i, t := range turns {
		if i > 0 {
			b.WriteString("\n\n")
		}
		switch t.Role {
		case RoleUser:
			b.WriteString("**User:**\n")
		case RoleAssistant:
			b.WriteString("**Assistant:**\n")
		default:
			// Unknown role: render literally so the agent still sees the
			// content rather than silently dropping it.
			b.WriteString("**" + string(t.Role) + ":**\n")
		}
		b.WriteString(t.Content)
	}
	return b.String()
}

// appendUserTurn is the immutable companion to renderConversation. Returns
// a new slice — Bubble Tea models should treat their state as copy-on-write
// so the diffing in Update stays predictable.
func appendUserTurn(turns []Turn, content string) []Turn {
	out := make([]Turn, 0, len(turns)+1)
	out = append(out, turns...)
	out = append(out, Turn{Role: RoleUser, Content: content})
	return out
}

// appendAssistantTurn mirrors appendUserTurn for assistant replies.
func appendAssistantTurn(turns []Turn, content string) []Turn {
	out := make([]Turn, 0, len(turns)+1)
	out = append(out, turns...)
	out = append(out, Turn{Role: RoleAssistant, Content: content})
	return out
}
