package env

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// SubprocessSSHBulkTransfer implements BulkTransfer by shelling out to the
// system `ssh` binary. Shares the ControlMaster connection from
// SubprocessSSHEnvironment.
//
// Ported from Hermes SSHEnvironment._ssh_bulk_upload / _ssh_bulk_download.
type SubprocessSSHBulkTransfer struct {
	env *SubprocessSSHEnvironment
}

// NewSubprocessSSHBulkTransfer creates a bulk transfer client that shares
// the ControlMaster connection from an existing SubprocessSSHEnvironment.
func NewSubprocessSSHBulkTransfer(env *SubprocessSSHEnvironment) *SubprocessSSHBulkTransfer {
	return &SubprocessSSHBulkTransfer{env: env}
}

// Upload transfers files via tar-over-SSH pipe using subprocess.
// Ported from Hermes SSHEnvironment._ssh_bulk_upload().
//
// 1. Creates remote parent directories via `ssh ... mkdir -p`
// 2. Builds tar archive in memory from file pairs
// 3. Pipes tar through SSH: `tar | ssh ... tar xf --no-overwrite-dir`
func (t *SubprocessSSHBulkTransfer) Upload(ctx context.Context, files [][2]string) error {
	if len(files) == 0 {
		return nil
	}

	// Batch-create parent directories on remote
	parents := uniqueParentDirs(files)
	if len(parents) > 0 {
		mkdirCmd := "mkdir -p " + strings.Join(shellQuoteAll(parents), " ")
		if err := t.runRemote(ctx, mkdirCmd, 30*time.Second); err != nil {
			return fmt.Errorf("remote mkdir failed: %w", err)
		}
	}

	// Build tar archive in memory
	var tarBuf bytes.Buffer
	if err := writeTarFromPairs(&tarBuf, files); err != nil {
		return fmt.Errorf("tar create failed: %w", err)
	}

	// Extract on remote side.
	// --no-overwrite-dir prevents overwriting existing directory modes
	// (protects sshd StrictModes).
	remoteBase := t.env.CWD()
	if remoteBase == "" || remoteBase == "$HOME" {
		remoteBase = "$HOME"
	}
	extractCmd := fmt.Sprintf("tar xf - --no-overwrite-dir -C %s", shellQuote(remoteBase))

	sshArgs := t.env.buildSSHArgs()
	sshArgs = append(sshArgs, extractCmd)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	sshCmd := exec.CommandContext(ctx, "ssh", sshArgs...)

	// Feed tar via stdin
	stdinPipe, err := sshCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("ssh stdin pipe: %w", err)
	}

	var stderr bytes.Buffer
	sshCmd.Stderr = &stderr
	// Discard stdout (tar extract produces none)
	sshCmd.Stdout = nil

	if err := sshCmd.Start(); err != nil {
		return fmt.Errorf("ssh start: %w", err)
	}

	// Write tar data to stdin in goroutine (avoids deadlock)
	writeDone := make(chan error, 1)
	go func() {
		_, err := stdinPipe.Write(tarBuf.Bytes())
		stdinPipe.Close()
		writeDone <- err
	}()

	// Wait for stdin write
	if writeErr := <-writeDone; writeErr != nil {
		sshCmd.Process.Kill()
		sshCmd.Wait()
		return fmt.Errorf("tar pipe write failed: %w", writeErr)
	}

	// Wait for remote extraction
	if err := sshCmd.Wait(); err != nil {
		return fmt.Errorf("remote tar extract failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}

	return nil
}

// Download fetches files from the remote as a tar archive via SSH subprocess.
// Ported from Hermes SSHEnvironment._ssh_bulk_download().
func (t *SubprocessSSHBulkTransfer) Download(ctx context.Context, remoteBase string) (string, error) {
	if remoteBase == "" {
		remoteBase = t.env.CWD()
	}
	if remoteBase == "" || remoteBase == "$HOME" {
		remoteBase = "$HOME"
	}

	// Build tar command on remote using absolute path structure
	relBase := strings.TrimPrefix(remoteBase, "/")
	if relBase == "" {
		relBase = "."
	}
	tarCmd := fmt.Sprintf("tar cf - -C / %s", shellQuote(relBase))

	sshArgs := t.env.buildSSHArgs()
	sshArgs = append(sshArgs, tarCmd)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	sshCmd := exec.CommandContext(ctx, "ssh", sshArgs...)

	var stdout, stderr bytes.Buffer
	sshCmd.Stdout = &stdout
	sshCmd.Stderr = &stderr

	if err := sshCmd.Run(); err != nil {
		return "", fmt.Errorf("remote tar create failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}

	// Extract to staging directory
	staging, err := os.MkdirTemp("", "pux-sync-back-")
	if err != nil {
		return "", fmt.Errorf("staging dir: %w", err)
	}

	if err := extractTar(bytes.NewReader(stdout.Bytes()), staging); err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("tar extract failed: %w", err)
	}

	return staging, nil
}

// Delete removes files from the remote in a single SSH call.
// Ported from Hermes SSHEnvironment._ssh_delete().
func (t *SubprocessSSHBulkTransfer) Delete(ctx context.Context, remotePaths []string) error {
	if len(remotePaths) == 0 {
		return nil
	}
	rmCmd := "rm -f " + strings.Join(shellQuoteAll(remotePaths), " ")
	return t.runRemote(ctx, rmCmd, 30*time.Second)
}

// runRemote executes a single command on the remote via SSH.
func (t *SubprocessSSHBulkTransfer) runRemote(ctx context.Context, cmd string, timeout time.Duration) error {
	sshArgs := t.env.buildSSHArgs()
	sshArgs = append(sshArgs, cmd)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sshCmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	output, err := sshCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}
