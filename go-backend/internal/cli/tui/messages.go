package tui

// Bubble Tea message types for the chat TUI.

// --- SSE event messages ---

type textDeltaMsg struct {
	text string
}

type thinkingDeltaMsg struct {
	text string
}

type toolStartMsg struct {
	name string
	id   string
	args string
}

type toolEndMsg struct {
	name   string
	id     string
	result string
	err    string
}

type approvalRequestMsg struct {
	requestID string
	toolName  string
	toolArgs  string
	message   string
	risk      string
}

type artifactMsg struct {
	artifactID string
	type_      string
	title      string
}

type errorMsg struct {
	err string
}

type doneMsg struct {
	inputTokens  int
	outputTokens int
}

type streamEndMsg struct{}

// --- UI action messages ---

// toggleThinkMsg toggles the thinking block for a given message index.
type toggleThinkMsg struct {
	msgIndex int
}

// toggleToolMsg toggles the tool result display.
type toggleToolMsg struct {
	msgIndex int
	toolIdx  int
}

// toggleHelpMsg shows or hides the keyboard help overlay.
type toggleHelpMsg struct{}

// scrollMsg scrolls the viewport.
type scrollMsg struct {
	lines int // positive = down, negative = up
}
