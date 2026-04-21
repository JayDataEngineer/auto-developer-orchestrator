package tui

// Message types for Bubble Tea updates.

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
