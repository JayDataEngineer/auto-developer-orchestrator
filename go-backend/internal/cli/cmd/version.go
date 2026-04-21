package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("orch v0.1.0")
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
