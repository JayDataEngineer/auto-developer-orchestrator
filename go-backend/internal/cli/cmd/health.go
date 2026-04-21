package cli

import (
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/cli/api"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check backend health",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := api.NewClient(serverURL)
		err := client.Get("/api/health", nil)
		if err != nil {
			return fmt.Errorf("backend not healthy: %w", err)
		}
		fmt.Println("OK")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(healthCmd)
}
