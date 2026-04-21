package main

import (
	"os"

	cli "github.com/auto-developer-orchestrator/backend/internal/cli/cmd"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
