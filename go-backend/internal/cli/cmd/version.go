package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/auto-developer-orchestrator/backend/internal"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("orch v%s\n", internal.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
