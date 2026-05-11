package adapters

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// HostExecutor runs commands directly on the host machine.
// Implements tools/bash.Executor for native sandbox tier.
type HostExecutor struct {
	WorkDir string // working directory for commands (optional)
}

func (h *HostExecutor) Exec(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if h.WorkDir != "" {
		cmd.Dir = h.WorkDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("host exec: %w\n%s", err, stderr.String())
	}
	return stdout.String(), nil
}
