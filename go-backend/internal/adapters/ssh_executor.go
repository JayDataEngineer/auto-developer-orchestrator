package adapters

import (
	"bytes"
	"context"
	"fmt"

	gossh "golang.org/x/crypto/ssh"
)

// SSHExecutor runs commands on a remote host via SSH.
// Implements tools/bash.Executor for SSH-backed projects.
type SSHExecutor struct {
	Client  *gossh.Client
	WorkDir string // remote working directory (optional)
}

func (e *SSHExecutor) Exec(ctx context.Context, command string) (string, error) {
	if e.Client == nil {
		return "", fmt.Errorf("ssh exec: no SSH client")
	}

	// Wrap in cd if WorkDir set
	if e.WorkDir != "" {
		command = fmt.Sprintf("cd %s 2>/dev/null; %s", e.WorkDir, command)
	}

	sess, err := e.Client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh exec: new session: %w", err)
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	if err := sess.Run(command); err != nil {
		return stdout.String(), fmt.Errorf("ssh exec: %w\n%s", err, stderr.String())
	}
	return stdout.String(), nil
}
