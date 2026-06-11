package env

import (
	"bytes"
	"context"
	"fmt"
	"time"

	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"golang.org/x/crypto/ssh"
)

// SSHProjectInfo carries the parsed SSH URL components.
// Shared with handlers.SSHProjectInfo to avoid duplication.
type SSHProjectInfo struct {
	User string
	Host string
	Port string
	Path string // remote directory path
}

// SSHEnvironment runs commands on a remote machine via x/crypto/ssh.
// Uses the existing ssh.SessionManager connection pool.
// CWD is tracked via in-band stdout markers.
type SSHEnvironment struct {
	baseEnvironment
	sessions *puxssh.SessionManager
	info     SSHProjectInfo
}

// NewSSHEnvironment creates an SSH-backed execution environment.
// The connection must already be established via SessionManager.Connect().
func NewSSHEnvironment(sessions *puxssh.SessionManager, info SSHProjectInfo, cwd string, timeout time.Duration) *SSHEnvironment {
	if cwd == "" {
		cwd = info.Path
	}
	if cwd == "" {
		cwd = "$HOME"
	}
	return &SSHEnvironment{
		baseEnvironment: newBaseEnvironment(cwd, timeout, NewSecurityGuard()),
		sessions:        sessions,
		info:            info,
	}
}

func (s *SSHEnvironment) Execute(ctx context.Context, command string, opts ExecuteOptions) (*ExecuteResult, error) {
	return s.baseEnvironment.Execute(ctx, command, opts, s.runBash)
}

func (s *SSHEnvironment) InitSession(ctx context.Context) error {
	return s.baseEnvironment.InitSession(ctx, s.runBash)
}

func (s *SSHEnvironment) CWD() string {
	return s.getCWD()
}

func (s *SSHEnvironment) SetEnv(key, value string) {
	s.setEnv(key, value)
}

func (s *SSHEnvironment) Close() error {
	// Clean up remote temp files (best-effort)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.runBash(ctx, fmt.Sprintf("rm -f %s %s", shellQuote(s.snapshotPath), shellQuote(s.cwdFilePath)), false, 5*time.Second, "")
	return nil
}

func (s *SSHEnvironment) runBash(ctx context.Context, cmd string, login bool, timeout time.Duration, stdinData string) (*BackendResult, error) {
	if timeout == 0 {
		timeout = s.defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	clientKey := fmt.Sprintf("%s@%s:%s", s.info.User, s.info.Host, s.info.Port)
	client, ok := s.sessions.GetClientByKey(clientKey)
	if !ok {
		return nil, fmt.Errorf("SSH not connected to %s", clientKey)
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("SSH session create failed: %w", err)
	}
	defer session.Close()

	// Set up stdin if provided
	if stdinData != "" {
		stdin, err := session.StdinPipe()
		if err == nil {
			go func() {
				stdin.Write([]byte(stdinData))
				stdin.Close()
			}()
		}
	}

	// Build bash command
	var bashCmd string
	if login {
		bashCmd = "bash -l -c " + shellQuote(cmd)
	} else {
		bashCmd = "bash -c " + shellQuote(cmd)
	}

	var output bytes.Buffer
	session.Stdout = &output
	session.Stderr = &output

	err = session.Run(bashCmd)
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			return nil, fmt.Errorf("SSH exec failed: %w", err)
		}
	}

	return &BackendResult{
		Output:   output.String(),
		ExitCode: exitCode,
	}, nil
}

