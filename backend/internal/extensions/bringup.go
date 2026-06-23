package extensions

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// CloneAndStart clones the source repo (if not already cached) and starts the
// bringup script as a long-running subprocess, expecting it to print
// PUX_EXT_PORT:<port> on stdout.
//
// Used by the PreWarmer (Phase 3) for capability tiers that declare
// `source: git+...` + `bringup: <shell>`. The Manager tracks the running
// subprocess under `prefix` so StopAll / Restart can reach it later.
//
// `bringup` is the LONG-RUNNING server-start command — NOT one-shot setup.
// Per the RFC verification snippet (`bringup: python3 server.py`), the field
// covers both install and run via shell chaining: `pip install -r reqs.txt &&
// python3 server.py`. The kernel does not separate setup from run; the script
// does.
//
// Returns the port the subprocess bound to. Caller registers an MCP client
// pointing at http://127.0.0.1:<port>/mcp.
func (m *Manager) CloneAndStart(ctx context.Context, source, bringup, prefix string) (int, error) {
	if strings.TrimSpace(bringup) == "" {
		return 0, fmt.Errorf("bringup script is empty (prefix=%s, source=%s)", prefix, source)
	}

	cacheDir, err := cacheDirFor(source)
	if err != nil {
		return 0, fmt.Errorf("compute cache dir: %w", err)
	}

	if err := m.cloneOrPull(ctx, source, cacheDir); err != nil {
		return 0, fmt.Errorf("clone %s: %w", source, err)
	}

	// Construct an Extension that re-uses startOne for the PUX_EXT_PORT
	// handshake. Timeout is generous (120s) because bringup may include
	// dependency install before the server actually starts.
	ext := &Extension{
		Name: prefix,
		Dir:  cacheDir,
		Server: ServerConfig{
			Command: "bash",
			Args:    []string{"-c", bringup},
			Timeout: 120,
			Restart: "on-failure",
		},
	}

	port, cmd, err := m.startOne(ctx, ext)
	if err != nil {
		return 0, fmt.Errorf("start subprocess: %w", err)
	}

	m.mu.Lock()
	m.exts = append(m.exts, &ManagedExtension{
		Extension: *ext,
		cmd:       cmd,
		port:      port,
	})
	m.mu.Unlock()

	m.logger.Info("extension cloned and started",
		zap.String("name", prefix),
		zap.String("source", source),
		zap.Int("port", port))

	return port, nil
}

// cloneOrPull clones the source URL into cacheDir. If cacheDir already has
// a .git directory, runs `git pull` instead. Source may be `git+https://...`
// or `git+file:///...` — the `git+` prefix is stripped before passing to git.
//
// Network failures return an error; the caller decides whether to skip the
// tier or retry. Existing cache + failed pull = use the cache as-is (logged).
func (m *Manager) cloneOrPull(ctx context.Context, source, cacheDir string) error {
	gitURL := strings.TrimPrefix(source, "git+")

	if _, err := os.Stat(filepath.Join(cacheDir, ".git")); err == nil {
		// Cache hit — pull for updates. Failure is non-fatal: we use what we have.
		pullCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(pullCtx, "git", "-C", cacheDir, "pull", "--ff-only")
		if out, err := cmd.CombinedOutput(); err != nil {
			m.logger.Warn("git pull failed (using cached clone)",
				zap.String("source", source),
				zap.Error(err),
				zap.ByteString("output", out))
		}
		return nil
	}

	// Fresh clone.
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}

	cloneCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", gitURL, cacheDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %w; output: %s", err, out)
	}
	return nil
}

// cacheDirFor returns the path under ~/.pux/ext-cache/ where this source
// should be cloned. Uses sha256(source) so the same source maps to the same
// dir across kernel restarts — clone is a one-time cost per source.
func cacheDirFor(source string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(strings.TrimPrefix(source, "git+")))
	short := fmt.Sprintf("%x", h[:8])
	return filepath.Join(home, ".pux", "ext-cache", short), nil
}
