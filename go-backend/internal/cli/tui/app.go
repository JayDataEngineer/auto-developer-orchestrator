package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---- data types ----

type chatMessage struct {
	role      string
	content   string
	thinking  string
	tools     []toolCallDisplay
	timestamp time.Time
}

type toolCallDisplay struct {
	name   string
	id     string
	args   string
	result string
	err    string
	done   bool
}

type approvalDisplay struct {
	requestID string
	toolName  string
	toolArgs  string
	message   string
	risk      string
}

// ---- key bindings (matching OpenCode pattern) ----

var editorKeys = struct {
	Send       key.Binding
	ToggleHelp key.Binding
	ToggleJobs key.Binding
	Quit       key.Binding
}{
	Send: key.NewBinding(
		key.WithKeys("enter", "ctrl+s"),
		key.WithHelp("enter", "send message"),
	),
	ToggleHelp: key.NewBinding(
		key.WithKeys("ctrl+h"),
		key.WithHelp("ctrl+h", "toggle help"),
	),
	ToggleJobs: key.NewBinding(
		key.WithKeys("ctrl+j"),
		key.WithHelp("ctrl+j", "scheduler"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
}

// ---- Model ----

type Model struct {
	serverURL string
	project   string
	agentID   string
	client    *api.Client

	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model

	messages     []chatMessage
	streaming    bool
	ready        bool
	width        int
	height       int
	eventCh      chan api.SSEEvent
	lastActivity time.Time

	currentText     string
	currentThinking string
	currentTools    []toolCallDisplay

	pendingApproval *approvalDisplay
	renderer        *glamour.TermRenderer

	showHelp      bool
	thinkExpanded bool
	inputTokens   int
	outputTokens  int

	schedMode bool
	scheduler schedModel
}

func NewModel(serverURL, project, agentID string) Model {
	ta := textarea.New()
	ta.Placeholder = "Message orch..."
	ta.Prompt = " " // suppress built-in prompt — we render ❯ ourselves
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.FocusedStyle.Base = lipgloss.NewStyle().Foreground(textWhite)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle().Foreground(textWhite)
	ta.FocusedStyle.Prompt = lipgloss.NewStyle()
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(textWhite)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(textDim)
	ta.BlurredStyle.Prompt = ta.FocusedStyle.Prompt
	ta.BlurredStyle.Text = ta.FocusedStyle.Text
	ta.BlurredStyle.Placeholder = ta.FocusedStyle.Placeholder
	ta.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(green)

	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().Padding(0, 1)

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithAutoStyle(),
	)

	return Model{
		serverURL:     serverURL,
		project:       project,
		agentID:       agentID,
		client:        api.NewClient(serverURL),
		viewport:      vp,
		input:         ta,
		spinner:       s,
		renderer:      renderer,
		thinkExpanded: true,
	}
}

// ---- Init ----

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

// ---- Update ----

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Scheduler mode
	if m.schedMode {
		return m.updateSched(msg)
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerH := 1
		footerH := 2
		inputH := 4
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - headerH - footerH - inputH
		m.input.SetWidth(msg.Width - 4)
		m.input.SetHeight(3)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		m.ready = true

	case tea.KeyMsg:
		switch {

		// Enter = send (OpenCode pattern: \ escape for newline, Shift+Enter for newline)
		case key.Matches(msg, editorKeys.Send):
			// Shift/Alt modifier → let textarea handle it as newline
			if msg.Alt {
				break
			}
			// Approval mode: send enters approval response
			if m.pendingApproval != nil {
				break // handled below
			}
			if m.streaming {
				break
			}
			// \ escape: remove trailing \, insert newline
			val := m.input.Value()
			if len(val) > 0 && val[len(val)-1] == '\\' {
				m.input.SetValue(val[:len(val)-1] + "\n")
				return m, nil
			}
			// Send the message
			if strings.TrimSpace(val) != "" {
				return m.sendMessage()
			}

		// Ctrl+H: toggle help
		case key.Matches(msg, editorKeys.ToggleHelp):
			m.showHelp = !m.showHelp
			return m, nil

		// Ctrl+J: scheduler mode
		case key.Matches(msg, editorKeys.ToggleJobs):
			m.schedMode = true
			m.scheduler = newSchedModel(m.width, m.height, api.NewClient(m.serverURL))
			return m, loadSchedJobs(m.client)

		// Ctrl+C: quit/abort
		case key.Matches(msg, editorKeys.Quit):
			if m.streaming {
				m.streaming = false
				m.finalizeMessage()
				m.refreshViewport()
				return m, nil
			}
			return m, tea.Quit
		}

	// SSE events
	case textDeltaMsg:
		m.currentText += msg.text
		m.lastActivity = time.Now()
		m.refreshViewport()
		cmds = append(cmds, readNextEvent(m.eventCh))

	case thinkingDeltaMsg:
		m.currentThinking += msg.text
		m.lastActivity = time.Now()
		m.refreshViewport()
		cmds = append(cmds, readNextEvent(m.eventCh))

	case toolStartMsg:
		m.currentTools = append(m.currentTools, toolCallDisplay{
			name: msg.name,
			id:   msg.id,
			args: msg.args,
		})
		m.lastActivity = time.Now()
		m.refreshViewport()
		cmds = append(cmds, readNextEvent(m.eventCh))

	case toolEndMsg:
		for i := range m.currentTools {
			if m.currentTools[i].name == msg.name && !m.currentTools[i].done {
				m.currentTools[i].result = msg.result
				m.currentTools[i].err = msg.err
				m.currentTools[i].done = true
				break
			}
		}
		m.lastActivity = time.Now()
		m.refreshViewport()
		cmds = append(cmds, readNextEvent(m.eventCh))

	case approvalRequestMsg:
		m.pendingApproval = &approvalDisplay{
			requestID: msg.requestID,
			toolName:  msg.toolName,
			toolArgs:  msg.toolArgs,
			message:   msg.message,
			risk:      msg.risk,
		}
		m.input.SetValue("")
		m.refreshViewport()

	case artifactMsg:
		cmds = append(cmds, readNextEvent(m.eventCh))

	case errorMsg:
		m.streaming = false
		m.finalizeMessage()
		m.messages = append(m.messages, chatMessage{
			role:      "assistant",
			content:   "**Error:** " + msg.err,
			timestamp: time.Now(),
		})
		m.refreshViewport()

	case doneMsg:
		m.streaming = false
		m.inputTokens = msg.inputTokens
		m.outputTokens = msg.outputTokens
		m.finalizeMessage()
		m.refreshViewport()

	case streamEndMsg:
		m.streaming = false
		if m.currentText != "" || m.currentThinking != "" || len(m.currentTools) > 0 {
			m.finalizeMessage()
		}
		m.refreshViewport()

	case spinner.TickMsg:
		if m.streaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case toggleSchedMsg:
		m.schedMode = false
		m.refreshViewport()
	}

	// Update input (textarea) — must happen AFTER our key handling
	// so the textarea gets all non-Enter keys for normal typing
	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	// Update viewport
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	// Approval response handling
	if m.pendingApproval != nil {
		val := strings.TrimSpace(strings.ToLower(m.input.Value()))
		switch {
		case val == "y" || val == "yes":
			return m.respondToApproval("approve", "")
		case val == "n" || val == "no":
			return m.respondToApproval("deny", "")
		case strings.HasPrefix(val, "a:") || strings.HasPrefix(val, "answer:"):
			answer := strings.TrimPrefix(val, "a:")
			answer = strings.TrimPrefix(answer, "answer:")
			return m.respondToApproval("answer", strings.TrimSpace(answer))
		}
	}

	return m, tea.Batch(cmds...)
}

// ---- View ----

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if m.schedMode {
		return m.scheduler.View()
	}

	if m.showHelp {
		return m.viewHelp()
	}

	header := m.viewHeader()
	content := m.viewport.View()
	input := m.viewInput()
	footer := m.viewFooter()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		content,
		input,
		footer,
	)
}

func (m Model) viewHeader() string {
	right := fmt.Sprintf("%s/%s", m.project, m.agentID)
	if m.inputTokens > 0 {
		right += fmt.Sprintf("  in:%d out:%d", m.inputTokens, m.outputTokens)
	}
	rightStyled := lipgloss.NewStyle().Foreground(textDim).Render(right)
	if m.streaming {
		rightStyled = lipgloss.NewStyle().Foreground(textDim).Render(
			m.spinner.View() + " " + right,
		)
	}

	gap := max(0, m.width-lipgloss.Width(orchLogo)-lipgloss.Width(rightStyled)-4)
	line := lipgloss.JoinHorizontal(lipgloss.Center,
		orchLogo,
		lipgloss.NewStyle().Width(gap).Render(""),
		lipgloss.NewStyle().Width(lipgloss.Width(rightStyled)).Render(rightStyled),
	)
	return headerStyle.Width(m.width).Render(line)
}

func (m Model) viewInput() string {
	if m.pendingApproval != nil {
		return m.viewApproval()
	}

	// OpenCode pattern: render prompt ❯ separately, then textarea
	prompt := lipgloss.NewStyle().
		Padding(0, 0, 0, 1).
		Bold(true).
		Foreground(accent).
		Render("❯")

	inputView := m.input.View()

	// Wrap with top border separator
	separator := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), false, false, true, false).
		BorderForeground(grayDark).
		Width(m.width - 2)

	return separator.Render(
		lipgloss.JoinHorizontal(lipgloss.Top, prompt, inputView),
	)
}

func (m Model) viewApproval() string {
	ap := m.pendingApproval

	riskLabel := approvalRiskLow
	if strings.ToLower(ap.risk) == "high" {
		riskLabel = approvalRiskHigh
	}

	header := approvalTitleStyle.Render("AUTHORIZATION REQUIRED") + "  " + riskLabel
	body := fmt.Sprintf(
		"%s %s\n\n%s\n\n",
		toolNameStyle.Render(ap.toolName),
		toolArgsStyle.Render(ap.toolArgs),
		ap.message,
	)
	labelStyle := lipgloss.NewStyle().Foreground(textDim)
	keys := lipgloss.JoinHorizontal(lipgloss.Left,
		approvalKeyStyle.Render("[y]"), labelStyle.Render(" Approve  "),
		approvalKeyStyle.Render("[n]"), labelStyle.Render(" Deny  "),
		approvalKeyStyle.Render("[a:text]"), labelStyle.Render(" Answer"),
	)

	content := header + "\n" + body + "\n" + keys
	return approvalBoxStyle.Width(m.width - 4).Render(content)
}

func (m Model) viewFooter() string {
	kbStyle := lipgloss.NewStyle().Foreground(textDim)
	kb := lipgloss.JoinHorizontal(lipgloss.Left,
		kbStyle.Render("enter"), statusInfoStyle.Render("send"),
		kbStyle.Render(" · \\+enter"), statusInfoStyle.Render("newline"),
		kbStyle.Render(" · ctrl+h"), statusInfoStyle.Render("help"),
		kbStyle.Render(" · ctrl+j"), statusInfoStyle.Render("jobs"),
		kbStyle.Render(" · ctrl+c"), statusInfoStyle.Render("quit"),
	)
	return statusBarStyle.Width(m.width).Render(kb)
}

// ---- help overlay ----

func (m Model) viewHelp() string {
	title := helpTitleStyle.Render(" Keyboard Shortcuts ")
	items := []string{
		statusKeyStyle.Render("Enter") + "  " + statusInfoStyle.Render("Send message (add trailing \\ for newline)"),
		statusKeyStyle.Render("Shift+Enter") + " " + statusInfoStyle.Render("Insert newline"),
		statusKeyStyle.Render("Ctrl+S") + "  " + statusInfoStyle.Render("Send message (alternative)"),
		statusKeyStyle.Render("Ctrl+T") + "  " + statusInfoStyle.Render("Toggle thinking display"),
		statusKeyStyle.Render("Ctrl+H") + "  " + statusInfoStyle.Render("Toggle this help"),
		statusKeyStyle.Render("Ctrl+J") + "  " + statusInfoStyle.Render("Scheduler mode"),
		statusKeyStyle.Render("Ctrl+C") + "  " + statusInfoStyle.Render("Quit (stop streaming if active)"),
		statusKeyStyle.Render("PgUp/Dn") + "" + statusInfoStyle.Render("Scroll messages"),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, items...)
	return helpBoxStyle.Width(m.width - 4).Render(
		lipgloss.JoinVertical(lipgloss.Left, title, "", content),
	)
}

// ---- viewport helpers ----

func (m *Model) refreshViewport() {
	m.viewport.SetContent(m.renderMessages())
	if m.streaming {
		m.viewport.GotoBottom()
	}
}

func (m *Model) finalizeMessage() {
	if m.currentText == "" && len(m.currentTools) == 0 && m.currentThinking == "" {
		return
	}
	m.messages = append(m.messages, chatMessage{
		role:      "assistant",
		content:   m.currentText,
		thinking:  m.currentThinking,
		tools:     append([]toolCallDisplay{}, m.currentTools...),
		timestamp: time.Now(),
	})
	m.currentText = ""
	m.currentThinking = ""
	m.currentTools = nil
}

// ---- rendering ----

func (m *Model) renderMessages() string {
	var sb strings.Builder

	for i := range m.messages {
		msg := m.messages[i]
		switch msg.role {
		case "user":
			sb.WriteString(m.renderUserMsg(msg))
		case "assistant":
			sb.WriteString(m.renderAssistantMsg(msg, i))
		}
	}

	if m.streaming && (m.currentText != "" || m.currentThinking != "" || len(m.currentTools) > 0) {
		sb.WriteString(m.renderStreamingState())
	}

	return sb.String()
}

func (m *Model) renderUserMsg(msg chatMessage) string {
	var sb strings.Builder
	sb.WriteString(userPrefix + " " + userMsgStyle.Render(msg.content) + "\n\n")
	return sb.String()
}

func (m *Model) renderAssistantMsg(msg chatMessage, idx int) string {
	var sb strings.Builder

	if msg.thinking != "" {
		if m.thinkExpanded {
			sb.WriteString(m.renderThinking(msg.thinking) + "\n")
		} else {
			wordCount := len(strings.Fields(msg.thinking))
			charCount := len(msg.thinking)
			summary := fmt.Sprintf("  Thought for %d words (%d chars)", wordCount, charCount)
			sb.WriteString(sectionStyle.Render(summary) + "\n\n")
		}
	}

	if msg.content != "" {
		rendered, err := m.renderer.Render(msg.content)
		if err != nil {
			rendered = assistantStyle.Render(msg.content)
		}
		sb.WriteString(rendered + "\n")
	}

	for _, tool := range msg.tools {
		sb.WriteString(m.renderToolCall(tool) + "\n")
	}

	if msg.content != "" || len(msg.tools) > 0 {
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m *Model) renderStreamingState() string {
	var sb strings.Builder

	if m.currentThinking != "" {
		sb.WriteString(m.renderThinking(m.currentThinking) + "\n")
	}

	if m.currentText != "" {
		sb.WriteString(assistantStyle.Render(m.currentText) + "\n")
	}

	for _, tool := range m.currentTools {
		sb.WriteString(m.renderToolCall(tool) + "\n")
	}

	sb.WriteString(m.spinner.View() + " thinking...\n")

	return sb.String()
}

func (m *Model) renderThinking(text string) string {
	header := thinkHeaderStyle.Render("·· thinking ··")
	body := thinkBodyStyle.Render(strings.TrimSpace(text))
	return thinkBoxStyle.Width(m.viewport.Width - 2).Render(header + "\n" + body)
}

func (m *Model) renderToolCall(tool toolCallDisplay) string {
	var sb strings.Builder

	statusStyle := lipgloss.NewStyle().Foreground(yellow)
	statusLabel := "running"
	if tool.done {
		if tool.err != "" {
			statusStyle = lipgloss.NewStyle().Foreground(red)
			statusLabel = "failed"
		} else {
			statusStyle = lipgloss.NewStyle().Foreground(green)
			statusLabel = "done"
		}
	}

	nameIcon := toolRunningIcon.String()
	if tool.done {
		if tool.err != "" {
			nameIcon = toolErrorIcon.String()
		} else {
			nameIcon = toolSuccessIcon.String()
		}
	}

	nameLine := toolBoxStyle.Width(m.viewport.Width - 2).Render(
		nameIcon + toolNameStyle.Render(tool.name) + " " +
			toolArgsStyle.Render(tool.args) + " " +
			toolArgsStyle.Render(statusStyle.Render("["+statusLabel+"]")),
	)
	sb.WriteString(nameLine)

	if tool.done && (tool.result != "" || tool.err != "") {
		resultText := tool.result
		if tool.err != "" {
			resultText = toolErrorIcon.String() + " " + tool.err
		}
		if len(resultText) > 500 {
			resultText = resultText[:500] + "..."
		}
		lines := strings.Split(resultText, "\n")
		for _, line := range lines {
			sb.WriteString("\n" + toolResultBorder.String() + " " +
				toolResultStyle.Render(line))
		}
	}

	return sb.String()
}

// ---- send message ----

func (m Model) sendMessage() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()

	m.messages = append(m.messages, chatMessage{
		role:      "user",
		content:   text,
		timestamp: time.Now(),
	})

	m.streaming = true
	m.currentText = ""
	m.currentThinking = ""
	m.currentTools = nil
	m.eventCh = make(chan api.SSEEvent, 100)
	m.inputTokens = 0
	m.outputTokens = 0

	m.refreshViewport()

	return m, startStreamCmd(m.client, m.project, m.agentID, text, m.eventCh)
}

// ---- scheduler integration ----

func (m Model) updateSched(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.scheduler, cmd = m.scheduler.Update(msg)

	// Clear notification after rendered
	m.scheduler.notification = ""

	return m, cmd
}

// ---- approval ----

func (m Model) respondToApproval(action, message string) (tea.Model, tea.Cmd) {
	if m.pendingApproval == nil {
		return m, nil
	}
	ap := m.pendingApproval
	m.pendingApproval = nil
	m.input.Reset()

	m.refreshViewport()

	return m, tea.Batch(
		func() tea.Msg {
			_ = m.client.Post("/api/pux/respond", api.ApprovalRequestBody{
				Project:   m.project,
				AgentID:   m.agentID,
				RequestID: ap.requestID,
				Action:    action,
				Message:   message,
			}, nil)
			return nil
		},
		readNextEvent(m.eventCh),
	)
}

// ---- SSE start ----

func startStreamCmd(client *api.Client, project, agentID, text string, ch chan api.SSEEvent) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.StreamPost("/api/pux/prompt", api.PromptRequest{
			Message: text,
			Project: project,
			AgentID: agentID,
		})
		if err != nil {
			return errorMsg{err: err.Error()}
		}

		go api.StreamSSE(resp.Body, ch)

		event, ok := <-ch
		if !ok {
			return streamEndMsg{}
		}
		msg := convertSSEEvent(event)
		if msg == nil {
			return streamEndMsg{}
		}
		return msg
	}
}
