package env

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"golang.org/x/crypto/ssh"
)

// BulkTransfer provides streaming file transfer for environments that need
// it (SSH, Docker). Bind-mount backends (local) don't need this.
//
// Ported from Hermes SSHEnvironment._ssh_bulk_upload/_ssh_bulk_download.
// Uses tar-over-SSH and tar-over-docker-exec to transfer many files in
// a single TCP stream instead of per-file SFTP/scp.
type BulkTransfer interface {
	// Upload transfers multiple files to the remote environment.
	// files is a list of (local_path, remote_path) pairs.
	Upload(ctx context.Context, files [][2]string) error

	// Download fetches files from the remote environment into a staging dir.
	// Returns the staging directory path (caller must clean up).
	Download(ctx context.Context, remoteBase string) (stagingDir string, err error)

	// Delete removes files from the remote environment.
	Delete(ctx context.Context, remotePaths []string) error
}

// ── SSH Bulk Transfer ──

// SSHBulkTransfer implements BulkTransfer over SSH using tar pipes.
// Uses the existing SessionManager connection pool.
type SSHBulkTransfer struct {
	sessions *puxssh.SessionManager
	info     SSHProjectInfo
}

// NewSSHBulkTransfer creates a bulk transfer client for an SSH target.
func NewSSHBulkTransfer(sessions *puxssh.SessionManager, info SSHProjectInfo) *SSHBulkTransfer {
	return &SSHBulkTransfer{sessions: sessions, info: info}
}

func (t *SSHBulkTransfer) clientKey() string {
	return fmt.Sprintf("%s@%s:%s", t.info.User, t.info.Host, t.info.Port)
}

func (t *SSHBulkTransfer) newSession() (*ssh.Session, error) {
	client, ok := t.sessions.GetClientByKey(t.clientKey())
	if !ok {
		return nil, fmt.Errorf("SSH not connected to %s", t.clientKey())
	}
	return client.NewSession()
}

// Upload transfers files via tar-over-SSH pipe.
// Creates remote directories first, then streams a tar archive through SSH
// to extract on the remote side in one TCP connection.
//
// Ported from Hermes SSHEnvironment._ssh_bulk_upload.
func (t *SSHBulkTransfer) Upload(ctx context.Context, files [][2]string) error {
	if len(files) == 0 {
		return nil
	}

	// Batch-create parent directories on remote
	parents := uniqueParentDirs(files)
	if len(parents) > 0 {
		mkdirCmd := "mkdir -p " + strings.Join(shellQuoteAll(parents), " ")
		session, err := t.newSession()
		if err != nil {
			return fmt.Errorf("ssh mkdir session: %w", err)
		}
		if err := session.Run(mkdirCmd); err != nil {
			session.Close()
			return fmt.Errorf("remote mkdir failed: %w", err)
		}
		session.Close()
	}

	// Build tar archive in memory from the file list
	var tarBuf bytes.Buffer
	if err := writeTarFromPairs(&tarBuf, files); err != nil {
		return fmt.Errorf("tar create failed: %w", err)
	}

	// Pipe tar through SSH to remote extraction.
	// --no-overwrite-dir prevents overwriting existing directory modes
	// (protects sshd StrictModes).
	remoteBase := t.info.Path
	if remoteBase == "" {
		remoteBase = "$HOME"
	}
	extractCmd := fmt.Sprintf("tar xf - --no-overwrite-dir -C %s", shellQuote(remoteBase))

	session, err := t.newSession()
	if err != nil {
		return fmt.Errorf("ssh upload session: %w", err)
	}
	defer session.Close()

	// Feed tar via stdin
	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("ssh stdin pipe: %w", err)
	}

	var stderr bytes.Buffer
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdinPipe, &tarBuf)
		stdinPipe.Close()
		done <- err
	}()

	if err := session.Start(extractCmd); err != nil {
		return fmt.Errorf("ssh tar extract start: %w", err)
	}

	// Wait for stdin copy to finish
	if copyErr := <-done; copyErr != nil {
		return fmt.Errorf("tar pipe failed: %w", copyErr)
	}

	// Wait for remote extraction
	if err := session.Wait(); err != nil {
		return fmt.Errorf("remote tar extract failed: %s: %w", stderr.String(), err)
	}

	return nil
}

// Download fetches the remote base directory as a tar archive and extracts
// it into a temporary staging directory.
//
// Ported from Hermes SSHEnvironment._ssh_bulk_download.
func (t *SSHBulkTransfer) Download(ctx context.Context, remoteBase string) (string, error) {
	if remoteBase == "" {
		remoteBase = t.info.Path
	}

	// tar the remote directory using absolute paths
	relBase := strings.TrimPrefix(remoteBase, "/")
	tarCmd := fmt.Sprintf("tar cf - -C / %s", shellQuote(relBase))

	session, err := t.newSession()
	if err != nil {
		return "", fmt.Errorf("ssh download session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(tarCmd); err != nil {
		return "", fmt.Errorf("remote tar create failed: %s: %w", stderr.String(), err)
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
func (t *SSHBulkTransfer) Delete(ctx context.Context, remotePaths []string) error {
	if len(remotePaths) == 0 {
		return nil
	}
	rmCmd := "rm -f " + strings.Join(shellQuoteAll(remotePaths), " ")

	session, err := t.newSession()
	if err != nil {
		return fmt.Errorf("ssh delete session: %w", err)
	}
	defer session.Close()

	if err := session.Run(rmCmd); err != nil {
		return fmt.Errorf("remote rm failed: %w", err)
	}
	return nil
}

// ── Docker Bulk Transfer ──

// DockerBulkTransfer implements BulkTransfer for Docker sandboxes.
// Uses tar-over-docker-exec instead of docker cp to avoid per-file overhead.
type DockerBulkTransfer struct {
	mgr       *sandbox.Manager
	sandboxID string
}

// NewDockerBulkTransfer creates a bulk transfer client for a Docker sandbox.
func NewDockerBulkTransfer(mgr *sandbox.Manager, sandboxID string) *DockerBulkTransfer {
	return &DockerBulkTransfer{mgr: mgr, sandboxID: sandboxID}
}

// Upload transfers files into the sandbox via base64-encoded tar over docker exec.
// Docker exec doesn't support streaming stdin like SSH, so we encode the tar
// as base64 and pipe it through the exec command.
func (t *DockerBulkTransfer) Upload(ctx context.Context, files [][2]string) error {
	if len(files) == 0 {
		return nil
	}

	// Create parent directories
	parents := uniqueParentDirs(files)
	if len(parents) > 0 {
		mkdirCmd := "mkdir -p " + strings.Join(shellQuoteAll(parents), " ")
		if _, err := t.mgr.ExecInSandbox(ctx, t.sandboxID, []string{"bash", "-c", mkdirCmd}); err != nil {
			return fmt.Errorf("sandbox mkdir failed: %w", err)
		}
	}

	// Build tar in memory
	var tarBuf bytes.Buffer
	if err := writeTarFromPairs(&tarBuf, files); err != nil {
		return fmt.Errorf("tar create failed: %w", err)
	}

	// Encode as base64 and pipe through docker exec
	encoded := base64.StdEncoding.EncodeToString(tarBuf.Bytes())
	extractCmd := fmt.Sprintf("echo '%s' | base64 -d | tar xf - --no-overwrite-dir -C /", encoded)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if _, err := t.mgr.ExecInSandbox(ctx, t.sandboxID, []string{"bash", "-c", extractCmd}); err != nil {
		return fmt.Errorf("sandbox tar extract failed: %w", err)
	}

	return nil
}

// Download fetches files from the sandbox as a tar archive.
func (t *DockerBulkTransfer) Download(ctx context.Context, remoteBase string) (string, error) {
	if remoteBase == "" {
		remoteBase = "/"
	}

	// Create tar inside sandbox and base64-encode it
	relBase := strings.TrimPrefix(remoteBase, "/")
	tarCmd := fmt.Sprintf("tar cf - -C / %s 2>/dev/null | base64 -w0", shellQuote(relBase))

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	output, err := t.mgr.ExecInSandbox(ctx, t.sandboxID, []string{"bash", "-c", tarCmd})
	if err != nil {
		return "", fmt.Errorf("sandbox tar create failed: %w", err)
	}

	// Decode and extract to staging
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(output))
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}

	staging, err := os.MkdirTemp("", "pux-sync-back-")
	if err != nil {
		return "", fmt.Errorf("staging dir: %w", err)
	}

	if err := extractTar(bytes.NewReader(data), staging); err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("tar extract failed: %w", err)
	}

	return staging, nil
}

// Delete removes files from the sandbox.
func (t *DockerBulkTransfer) Delete(ctx context.Context, remotePaths []string) error {
	if len(remotePaths) == 0 {
		return nil
	}
	rmCmd := "rm -f " + strings.Join(shellQuoteAll(remotePaths), " ")
	_, err := t.mgr.ExecInSandbox(ctx, t.sandboxID, []string{"bash", "-c", rmCmd})
	return err
}

// ── Shared helpers ──

// writeTarFromPairs creates a tar archive containing files from the pairs list.
// Each pair is (local_path, remote_path). The tar entries use remote_path as
// the name so extraction places files at the correct location.
func writeTarFromPairs(w io.Writer, files [][2]string) error {
	tw := tar.NewWriter(w)
	defer tw.Close()

	for _, pair := range files {
		localPath := pair[0]
		remotePath := pair[1]

		info, err := os.Stat(localPath)
		if err != nil {
			// Skip missing files — they may have been deleted between listing and upload
			continue
		}

		// Only regular files
		if !info.Mode().IsRegular() {
			continue
		}

		f, err := os.Open(localPath)
		if err != nil {
			continue
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			f.Close()
			continue
		}
		hdr.Name = remotePath

		if err := tw.WriteHeader(hdr); err != nil {
			f.Close()
			return fmt.Errorf("tar header for %s: %w", remotePath, err)
		}

		if _, err := io.CopyN(tw, f, info.Size()); err != nil {
			f.Close()
			return fmt.Errorf("tar copy for %s: %w", remotePath, err)
		}
		f.Close()
	}

	return nil
}

// extractTar extracts a tar archive from r into dst.
// Safety cap: refuses to extract archives larger than 2 GiB.
func extractTar(r io.Reader, dst string) error {
	// Read into memory to check size cap
	data, err := io.ReadAll(io.LimitReader(r, 2*1024*1024*1024+1))
	if err != nil {
		return fmt.Errorf("read tar: %w", err)
	}
	if len(data) > 2*1024*1024*1024 {
		return fmt.Errorf("tar archive exceeds 2 GiB safety cap (%d bytes)", len(data))
	}

	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Security: verify target stays within dst
		target := filepath.Join(dst, hdr.Name)
		cleanTarget := filepath.Clean(target)
		cleanDst := filepath.Clean(dst)
		if !strings.HasPrefix(cleanTarget, cleanDst+string(os.PathSeparator)) && cleanTarget != cleanDst {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir for %s: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			f.Close()
		}
	}

	return nil
}

// uniqueParentDirs extracts unique parent directories from file pairs.
func uniqueParentDirs(files [][2]string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, pair := range files {
		parent := filepath.Dir(pair[1])
		if !seen[parent] {
			seen[parent] = true
			result = append(result, parent)
		}
	}
	return result
}

// shellQuoteAll quotes a slice of strings for shell use.
func shellQuoteAll(paths []string) []string {
	result := make([]string, len(paths))
	for i, p := range paths {
		result[i] = shellQuote(p)
	}
	return result
}
