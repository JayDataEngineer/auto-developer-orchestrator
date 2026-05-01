package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	serverURL string
	outputFmt string
	projectName string
)

// rootCmd is the base command. Running `orch` with no subcommand launches chat TUI.
var rootCmd = &cobra.Command{
	Use:   "orch",
	Short: "Auto-Developer Orchestrator CLI",
	Long:  "CLI for the Auto-Developer Orchestrator. Run 'orch chat' for interactive mode, or use subcommands for scripting.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runChat(cmd, args)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", envOr("ORCH_SERVER_URL", "http://localhost:3847"), "Backend URL")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text|json")
	rootCmd.PersistentFlags().StringVarP(&projectName, "project", "p", envOr("ORCH_PROJECT", ""), "Project name")

	// Register 'chat' as an alias for the default TUI mode
	rootCmd.AddCommand(&cobra.Command{
		Use:   "chat",
		Short: "Launch interactive TUI chat mode",
		RunE:  runChat,
	})
}

func Execute() error {
	return rootCmd.Execute()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireProject() error {
	if projectName == "" {
		return fmt.Errorf("project is required: use --project or set ORCH_PROJECT")
	}
	return nil
}
