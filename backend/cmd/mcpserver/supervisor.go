// supervisor.go holds the PID-file + daemonization helpers backing the
// `mcpserver start | stop | status` subcommands. Lives in package main
// alongside main.go so the run/start/stop/status commands share types.
//
// Design notes:
//
//   - PID file location is per-project (`<project>/.pux/mcpserver.pid`) so
//     one project = one server = one PID file. Matches the single-tenant
//     contract. Override via `PUX_PID_FILE` for unusual setups.
//
//   - Daemonization is the standard detached-spawn pattern: `exec.Command`
//     with `Setsid: true` creates a new session, `cmd.Process.Release()`
//     detaches so the child survives the parent's exit. No double-fork
//     needed on Linux.
//
//   - Stale detection: `start` reads any existing PID file and probes
//     liveness via `kill(pid, 0)`. Dead → clean up. Live → refuse (or
//     `--force` to stop-then-start).
//
//   - `run` (foreground) writes the PID file at boot and removes it on
//     clean signal-driven exit. Crashes leave stale files; that's the
//     signal for the next `start` to clean up.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// pidFileName is the file placed under <project>/.pux/ when no override
// is set. Single file per project — matches the single-tenant contract.
const pidFileName = "mcpserver.pid"

// pidFileEntry is the JSON body of the PID file. Every field is meant for
// operator consumption via `mcpserver status` — keep it small + readable.
type pidFileEntry struct {
	PID         int       `json:"pid"`
	Addr        string    `json:"addr"`
	Project     string    `json:"project"`
	SandboxID   string    `json:"sandbox_id"`
	ContainerID string    `json:"container_id,omitempty"`
	StartedAt   time.Time `json:"started_at"`
}

// resolvePIDFile picks the PID file path for a given project. Precedence:
//  1. $PUX_PID_FILE (operator override)
//  2. <project>/.pux/mcpserver.pid (default — single-tenant per-project)
//
// projectPath must be absolute (caller's responsibility).
func resolvePIDFile(projectPath string) string {
	if v := os.Getenv("PUX_PID_FILE"); v != "" {
		return v
	}
	return filepath.Join(projectPath, ".pux", pidFileName)
}

// writePIDFile atomically writes the PID file. Creates the parent directory
// (the `.pux/` folder convention) if missing. Returns an error if the file
// already exists — callers should removePIDFile or refuse-start.
func writePIDFile(path string, entry pidFileEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create pid file dir: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("pid file already exists: %s", path)
	}
	body, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pid file: %w", err)
	}
	// 0644: readable by operator tools (pux-history, jq), writable only
	// by the owning user.
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

// readPIDFile reads + unmarshals the PID file. Returns a wrapped error if
// the file is missing or malformed — callers distinguish via errors.Is on
// os.IsNotExist.
func readPIDFile(path string) (pidFileEntry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return pidFileEntry{}, err
	}
	var entry pidFileEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		return pidFileEntry{}, fmt.Errorf("parse pid file %s: %w", path, err)
	}
	return entry, nil
}

// removePIDFile is a best-effort unlink — errors are returned but callers
// usually log-and-continue since a stale file is recoverable on next start.
func removePIDFile(path string) error {
	return os.Remove(path)
}

// isProcessAlive returns true if the given PID is currently running. Uses
// `kill(pid, 0)` (signal 0 = existence check, no actual signal delivered).
// On Linux, PID 1 is always alive (init) — the caller is responsible for
// not testing that path unless it's meaningful.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// ESRCH = no such process. EPERM = exists but not ours (treat
		// as alive — something else owns it).
		return errors.Is(err, os.ErrPermission)
	}
	return true
}

// stopProcess sends SIGTERM, polls up to wait for the process to exit,
// then SIGKILLs if still alive. Returns nil if the process exited (or
// was already gone), an error if SIGKILL failed.
func stopProcess(pid int, wait time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid: %d", pid)
	}
	if !isProcessAlive(pid) {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM: %w", err)
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("SIGKILL: %w", err)
	}
	return nil
}

// daemonize spawns the binary (os.Args[0]) with the given args in a new
// session, redirecting stdout+stderr to logPath (or /dev/null if empty).
// The child is fully detached — releasing the process handle means the
// caller (the `start` subcommand) can return immediately and the child
// survives. Returns the child PID on success.
//
// logPath "-" keeps stdio inherited from the parent (useful under
// `task start` where task captures stderr).
func daemonize(args []string, logPath string) (int, error) {
	bin, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve executable: %w", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = nil

	switch logPath {
	case "", "/dev/null":
		devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return 0, fmt.Errorf("open /dev/null: %w", err)
		}
		cmd.Stdout = devnull
		cmd.Stderr = devnull
	case "-":
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	default:
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return 0, fmt.Errorf("open log file: %w", err)
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	// Setsid: new session + new process group, detached from the
	// parent's controlling terminal. This is the Linux idiom for
	// "daemonize without a double-fork".
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("spawn child: %w", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		// Non-fatal — the child is running, we just couldn't release
		// the handle cleanly. Log + continue.
		fmt.Fprintf(os.Stderr, "warn: process release: %v\n", err)
	}
	return pid, nil
}

// parseIntOrZero parses s as a positive int, returning 0 on error. Used
// for the --wait flag on `stop`. Mirrors envOrZero's semantics in main.go.
func parseIntOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
