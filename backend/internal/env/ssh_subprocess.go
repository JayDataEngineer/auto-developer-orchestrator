package env

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SubprocessSSHEnvironment runs commands on a remote machine by shelling out
// to the system `ssh` binary. This matches Hermes-agent's approach exactly.
//
// Uses SSH ControlMaster for connection reuse — one TCP connection shared
// across all invocations. Works with Tailscale SSH, jump hosts,
// ~/.ssh/config ProxyCommands, and any SSH feature the system client supports.
//
// Every Execute() spawns a new `ssh` process, but ControlMaster makes each
// invocation lightweight (no TCP handshake, no key exchange — just local mux).
type SubprocessSSHEnvironment struct {
	baseEnvironment
	host          string
	user          string
	port          string
	keyPath       string
	controlDir    string
	controlSocket string
}

// NewSubprocessSSHEnvironment creates an SSH environment that uses the system
// `ssh` binary. Falls back to x/crypto/ssh if `ssh` is not found.
//
// Ported from Hermes SSHEnvironment.__init__().
func NewSubprocessSSHEnvironment(user, host, port, keyPath, cwd string, timeout time.Duration) *SubprocessSSHEnvironment {
	if port == "" {
		port = "22"
	}
	if cwd == "" {
		cwd = "$HOME"
	}

	controlDir := filepath.Join(os.TempDir(), "pux-ssh")
	os.MkdirAll(controlDir, 0o700)

	// Deterministic socket name — short for macOS sun_path (104 byte limit)
	socketID := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s@%s:%s", user, host, port))))[:16]
	controlSocket := filepath.Join(controlDir, socketID+".sock")

	return &SubprocessSSHEnvironment{
		baseEnvironment: newBaseEnvironment(cwd, timeout, NewSecurityGuard()),
		host:            host,
		user:            user,
		port:            port,
		keyPath:         keyPath,
		controlDir:      controlDir,
		controlSocket:   controlSocket,
	}
}

// Execute runs a command on the remote via ssh subprocess.
func (e *SubprocessSSHEnvironment) Execute(ctx context.Context, command string, opts ExecuteOptions) (*ExecuteResult, error) {
	return e.baseEnvironment.Execute(ctx, command, opts, e.runBash)
}

// InitSession establishes the SSH connection and captures login shell env.
// Ported from Hermes SSHEnvironment.init_session().
func (e *SubprocessSSHEnvironment) InitSession(ctx context.Context) error {
	// Establish ControlMaster connection first
	if err := e.establishConnection(ctx); err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}

	// Detect remote home directory
	home := e.detectRemoteHome(ctx)
	if home != "" {
		e.mu.Lock()
		if e.cwd == "$HOME" {
			e.cwd = home
		}
		e.mu.Unlock()
	}

	return e.baseEnvironment.InitSession(ctx, e.runBash)
}

func (e *SubprocessSSHEnvironment) CWD() string   { return e.getCWD() }
func (e *SubprocessSSHEnvironment) SetEnv(k, v string) { e.setEnv(k, v) }

// Close shuts down the ControlMaster connection and cleans up.
// Ported from Hermes SSHEnvironment.cleanup().
func (e *SubprocessSSHEnvironment) Close() error {
	// Send ControlMaster exit signal
	args := []string{
		"-o", "ControlPath=" + e.controlSocket,
		"-O", "exit",
		fmt.Sprintf("%s@%s", e.user, e.host),
	}
	if e.port != "22" {
		args = append([]string{"-p", e.port}, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Run() // best-effort

	// Remove stale socket
	os.Remove(e.controlSocket)
	return nil
}

// buildSSHArgs returns the common SSH arguments for all invocations.
// Ported from Hermes SSHEnvironment._build_ssh_command().
func (e *SubprocessSSHEnvironment) buildSSHArgs(extraArgs ...string) []string {
	args := []string{
		"-o", "ControlPath=" + e.controlSocket,
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=300",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
	}
	if e.port != "22" {
		args = append(args, "-p", e.port)
	}
	if e.keyPath != "" {
		args = append(args, "-i", e.keyPath)
	}
	args = append(args, extraArgs...)
	args = append(args, fmt.Sprintf("%s@%s", e.user, e.host))
	return args
}

// establishConnection creates the ControlMaster connection.
// Ported from Hermes SSHEnvironment._establish_connection().
func (e *SubprocessSSHEnvironment) establishConnection(ctx context.Context) error {
	args := e.buildSSHArgs()
	args = append(args, "echo 'SSH connection established'")

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// detectRemoteHome runs `echo $HOME` on the remote to find the user's home.
// Ported from Hermes SSHEnvironment._detect_remote_home().
func (e *SubprocessSSHEnvironment) detectRemoteHome(ctx context.Context) string {
	args := e.buildSSHArgs()
	args = append(args, "echo $HOME")

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh", args...)
	output, err := cmd.Output()
	if err != nil {
		if e.user == "root" {
			return "/root"
		}
		return "/home/" + e.user
	}

	home := strings.TrimSpace(string(output))
	if home != "" {
		return home
	}
	if e.user == "root" {
		return "/root"
	}
	return "/home/" + e.user
}

// runBash executes a bash command on the remote via ssh subprocess.
// Ported from Hermes SSHEnvironment._run_bash().
func (e *SubprocessSSHEnvironment) runBash(ctx context.Context, cmd string, login bool, timeout time.Duration, stdinData string) (*BackendResult, error) {
	if timeout == 0 {
		timeout = e.defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build the remote bash command
	var remoteCmd string
	if login {
		remoteCmd = "bash -l -c " + shellQuote(cmd)
	} else {
		remoteCmd = "bash -c " + shellQuote(cmd)
	}

	args := e.buildSSHArgs()
	args = append(args, remoteCmd)

	sshCmd := exec.CommandContext(ctx, "ssh", args...)

	// Stdin handling — pipe via goroutine to avoid deadlocks
	// Ported from Hermes base._pipe_stdin()
	if stdinData != "" {
		stdinPipe, err := sshCmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("ssh stdin pipe: %w", err)
		}
		go func() {
			stdinPipe.Write([]byte(stdinData))
			stdinPipe.Close()
		}()
	} else {
		sshCmd.Stdin = nil
	}

	// Merge stderr into stdout (like Hermes stderr=subprocess.STDOUT)
	var output bytes.Buffer
	sshCmd.Stdout = &output
	sshCmd.Stderr = &output

	err := sshCmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("ssh exec failed: %w", err)
		}
	}

	return &BackendResult{
		Output:   output.String(),
		ExitCode: exitCode,
	}, nil
}

// RemoteHome returns the detected remote home directory.
func (e *SubprocessSSHEnvironment) RemoteHome() string {
	return e.detectRemoteHome(context.Background())
}
