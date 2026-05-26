package env

import (
	"context"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
)

// DockerEnvironment runs commands inside a Docker sandbox via sandbox.Manager.
// CWD is tracked via in-band stdout markers.
type DockerEnvironment struct {
	baseEnvironment
	mgr       *sandbox.Manager
	sandboxID string
}

// NewDockerEnvironment creates a Docker-backed execution environment.
func NewDockerEnvironment(mgr *sandbox.Manager, sandboxID, cwd string, timeout time.Duration) *DockerEnvironment {
	return &DockerEnvironment{
		baseEnvironment: newBaseEnvironment(cwd, timeout, NewSecurityGuard()),
		mgr:             mgr,
		sandboxID:       sandboxID,
	}
}

func (d *DockerEnvironment) Execute(ctx context.Context, command string, opts ExecuteOptions) (*ExecuteResult, error) {
	return d.baseEnvironment.Execute(ctx, command, opts, d.runBash)
}

func (d *DockerEnvironment) InitSession(ctx context.Context) error {
	return d.baseEnvironment.InitSession(ctx, d.runBash)
}

func (d *DockerEnvironment) CWD() string {
	return d.getCWD()
}

func (d *DockerEnvironment) SetEnv(key, value string) {
	d.setEnv(key, value)
}

func (d *DockerEnvironment) Close() error {
	// Clean up snapshot files inside the container (best-effort)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.mgr.ExecInSandbox(ctx, d.sandboxID, []string{
		"rm", "-f", d.snapshotPath, d.cwdFilePath,
	})
	return nil
}

func (d *DockerEnvironment) runBash(ctx context.Context, cmd string, login bool, timeout time.Duration, stdinData string) (*BackendResult, error) {
	if timeout == 0 {
		timeout = d.defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build bash args
	var args []string
	if login {
		args = []string{"bash", "-l", "-c", cmd}
	} else {
		args = []string{"bash", "-c", cmd}
	}

	output, exitCode, err := d.mgr.ExecInSandboxRaw(ctx, d.sandboxID, args)
	if err != nil {
		return nil, fmt.Errorf("docker exec failed: %w", err)
	}

	return &BackendResult{
		Output:   output,
		ExitCode: exitCode,
	}, nil
}
