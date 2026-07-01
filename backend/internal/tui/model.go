// Package tui implements the pux-tui conversational interface — a Bubble
// Tea program that points at a running pux-mcpserver and lets the operator
// chat with an org's CTO. Conversation state lives client-side; each Enter
// dispatches the full accumulated conversation as a single task.
//
// ISOLATION CONTRACT: this package is fully deletable. rm -rf the package
// + cmd/pux-tui/ and the MCP server still builds + runs identically. The
// only allowed imports are stdlib + charmbracelet/* + internal/history
// (for the optional history pane). Reach for agent/, mcpserver/, sandbox/,
// org/, audit/, sensitive/, adapters/, tools/, or core/ → contract broken.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

	"github.com/auto-developer-orchestrator/backend/internal/history"
)

// mode is the current top-level screen.
type mode int

const (
	modeChat mode = iota
	modeHistory
)

// Model is the top-level tea.Model. Owns the conversation, the in-flight
// task (if any), and the optional history pane. The Bubble Tea runtime
// calls Update serially — no internal locking needed.
type Model struct {
	client *MCPClient
	hist   *HistoryPane
	org    string

	// chat state
	mode     mode
	convo    []Turn
	input    textinput.Model
	width    int
	height   int
	mdRenderer *glamour.TermRenderer

	// in-flight task state
	taskID  string
	status  string // "" idle | "running" | "complete" | "failed"
	round   int
	tail    string
	lastErr string

	// history-mode state
	historyTasks   []history.TaskRow
	historyCursor  int
	historyDetail  *history.TaskRow
	historyLoadErr string
}

// NewModel constructs the tea.Model. callerOrg may be empty — the model
// handles a missing org by rendering a hint in the input bar. The history
// pane may be zero-value (not available) — methods nil-check it.
func NewModel(client *MCPClient, hist *HistoryPane, callerOrg string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message…  (Enter to send, H for history, Ctrl+C to quit)"
	ti.Prompt = "> "
	ti.CharLimit = 0 // unbounded; long task descriptions are fine
	ti.Focus()

	return Model{
		client: client,
		hist:   hist,
		org:    callerOrg,
		mode:   modeChat,
		input:  ti,
		status: "",
	}
}

// Init returns the initial command. We don't need to bootstrap anything
// synchronously — the program is ready to receive input immediately.
func (m Model) Init() tea.Cmd {
	return nil
}

// ── Messages ──────────────────────────────────────────────────────────

type taskDispatchedMsg struct {
	taskID string
	err    error
}

type statusMsg struct {
	resp statusResponse
	err  error
}

// statusResponse is the locally-held copy of MCPClient.StatusResponse —
// redeclared here so the message type lives in the package's vocabulary
// (we already export StatusResponse via the client; alias for clarity).
type statusResponse = StatusResponse

type historyLoadedMsg struct {
	tasks []history.TaskRow
	err   error
}

type errMsg struct {
	err error
}

// ── Update ────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case taskDispatchedMsg:
		if msg.err != nil {
			m.status = "failed"
			m.lastErr = msg.err.Error()
			// Restore the user's last message text so they can edit + retry.
			if len(m.convo) > 0 && m.convo[len(m.convo)-1].Role == RoleUser {
				m.input.SetValue(m.convo[len(m.convo)-1].Content)
				m.convo = m.convo[:len(m.convo)-1]
			}
			return m, nil
		}
		m.taskID = msg.taskID
		m.status = "running"
		m.lastErr = ""
		return m, pollStatusCmd(m.client, msg.taskID)

	case statusMsg:
		if msg.err != nil {
			m.status = "failed"
			m.lastErr = msg.err.Error()
			return m, nil
		}
		m.status = msg.resp.Status
		m.round = msg.resp.Round
		m.tail = msg.resp.TranscriptTail
		if msg.resp.Status == "complete" {
			m.convo = appendAssistantTurn(m.convo, msg.resp.Result)
			m.taskID = ""
			return m, nil
		}
		if msg.resp.Status == "failed" {
			m.lastErr = msg.resp.Error
			m.taskID = ""
			return m, nil
		}
		// Still pending or running — keep polling.
		return m, pollStatusCmd(m.client, m.taskID)

	case historyLoadedMsg:
		if msg.err != nil {
			m.historyLoadErr = msg.err.Error()
			return m, nil
		}
		m.historyTasks = msg.tasks
		m.historyLoadErr = ""
		m.historyDetail = nil
		return m, nil

	case errMsg:
		m.lastErr = msg.err.Error()
		return m, nil
	}

	// Forward to the input model so typing works.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleKey routes keypresses per mode.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys work in any mode.
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	}

	if m.mode == modeHistory {
		return m.handleHistoryKey(msg)
	}
	return m.handleChatKey(msg)
}

func (m Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Don't dispatch if a task is already running — wait for it to finish.
		if m.status == "running" {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		if m.org == "" {
			m.lastErr = "no --org set; restart pux-tui with --org <name>"
			return m, nil
		}
		m.convo = appendUserTurn(m.convo, text)
		m.input.Reset()
		m.status = "running"
		m.lastErr = ""
		desc := renderConversation(m.convo)
		return m, dispatchCmd(m.client, m.org, desc)

	case tea.KeyCtrlH:
		// History pane toggle. No-op if not available.
		if !m.hist.Available() {
			m.lastErr = "history disabled (set PUX_HISTORY_DIR on the server)"
			return m, nil
		}
		m.mode = modeHistory
		m.historyCursor = 0
		m.historyDetail = nil
		return m, loadHistoryCmd(m.hist, m.org)
	}

	// All other keys → forward to textinput.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.historyTasks) == 0 {
		// Any key exits back to chat when the list is empty.
		if msg.String() == "esc" || msg.String() == "ctrl+h" || msg.Type == tea.KeyEnter {
			m.mode = modeChat
		}
		return m, nil
	}

	switch msg.String() {
	case "esc", "ctrl+h":
		if m.historyDetail != nil {
			m.historyDetail = nil
		} else {
			m.mode = modeChat
		}
		return m, nil
	case "up", "k":
		if m.historyCursor > 0 {
			m.historyCursor--
		}
	case "down", "j":
		if m.historyCursor < len(m.historyTasks)-1 {
			m.historyCursor++
		}
	case "enter":
		task := m.historyTasks[m.historyCursor]
		m.historyDetail = &task
	case "r":
		// Refresh the task list.
		return m, loadHistoryCmd(m.hist, m.org)
	}
	return m, nil
}

// ── Commands ──────────────────────────────────────────────────────────

// dispatchCmd runs the HTTP dispatch call in a goroutine + returns the
// taskDispatchedMsg when done. The Cmd signature is the Bubble Tea pattern
// for non-blocking IO.
func dispatchCmd(client *MCPClient, org, desc string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.Dispatch(context.Background(), org, desc)
		return taskDispatchedMsg{taskID: resp.TaskID, err: err}
	}
}

// pollStatusCmd polls the task status after a 500ms delay. Cmds run in
// goroutines, so sleeping here is fine — the runtime will keep the
// program responsive. If the task is still pending/running, Update will
// re-issue this Cmd.
func pollStatusCmd(client *MCPClient, taskID string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)
		resp, err := client.Status(context.Background(), taskID)
		return statusMsg{resp: resp, err: err}
	}
}

// loadHistoryCmd fetches the recent task list for the history pane.
func loadHistoryCmd(hist *HistoryPane, org string) tea.Cmd {
	return func() tea.Msg {
		tasks, err := hist.ListTasks(context.Background(), org, 50)
		return historyLoadedMsg{tasks: tasks, err: err}
	}
}

// ── View ──────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		// WindowSizeMsg hasn't arrived yet — render a placeholder so the
		// first frame doesn't crash on a 0-width viewport.
		return brandStyle.Render("pux-tui") + "\n\n(starting up…)"
	}

	if m.mode == modeHistory {
		return m.viewHistory()
	}
	return m.viewChat()
}

func (m Model) viewChat() string {
	var b strings.Builder

	// Top bar: brand · org · status · history hint
	b.WriteString(m.renderTopBar())
	b.WriteString("\n")

	// Conversation viewport area.
	body := m.renderConversation()
	// Reserve space for top bar (1) + spacer (1) + input (1) + hint (1) = 4
	bodyHeight := m.height - 4
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	// Truncate the body to fit — show the LAST bodyHeight lines (most recent).
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > bodyHeight {
		bodyLines = bodyLines[len(bodyLines)-bodyHeight:]
	}
	for _, ln := range bodyLines {
		b.WriteString(ln)
		b.WriteString("\n")
	}

	// Input bar.
	b.WriteString(m.input.View())
	b.WriteString("\n")

	// Hint bar.
	b.WriteString(m.renderHintBar())

	return b.String()
}

func (m Model) renderTopBar() string {
	statusBadge := m.renderStatusBadge()
	right := ""
	if m.hist.Available() {
		right = mutedStyle.Render("  Ctrl+H history")
	}
	return topBarStyle.Render(fmt.Sprintf(
		"%s · %s · %s%s",
		brandStyle.Render("pux-tui"),
		orgBadgeStyle.Render(m.orgLabel()),
		statusBadge,
		right,
	))
}

func (m Model) renderStatusBadge() string {
	switch m.status {
	case "running":
		round := ""
		if m.round > 0 {
			round = fmt.Sprintf(" round %d", m.round)
		}
		return statusRunningStyle.Render("running" + round)
	case "complete":
		return statusCompleteStyle.Render("complete")
	case "failed":
		return statusFailedStyle.Render("failed")
	default:
		return statusIdleStyle.Render("idle")
	}
}

func (m Model) orgLabel() string {
	if m.org == "" {
		return mutedStyle.Render("(no --org)")
	}
	return m.org
}

func (m Model) renderHintBar() string {
	parts := []string{
		mutedStyle.Render("Enter send"),
		mutedStyle.Render("Ctrl+C quit"),
	}
	if m.hist.Available() {
		parts = append(parts, mutedStyle.Render("Ctrl+H history"))
	}
	if m.lastErr != "" {
		parts = append(parts, errorStyle.Render("err: "+truncateForHint(m.lastErr, 60)))
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// renderConversation flattens the convo + live "thinking" indicator into a
// single string. The actual rendering of assistant markdown is delegated to
// glamour for a nicer terminal aesthetic.
func (m Model) renderConversation() string {
	if len(m.convo) == 0 && m.status != "running" {
		return mutedStyle.Render("(no messages yet — type your first message below)") + "\n"
	}

	var b strings.Builder
	for _, t := range m.convo {
		switch t.Role {
		case RoleUser:
			b.WriteString(userLabelStyle.Render("user"))
			b.WriteString("\n")
			b.WriteString(t.Content)
			b.WriteString("\n\n")
		case RoleAssistant:
			b.WriteString(assistantLabelStyle.Render("assistant"))
			b.WriteString("\n")
			b.WriteString(m.renderMarkdown(t.Content))
			b.WriteString("\n\n")
		}
	}

	// Live "thinking" indicator while a task is in flight.
	if m.status == "running" {
		b.WriteString(assistantLabelStyle.Render("assistant"))
		hint := " · thinking…"
		if m.round > 0 {
			hint += fmt.Sprintf(" (round %d)", m.round)
		}
		b.WriteString(statusRunningStyle.Render(hint))
		b.WriteString("\n")
		if m.tail != "" {
			tailLines := strings.Split(strings.TrimSpace(m.tail), "\n")
			for _, ln := range tailLines {
				b.WriteString(mutedStyle.Render("  ↳ " + truncateLine(ln, m.width-4)))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderMarkdown wraps glamour for assistant output. Lazily constructs the
// renderer on first use so we don't pay for it when only sending messages.
func (m *Model) renderMarkdown(s string) string {
	if s == "" {
		return ""
	}
	if m.mdRenderer == nil {
		r, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(120),
		)
		if err != nil {
			// Fallback: raw text without markdown rendering.
			return s
		}
		m.mdRenderer = r
	}
	out, err := m.mdRenderer.Render(s)
	if err != nil {
		return s
	}
	// Glamour pads with newlines; trim them so the bubble stays compact.
	return strings.TrimSpace(out)
}

// ── History view ──────────────────────────────────────────────────────

func (m Model) viewHistory() string {
	var b strings.Builder
	b.WriteString(m.renderTopBar())
	b.WriteString("\n")

	if m.historyDetail != nil {
		b.WriteString(m.renderHistoryDetail(*m.historyDetail))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("Esc/E Ctrl+H back"))
		return b.String()
	}

	if m.historyLoadErr != "" {
		b.WriteString(errorStyle.Render("history load error: " + m.historyLoadErr))
		b.WriteString("\n")
	}

	if len(m.historyTasks) == 0 {
		b.WriteString(mutedStyle.Render("(no tasks yet)"))
		b.WriteString("\n\n")
		b.WriteString(mutedStyle.Render("Esc/E Ctrl+H back"))
		return b.String()
	}

	// Render the task list. Body height reserves top bar + hint.
	bodyHeight := m.height - 4
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	for i, t := range m.historyTasks {
		if i >= bodyHeight {
			break
		}
		marker := "  "
		line := fmt.Sprintf("%s  %-10s  %s", t.ID, t.Status, truncateLine(t.Task, m.width-30))
		if i == m.historyCursor {
			b.WriteString(historyCursorStyle.Render("▶ " + line))
		} else {
			b.WriteString(marker + mutedStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("↑↓ navigate · Enter detail · r refresh · Esc/E Ctrl+H back"))
	return b.String()
}

func (m Model) renderHistoryDetail(t history.TaskRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task     %s\n", t.ID)
	fmt.Fprintf(&b, "Org      %s\n", t.Org)
	fmt.Fprintf(&b, "Status   %s\n", t.Status)
	fmt.Fprintf(&b, "Started  %s\n", t.StartedAt.Format(time.RFC3339))
	if !t.FinishedAt.IsZero() {
		fmt.Fprintf(&b, "Finished %s (%s)\n",
			t.FinishedAt.Format(time.RFC3339),
			t.FinishedAt.Sub(t.StartedAt).Round(time.Millisecond),
		)
	}
	fmt.Fprintf(&b, "\nRequest:\n  %s\n\n", t.Task)
	if t.Result != "" {
		fmt.Fprintf(&b, "Result:\n%s\n\n", indentString(t.Result, "  "))
	}
	if t.Error != "" {
		fmt.Fprintf(&b, "Error:\n%s\n\n", indentString(t.Error, "  "))
	}

	if m.hist.Available() {
		ctx := context.Background()
		if msgs, err := m.hist.ListMessages(ctx, t.ID); err == nil {
			for _, msg := range msgs {
				fmt.Fprintf(&b, "[round %d | %s] assistant:\n%s\n\n",
					msg.Round, msg.Role, indentString(msg.Content, "  "))
			}
		}
		if calls, err := m.hist.ListToolCalls(ctx, t.ID); err == nil {
			for _, c := range calls {
				fmt.Fprintf(&b, "[round %d | %s] tool %s (%dms)\n",
					c.Round, c.Role, c.Tool, c.DurationMs)
				if c.Error != "" {
					fmt.Fprintf(&b, "  error: %s\n", c.Error)
				}
			}
		}
	}

	return b.String()
}

// ── helpers ───────────────────────────────────────────────────────────

// truncateForHint clips a string to n chars for use in a one-line hint bar.
func truncateForHint(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// truncateLine clips a single line to n visible chars. Doesn't wrap —
// wrapping would inflate the body height calculation.
func truncateLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// indentString prefixes every line of s with prefix. Used for the history
// detail transcript.
func indentString(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

// Compile-time assertion that Model satisfies tea.Model.
var _ tea.Model = Model{}
