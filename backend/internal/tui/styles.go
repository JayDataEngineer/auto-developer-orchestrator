// styles.go holds all lipgloss styles for the TUI. Centralizing them makes
// it easy to tweak the visual identity in one place and keeps View() short.

package tui

import "github.com/charmbracelet/lipgloss"

// App-wide palette. Stays close to common terminal defaults so it renders
// well in both light and dark themes.
const (
	colorAccent   = "13" // magenta — used for the brand + active hints
	colorUser     = "12" // bright blue — user turns
	colorAssistant = "10" // bright green — assistant turns (success-ish)
	colorMuted    = "8"  // bright black (gray) — secondary text
	colorError    = "9"  // bright red — errors + failed status
	colorOK       = "10" // bright green — complete status
	colorWarn     = "11" // bright yellow — running status
)

var (
	// brandStyle is the top-left logo/identifier.
	brandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorAccent))

	// topBarStyle is the whole status strip at the top.
	topBarStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// orgBadgeStyle is the current org label in the top bar.
	orgBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorAccent)).
			Bold(true)

	// statusIdleStyle / statusRunningStyle / statusCompleteStyle /
	// statusFailedStyle color the status text per the task lifecycle.
	statusIdleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted))
	statusRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorWarn))
	statusCompleteStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorOK))
	statusFailedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorError))

	// userLabelStyle / assistantLabelStyle prefix each turn bubble.
	userLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorUser)).
			Bold(true)
	assistantLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorAssistant)).
				Bold(true)

	// mutedStyle is for the hint bar + secondary text.
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted))

	// errorStyle is for in-pane error messages.
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorError)).
			Bold(true)

	// historyCursorStyle highlights the selected row in history mode.
	historyCursorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(colorAccent))
)
