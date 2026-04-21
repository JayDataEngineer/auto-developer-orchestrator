package cli

import (
	"github.com/auto-developer-orchestrator/backend/internal/cli/tui"
	"github.com/spf13/cobra"
	tea "github.com/charmbracelet/bubbletea"
)

// runChat launches the Bubble Tea TUI.
func runChat(cmd *cobra.Command, args []string) error {
	if projectName == "" {
		projectName = "."
	}
	model := tui.NewModel(serverURL, projectName, "default")
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
