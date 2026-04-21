package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primary   = lipgloss.Color("#00ff00")
	dimText   = lipgloss.Color("#666666")
	cyan      = lipgloss.Color("#00cccc")
	yellow    = lipgloss.Color("#cccc00")
	red       = lipgloss.Color("#cc0000")
	white     = lipgloss.Color("#ffffff")
	zinc400   = lipgloss.Color("#a1a1aa")

	// Styles
	titleStyle = lipgloss.NewStyle().
			Foreground(primary).
			Bold(true)

	userMsgStyle = lipgloss.NewStyle().
			Foreground(white).
			Bold(true)

	assistantStyle = lipgloss.NewStyle()

	thinkingStyle = lipgloss.NewStyle().
			Foreground(cyan).
			Faint(true)

	toolStyle = lipgloss.NewStyle().
			Foreground(yellow)

	toolResultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00cc00"))

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(red)

	errorStyle = lipgloss.NewStyle().
			Foreground(red).
			Bold(true)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(dimText).
			Background(lipgloss.Color("#111111"))

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(primary).
				Bold(true)

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#333333"))

	artifactStyle = lipgloss.NewStyle().
			Foreground(primary).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#222222"))

	approvalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(yellow).
			Foreground(yellow)
)
