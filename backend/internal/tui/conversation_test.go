package tui

import (
	"strings"
	"testing"
)

// TestRenderConversation_Empty verifies the empty-slice case returns ""
// (not nil, not "null" — the dispatch surface rejects empty task_descriptions
// and a nil-render would surface that error confusingly).
func TestRenderConversation_Empty(t *testing.T) {
	if got := renderConversation(nil); got != "" {
		t.Errorf("empty: got %q want %q", got, "")
	}
}

// TestRenderConversation_SingleTurn verifies a single user turn renders as
// the markdown envelope with no leading/trailing whitespace beyond the
// content itself.
func TestRenderConversation_SingleTurn(t *testing.T) {
	got := renderConversation([]Turn{{Role: RoleUser, Content: "hello"}})
	want := "**User:**\nhello"
	if got != want {
		t.Errorf("single user: got %q want %q", got, want)
	}
}

// TestRenderConversation_MultiTurn verifies the separator + role labels
// across a user/assistant/user exchange. This is the shape that gets sent
// to dispatch_task on the second turn — the format is what gives the CTO
// context across what are technically independent dispatches.
func TestRenderConversation_MultiTurn(t *testing.T) {
	turns := []Turn{
		{Role: RoleUser, Content: "I want to make a 2D platformer"},
		{Role: RoleAssistant, Content: "Great — what art style?"},
		{Role: RoleUser, Content: "Use pixel art"},
	}
	got := renderConversation(turns)

	if !strings.Contains(got, "**User:**\nI want to make a 2D platformer") {
		t.Errorf("missing first user block: %q", got)
	}
	if !strings.Contains(got, "**Assistant:**\nGreat — what art style?") {
		t.Errorf("missing assistant block: %q", got)
	}
	if !strings.Contains(got, "**User:**\nUse pixel art") {
		t.Errorf("missing second user block: %q", got)
	}
	if !strings.Contains(got, "platformer\n\n**Assistant:**") {
		t.Errorf("missing blank-line separator: %q", got)
	}
}

// TestAppendUserTurn_Immutable verifies the helper returns a new slice
// rather than mutating the input. Bubble Tea Update functions treat state
// as copy-on-write.
func TestAppendUserTurn_Immutable(t *testing.T) {
	orig := []Turn{{Role: RoleUser, Content: "a"}}
	got := appendUserTurn(orig, "b")
	if len(orig) != 1 {
		t.Errorf("original mutated: got len %d want 1", len(orig))
	}
	if len(got) != 2 {
		t.Errorf("appended: got len %d want 2", len(got))
	}
	if got[1].Content != "b" || got[1].Role != RoleUser {
		t.Errorf("appended turn wrong: %+v", got[1])
	}
}

// TestAppendAssistantTurn_Immutable mirrors the user-turn test.
func TestAppendAssistantTurn_Immutable(t *testing.T) {
	orig := []Turn{{Role: RoleUser, Content: "a"}}
	got := appendAssistantTurn(orig, "reply")
	if len(orig) != 1 {
		t.Errorf("original mutated: got len %d want 1", len(orig))
	}
	if len(got) != 2 || got[1].Role != RoleAssistant || got[1].Content != "reply" {
		t.Errorf("appended turn wrong: %+v", got[1])
	}
}

// TestRenderConversation_UnknownRole verifies an unknown role string
// still surfaces (rather than dropping the content silently).
func TestRenderConversation_UnknownRole(t *testing.T) {
	got := renderConversation([]Turn{{Role: "system", Content: "warning"}})
	if !strings.Contains(got, "**system:**") {
		t.Errorf("unknown role dropped: %q", got)
	}
	if !strings.Contains(got, "warning") {
		t.Errorf("unknown role content lost: %q", got)
	}
}
