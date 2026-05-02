package tui

import (
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- scheduler messages ----

type schedJobsLoaded struct {
	jobs []api.SchedulerJob
	err  error
}

type schedJobToggled struct{}
type schedJobDeleted struct {
	id  string
	err error
}
type schedJobTriggered struct {
	id string
}

// ---- scheduler form messages ----

type schedStartCreateMsg struct{}
type schedStartEditMsg struct {
	job api.SchedulerJob
}

// ---- scheduler model ----

// schedModel is the scheduler list view, embedded in the main chat Model.
type schedModel struct {
	width  int
	height int

	jobs     []api.SchedulerJob
	selected int
	err      string

	client *api.Client

	// Form mode
	showForm bool
	editing  bool // true if editing existing, false if creating new
	editID   string
	formName textinput.Model
	formMsg  textinput.Model
	formCron textinput.Model
	formFocus int // 0=name, 1=msg, 2=cron

	// Notification
	notification string
}

func newSchedModel(width, height int, client *api.Client) schedModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "Job name (e.g. Daily cleanup)"
	nameInput.CharLimit = 100
	nameInput.Width = 50

	msgInput := textinput.New()
	msgInput.Placeholder = "Prompt message (e.g. Check and update dependencies)"
	msgInput.CharLimit = 500
	msgInput.Width = 50

	cronInput := textinput.New()
	cronInput.Placeholder = "Cron expression (e.g. @daily, 0 9 * * *)"
	cronInput.CharLimit = 80
	cronInput.Width = 50

	return schedModel{
		width:    width,
		height:   height,
		selected: 0,
		client:   client,
		formName: nameInput,
		formMsg:  msgInput,
		formCron: cronInput,
	}
}

// ---- key bindings for scheduler ----

type schedKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Toggle    key.Binding
	Delete    key.Binding
	Trigger   key.Binding
	New       key.Binding
	Edit      key.Binding
	Refresh   key.Binding
	Back      key.Binding
	Quit      key.Binding
}

func (k schedKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.New, k.Delete, k.Trigger, k.Back}
}

func (k schedKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Toggle},
		{k.New, k.Edit, k.Delete, k.Trigger},
		{k.Refresh, k.Back, k.Quit},
	}
}

var schedKeys = schedKeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Toggle:  key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "toggle enable")),
	Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete job")),
	Trigger: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "trigger run")),
	New:     key.NewBinding(key.WithKeys("n", "c"), key.WithHelp("n", "new job")),
	Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit job")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Back:    key.NewBinding(key.WithKeys("esc", "ctrl+j"), key.WithHelp("esc", "back to chat")),
	Quit:    key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
}

// ---- update ----

func (m schedModel) Update(msg tea.Msg) (schedModel, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle form mode
	if m.showForm {
		return m.updateForm(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, schedKeys.Back):
			return m, func() tea.Msg { return toggleSchedMsg{} }

		case key.Matches(msg, schedKeys.Quit):
			return m, tea.Quit

		case key.Matches(msg, schedKeys.Up):
			if m.selected > 0 {
				m.selected--
			}

		case key.Matches(msg, schedKeys.Down):
			if m.selected < len(m.jobs)-1 {
				m.selected++
			}

		case key.Matches(msg, schedKeys.Toggle):
			return m.toggleSelected()

		case key.Matches(msg, schedKeys.Delete):
			return m.deleteSelected()

		case key.Matches(msg, schedKeys.Trigger):
			return m.triggerSelected()

		case key.Matches(msg, schedKeys.New):
			m.showForm = true
			m.editing = false
			m.editID = ""
			m.formName.SetValue("")
			m.formMsg.SetValue("")
			m.formCron.SetValue("")
			m.formFocus = 0
			m.formName.Focus()
			m.formMsg.Blur()
			m.formCron.Blur()

		case key.Matches(msg, schedKeys.Edit):
			if len(m.jobs) > 0 && m.selected < len(m.jobs) {
				return m.editSelected()
			}

		case key.Matches(msg, schedKeys.Refresh):
			return m, loadSchedJobs(m.client)
		}

	case schedJobsLoaded:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.jobs = msg.jobs
			m.err = ""
		}
		if m.selected >= len(m.jobs) {
			m.selected = max(0, len(m.jobs)-1)
		}

	case schedJobToggled:
		m.notification = "Job toggled"
		return m, loadSchedJobs(m.client)

	case schedJobDeleted:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.notification = fmt.Sprintf("Deleted job %s", msg.id)
		}
		return m, loadSchedJobs(m.client)

	case schedJobTriggered:
		m.notification = fmt.Sprintf("Triggered job %s", msg.id)
	}
	return m, tea.Batch(cmds...)
}

func (m schedModel) toggleSelected() (schedModel, tea.Cmd) {
	if len(m.jobs) == 0 || m.selected >= len(m.jobs) {
		return m, nil
	}
	job := m.jobs[m.selected]
	newEnabled := !job.Enabled

	return m, func() tea.Msg {
		req := api.CreateJobRequest{Enabled: newEnabled}
		err := m.client.Put("/api/scheduler/"+job.ID, req, nil)
		if err != nil {
			return schedJobDeleted{err: err}
		}
		return schedJobToggled{}
	}
}

func (m schedModel) deleteSelected() (schedModel, tea.Cmd) {
	if len(m.jobs) == 0 || m.selected >= len(m.jobs) {
		return m, nil
	}
	job := m.jobs[m.selected]
	return m, func() tea.Msg {
		err := m.client.Delete("/api/scheduler/"+job.ID, nil)
		return schedJobDeleted{id: job.ID, err: err}
	}
}

func (m schedModel) triggerSelected() (schedModel, tea.Cmd) {
	if len(m.jobs) == 0 || m.selected >= len(m.jobs) {
		return m, nil
	}
	job := m.jobs[m.selected]
	return m, func() tea.Msg {
		_, err := m.client.StreamPost("/api/scheduler/"+job.ID+"/trigger", nil)
		if err == nil {
			// close the response body; it's a no-op response
		}
		return schedJobTriggered{id: job.ID}
	}
}

func (m schedModel) editSelected() (schedModel, tea.Cmd) {
	if m.selected >= len(m.jobs) {
		return m, nil
	}
	job := m.jobs[m.selected]
	m.showForm = true
	m.editing = true
	m.editID = job.ID
	m.formName.SetValue(job.Name)
	m.formMsg.SetValue(job.Message)
	if job.CronExpr != "" {
		m.formCron.SetValue(job.CronExpr)
	} else {
		m.formCron.SetValue(job.ScheduleType)
	}
	m.formFocus = 0
	m.formName.Focus()
	m.formMsg.Blur()
	m.formCron.Blur()
	return m, nil
}

// ---- form update ----

func (m schedModel) updateForm(msg tea.Msg) (schedModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.showForm = false
			m.notification = "Cancelled"
			return m, nil

		case "tab":
			m.formFocus = (m.formFocus + 1) % 3
			m.formName.Blur()
			m.formMsg.Blur()
			m.formCron.Blur()
			switch m.formFocus {
			case 0:
				m.formName.Focus()
			case 1:
				m.formMsg.Focus()
			case 2:
				m.formCron.Focus()
			}
			return m, nil

		case "shift+tab":
			m.formFocus = (m.formFocus - 1 + 3) % 3
			m.formName.Blur()
			m.formMsg.Blur()
			m.formCron.Blur()
			switch m.formFocus {
			case 0:
				m.formName.Focus()
			case 1:
				m.formMsg.Focus()
			case 2:
				m.formCron.Focus()
			}
			return m, nil

		case "ctrl+s", "enter":
			// Enter always submits the form
			return m.submitForm()
		}
	}

	// Update focused input
	var cmd tea.Cmd
	switch m.formFocus {
	case 0:
		m.formName, cmd = m.formName.Update(msg)
	case 1:
		m.formMsg, cmd = m.formMsg.Update(msg)
	case 2:
		m.formCron, cmd = m.formCron.Update(msg)
	}
	return m, cmd
}

func (m schedModel) submitForm() (schedModel, tea.Cmd) {
	name := strings.TrimSpace(m.formName.Value())
	msg_ := strings.TrimSpace(m.formMsg.Value())
	cron := strings.TrimSpace(m.formCron.Value())

	if name == "" || msg_ == "" {
		m.notification = "Name and message are required"
		return m, nil
	}

	m.showForm = false
	editing := m.editing
	editID := m.editID

	return m, func() tea.Msg {
		req := api.CreateJobRequest{
			Name:         name,
			Message:      msg_,
			ScheduleType: "cron",
			CronExpr:     cron,
			Enabled:      true,
		}
		var apiErr error
		if editing {
			apiErr = m.client.Put("/api/scheduler/"+editID, req, nil)
		} else {
			apiErr = m.client.Post("/api/scheduler/", req, nil)
		}
		if apiErr != nil {
			return schedJobsLoaded{err: apiErr}
		}
		// Reload jobs
		var result api.SchedulerListResponse
		if err := m.client.Get("/api/scheduler/", &result); err != nil {
			return schedJobsLoaded{err: err}
		}
		return schedJobsLoaded{jobs: result.Jobs, err: nil}
	}
}

// ---- view ----

func (m schedModel) View() string {
	if m.showForm {
		return m.viewForm()
	}
	return m.viewList()
}

func (m schedModel) viewList() string {
	var sb strings.Builder

	// Title
	title := orchLogo + " " + lipgloss.NewStyle().Foreground(accent).Bold(true).Render("Scheduler")
	sb.WriteString(headerStyle.Width(m.width).Render(title))
	sb.WriteString("\n\n")

	// Column headers
	colStyle := lipgloss.NewStyle().Foreground(grayLight).Bold(true)
	headerLine := fmt.Sprintf("%-36s  %-10s  %-12s  %-20s  %s",
		colStyle.Render("ID"),
		colStyle.Render("Enabled"),
		colStyle.Render("Schedule"),
		colStyle.Render("Last Run"),
		colStyle.Render("Name"),
	)
	sb.WriteString(headerLine)
	sb.WriteString("\n")
	separator := lipgloss.NewStyle().Foreground(grayDark).Render(strings.Repeat("─", m.width-4))
	sb.WriteString(separator)
	sb.WriteString("\n")

	if len(m.jobs) == 0 {
		if m.err != "" {
			sb.WriteString(lipgloss.NewStyle().Foreground(red).Render("Error: " + m.err))
		} else {
			sb.WriteString(lipgloss.NewStyle().Foreground(textDim).Render("No scheduled jobs. Press 'n' to create one."))
		}
	} else {
		for i, job := range m.jobs {
			sb.WriteString(m.renderJobRow(job, i == m.selected))
			sb.WriteString("\n")
		}
	}

	// Notification
	if m.notification != "" {
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(green).Render(m.notification))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(separator)
	sb.WriteString("\n")

	// Help keys
	helpText := lipgloss.JoinHorizontal(lipgloss.Left,
		statusKeyStyle.Render("↑↓"), statusInfoStyle.Render("navigate  "),
		statusKeyStyle.Render("enter"), statusInfoStyle.Render("toggle  "),
		statusKeyStyle.Render("n"), statusInfoStyle.Render("new  "),
		statusKeyStyle.Render("e"), statusInfoStyle.Render("edit  "),
		statusKeyStyle.Render("d"), statusInfoStyle.Render("delete  "),
		statusKeyStyle.Render("t"), statusInfoStyle.Render("trigger  "),
		statusKeyStyle.Render("r"), statusInfoStyle.Render("refresh  "),
		statusKeyStyle.Render("esc"), statusInfoStyle.Render("chat"),
	)
	sb.WriteString(helpText)

	return sb.String()
}

func (m schedModel) renderJobRow(job api.SchedulerJob, selected bool) string {
	idStr := job.ID
	if len(idStr) > 12 {
		idStr = idStr[:12]
	}

	enabledStr := "✗"
	enabledStyle := lipgloss.NewStyle().Foreground(red)
	if job.Enabled {
		enabledStr = "✓"
		enabledStyle = lipgloss.NewStyle().Foreground(green)
	}

	schedule := job.CronExpr
	if schedule == "" {
		schedule = job.ScheduleType
	}

	lastRun := job.LastRunAt
	if lastRun == "" {
		lastRun = "never"
	} else if len(lastRun) > 19 {
		lastRun = lastRun[0:19]
	}

	rowStyle := lipgloss.NewStyle().Foreground(textWhite)
	bgStyle := lipgloss.NewStyle()
	if selected {
		bgStyle = lipgloss.NewStyle().Background(grayDark)
	}

	row := fmt.Sprintf("%-36s  %-10s  %-12s  %-20s  %s",
		idStr,
		enabledStyle.Render(enabledStr),
		lipgloss.NewStyle().Foreground(cyan).Render(schedule),
		lipgloss.NewStyle().Foreground(textDim).Render(lastRun),
		rowStyle.Render(job.Name),
	)

	return bgStyle.Render(row)
}

func (m schedModel) viewForm() string {
	var sb strings.Builder

	actionText := "New Job"
	if m.editing {
		actionText = "Edit Job"
	}

	title := orchLogo + " " + lipgloss.NewStyle().Foreground(accent).Bold(true).Render("Scheduler: "+actionText)
	sb.WriteString(headerStyle.Width(m.width).Render(title))
	sb.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().Foreground(grayLight)
	sb.WriteString(helpStyle.Render("Tab: next field  Enter: submit  Esc: cancel"))
	sb.WriteString("\n\n")

	// Form fields
	labelStyle := lipgloss.NewStyle().Foreground(accentDim).Bold(true)

	sb.WriteString(labelStyle.Render("Name:"))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(m.formName.View()))
	sb.WriteString("\n\n")

	sb.WriteString(labelStyle.Render("Prompt message:"))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(m.formMsg.View()))
	sb.WriteString("\n\n")

	sb.WriteString(labelStyle.Render("Schedule (cron expression):"))
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().PaddingLeft(2).Render(
		lipgloss.NewStyle().Foreground(textDim).Render("e.g. @daily, 0 9 * * *, @hourly, @weekly") + "\n" +
			m.formCron.View(),
	))
	sb.WriteString("\n\n")

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Left,
		statusKeyStyle.Render("Tab"), statusInfoStyle.Render("next  "),
		statusKeyStyle.Render("Enter"), statusInfoStyle.Render("submit  "),
		statusKeyStyle.Render("Esc"), statusInfoStyle.Render("cancel"),
	))

	return sb.String()
}

// ---- toggle message ----

type toggleSchedMsg struct{}

// ---- load jobs command ----

func loadSchedJobs(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		var result api.SchedulerListResponse
		if err := client.Get("/api/scheduler/", &result); err != nil {
			return schedJobsLoaded{err: err}
		}
		return schedJobsLoaded{jobs: result.Jobs}
	}
}
