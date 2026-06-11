package env

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LocalEnvironment runs commands on the host machine via os/exec.
// CWD is tracked via temp file (read after each command) and stdout markers.
type LocalEnvironment struct {
	baseEnvironment
	workDir string
}

// NewLocalEnvironment creates a local execution environment.
func NewLocalEnvironment(cwd string, timeout time.Duration) *LocalEnvironment {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return &LocalEnvironment{
		baseEnvironment: newBaseEnvironment(cwd, timeout, NewSecurityGuard()),
		workDir:         cwd,
	}
}

// NewLocalEnvironmentWithGuard creates a local environment with a custom security guard.
func NewLocalEnvironmentWithGuard(cwd string, timeout time.Duration, guard *SecurityGuard) *LocalEnvironment {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return &LocalEnvironment{
		baseEnvironment: newBaseEnvironment(cwd, timeout, guard),
		workDir:         cwd,
	}
}

func (l *LocalEnvironment) Execute(ctx context.Context, command string, opts ExecuteOptions) (*ExecuteResult, error) {
	return l.baseEnvironment.Execute(ctx, command, opts, l.runBash)
}

func (l *LocalEnvironment) InitSession(ctx context.Context) error {
	return l.baseEnvironment.InitSession(ctx, l.runBash)
}

func (l *LocalEnvironment) CWD() string {
	return l.getCWD()
}

func (l *LocalEnvironment) SetEnv(key, value string) {
	l.setEnv(key, value)
}

func (l *LocalEnvironment) Close() error {
	// Clean up temp files
	os.Remove(l.snapshotPath)
	os.Remove(l.cwdFilePath)
	return nil
}

// updateCWD reads the CWD from the temp file for local execution.
// This is more reliable than parsing stdout markers on the local backend.
func (l *LocalEnvironment) updateCWD(cleanedOutput, rawOutput string) {
	data, err := os.ReadFile(l.cwdFilePath)
	if err == nil && len(data) > 0 {
		cwd := strings.TrimSpace(string(data))
		if cwd != "" {
			l.mu.Lock()
			l.cwd = cwd
			l.mu.Unlock()
		}
	}
}

func (l *LocalEnvironment) runBash(ctx context.Context, cmd string, login bool, timeout time.Duration, stdinData string) (*BackendResult, error) {
	if timeout == 0 {
		timeout = l.defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var args []string
	if login {
		args = []string{"-l", "-c", cmd}
	} else {
		args = []string{"-c", cmd}
	}

	proc := exec.CommandContext(ctx, "bash", args...)
	proc.Dir = l.workDir

	if stdinData != "" {
		proc.Stdin = strings.NewReader(stdinData)
	}

	var output strings.Builder
	proc.Stdout = &output
	proc.Stderr = &output

	err := proc.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("local exec failed: %w", err)
		}
	}

	return &BackendResult{
		Output:   output.String(),
		ExitCode: exitCode,
	}, nil
}
