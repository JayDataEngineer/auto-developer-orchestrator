package tui

import (
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type chatMessage struct {
	role     string // "user" or "assistant"
	content  string
	thinking string
	tools    []toolCallDisplay
}

type toolCallDisplay struct {
	name   string
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

// Model is the Bubble Tea model for the chat TUI.
type Model struct {
	serverURL string
	project   string
	agentID   string
	client    *api.Client

	// UI components
	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model

	// State
	messages  []chatMessage
	streaming bool
	ready     bool
	width     int
	height    int
	eventCh   chan api.SSEEvent

	// Streaming accumulation
	currentText     string
	currentThinking string
	currentTools    []toolCallDisplay

	// Approval
	pendingApproval *approvalDisplay

	// Renderer
	renderer *glamour.TermRenderer
}

func NewModel(serverURL, project, agentID string) Model {
	ta := textarea.New()
	ta.Placeholder = "Send a message..."
	ta.Prompt = "> "
	ta.CharLimit = 0 // unlimited
	ta.SetWidth(60)
	ta.SetHeight(3)
	ta.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))

	vp := viewport.New(80, 20)

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithEnvironmentConfig(),
		glamour.WithAutoStyle(),
	)

	return Model{
		serverURL: serverURL,
		project:   project,
		agentID:   agentID,
		client:    api.NewClient(serverURL),
		viewport:  vp,
		input:     ta,
		spinner:   s,
		renderer:  renderer,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 2
		inputHeight := 5
		footerHeight := 1
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - headerHeight - inputHeight - footerHeight
		m.input.SetWidth(msg.Width - 4)
		m.viewport.SetContent(m.renderMessages())
		m.ready = true

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.streaming {
				m.streaming = false
				m.finalizeMessage()
				m.refreshViewport()
				return m, nil
			}
			return m, tea.Quit

		case tea.KeyCtrlD:
			return m, tea.Quit

		case tea.KeyEnter:
			// If approval pending, handle approval response
			if m.pendingApproval != nil {
				return m, nil // handled in textarea update below
			}
			// Send message
			if !m.streaming && strings.TrimSpace(m.input.Value()) != "" {
				return m.sendMessage()
			}
		}

	case textDeltaMsg:
		m.currentText += msg.text
		m.refreshViewport()
		cmds = append(cmds, readNextEvent(m.eventCh))

	case thinkingDeltaMsg:
		m.currentThinking += msg.text
		m.refreshViewport()
		cmds = append(cmds, readNextEvent(m.eventCh))

	case toolStartMsg:
		m.currentTools = append(m.currentTools, toolCallDisplay{
			name: msg.name,
			args: msg.args,
		})
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
		m.refreshViewport()
		// Don't chain readNext — wait for approval response

	case artifactMsg:
		cmds = append(cmds, readNextEvent(m.eventCh))

	case errorMsg:
		m.streaming = false
		m.finalizeMessage()
		m.messages = append(m.messages, chatMessage{
			role:    "assistant",
			content: fmt.Sprintf("Error: %s", msg.err),
		})
		m.refreshViewport()

	case doneMsg:
		m.streaming = false
		m.finalizeMessage()
		m.refreshViewport()

	case streamEndMsg:
		m.streaming = false
		if m.currentText != "" {
			m.finalizeMessage()
		}
		m.refreshViewport()

	case spinner.TickMsg:
		if m.streaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	// Update sub-components
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	// Handle approval input (y/n/a)
	if m.pendingApproval != nil {
		val := m.input.Value()
		switch strings.ToLower(val) {
		case "y", "yes":
			return m.respondToApproval("approve", "")
		case "n", "no":
			return m.respondToApproval("deny", "")
		}
		if strings.HasPrefix(strings.ToLower(val), "a:") {
			answer := strings.TrimPrefix(val, "a:")
			return m.respondToApproval("answer", answer)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if !m.ready {
		return "Loading..."
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
	left := titleStyle.Render("orch")
	right := fmt.Sprintf("project:%s  agent:%s", m.project, m.agentID)
	if m.streaming {
		right += "  " + m.spinner.View()
	}
	return lipgloss.NewStyle().Width(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", dimTextStyle(right)),
	) + "\n"
}

func (m Model) viewInput() string {
	if m.pendingApproval != nil {
		ap := m.pendingApproval
		approvalText := fmt.Sprintf("APPROVAL NEEDED: %s %s\nRisk: %s\n%s\n[y] Approve  [n] Deny  [a:<text>] Answer",
			ap.toolName, ap.toolArgs, ap.risk, ap.message)
		return approvalStyle.Render(approvalText) + "\n"
	}
	return m.input.View() + "\n"
}

func (m Model) viewFooter() string {
	keys := "[Ctrl+S send] [Ctrl+C abort] [Ctrl+D quit]"
	return statusBarStyle.Width(m.width).Render(keys)
}

func (m *Model) refreshViewport() {
	m.refreshContent()
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
	})
	m.currentText = ""
	m.currentThinking = ""
	m.currentTools = nil
}

// refreshContent rebuilds the viewport content from the message list + current streaming state.
func (m *Model) refreshContent() {
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
}

func (m *Model) renderMessages() string {
	var sb strings.Builder

	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			sb.WriteString(userMsgStyle.Render("> "+msg.content) + "\n\n")
		case "assistant":
			if msg.thinking != "" {
				sb.WriteString(thinkingStyle.Render("<thinking>\n"+msg.thinking+"\n</thinking>") + "\n\n")
			}
			if msg.content != "" {
				rendered, err := m.renderer.Render(msg.content)
				if err != nil {
					rendered = msg.content
				}
				sb.WriteString(rendered + "\n")
			}
			for _, tool := range msg.tools {
				status := "..."
				if tool.done {
					if tool.err != "" {
						status = toolErrorStyle.Render("✗ " + tool.err)
					} else {
						status = toolResultStyle.Render("✓")
					}
				}
				sb.WriteString(toolStyle.Render(fmt.Sprintf("[TOOL] %s", tool.name)) + " " + status + "\n")
			}
			sb.WriteString("\n")
		}
	}

	// Current streaming state
	if m.currentText != "" || m.currentThinking != "" || len(m.currentTools) > 0 {
		if m.currentThinking != "" {
			sb.WriteString(thinkingStyle.Render("<thinking>\n"+m.currentThinking+"\n</thinking>") + "\n\n")
		}
		if m.currentText != "" {
			rendered, err := m.renderer.Render(m.currentText)
			if err != nil {
				rendered = m.currentText
			}
			sb.WriteString(rendered + "\n")
		}
		for _, tool := range m.currentTools {
			status := "..."
			if tool.done {
				if tool.err != "" {
					status = toolErrorStyle.Render("✗ " + tool.err)
				} else {
					status = toolResultStyle.Render("✓")
				}
			}
			sb.WriteString(toolStyle.Render(fmt.Sprintf("[TOOL] %s", tool.name)) + " " + status + "\n")
		}
		sb.WriteString(m.spinner.View() + "\n")
	}

	return sb.String()
}

func (m Model) sendMessage() (tea.Model, tea.Cmd) {
	text := m.input.Value()
	m.input.Reset()

	// Add user message
	m.messages = append(m.messages, chatMessage{role: "user", content: text})
	m.refreshContent()

	// Start streaming
	m.streaming = true
	m.currentText = ""
	m.currentThinking = ""
	m.currentTools = nil
	m.eventCh = make(chan api.SSEEvent, 100)

	return m, startStreamCmd(m.client, m.project, m.agentID, text, m.eventCh)
}

// startStreamCmd creates a tea.Cmd that starts the SSE connection and
// returns the first event. Subsequent events are read via chainReadNext.
func startStreamCmd(client *api.Client, project, agentID, text string, ch chan api.SSEEvent) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.StreamPost("/api/pi/prompt", api.PromptRequest{
			Message: text,
			Project: project,
			AgentID: agentID,
		})
		if err != nil {
			return errorMsg{err: err.Error()}
		}

		// Start SSE reader goroutine — writes to ch
		go api.StreamSSE(resp.Body, ch)

		// Read first event from channel
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

func (m Model) respondToApproval(action, message string) (tea.Model, tea.Cmd) {
	if m.pendingApproval == nil {
		return m, nil
	}
	ap := m.pendingApproval
	m.pendingApproval = nil
	m.input.Reset()
	m.refreshContent()

	return m, tea.Batch(
		func() tea.Msg {
			_ = m.client.Post("/api/pi/respond", api.ApprovalResponse{
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

func dimTextStyle(s string) string {
	return lipgloss.NewStyle().Foreground(dimText).Render(s)
}
