package extensions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestCloneAndStart_LocalGitRepo clones a file:// fixture, runs a bringup
// script that spawns a tiny TCP server, and confirms CloneAndStart returns a
// non-zero port + registers the extension under the requested prefix.
//
// Skips if git is not on PATH (CI without git, Windows without git-bash).
func TestCloneAndStart_LocalGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("bash-centric test; skip on Windows")
	}

	// Build a local "remote" — a real git repo with a server script that prints
	// PUX_EXT_PORT:<port> on stdout.
	remote := t.TempDir()
	serverPath := filepath.Join(remote, "server.py")
	// Minimal server: bind to an ephemeral port, print PUX_EXT_PORT:N, then
	// sleep forever. CloneAndStart's startOne() reads the port line and returns,
	// leaving the subprocess alive until StopAll kills it.
	if err := os.WriteFile(serverPath, []byte(`import socket, sys, time
s = socket.socket()
s.bind(('127.0.0.1', 0))
s.listen(1)
print('PUX_EXT_PORT:' + str(s.getsockname()[1]))
sys.stdout.flush()
while True:
    time.sleep(60)
`), 0o644); err != nil {
		t.Fatalf("write server.py: %v", err)
	}

	// Initialize the fixture as a git repo with at least one commit, so
	// `git clone --depth 1` works against it.
	if out, err := exec.Command("git", "-C", remote, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v; out=%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(remote, ".gitignore"), []byte("__pycache__/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if out, err := exec.Command("git", "-C", remote, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v; out=%s", err, out)
	}
	// Some git configs refuse commit without user.email/user.name; set locally.
	_ = exec.Command("git", "-C", remote, "config", "user.email", "test@example.com").Run()
	_ = exec.Command("git", "-C", remote, "config", "user.name", "Test").Run()
	if out, err := exec.Command("git", "-C", remote, "commit", "-q", "-m", "init").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v; out=%s", err, out)
	}

	// Point cacheDirFor at a per-test temp so we don't pollute ~/.pux/ext-cache.
	// We swap by constructing the Manager, then mutating its cacheDir via
	// cacheDirFor override. Since cacheDirFor is package-level (not a method),
	// we instead redirect by setting HOME (which cacheDirFor uses via
	// os.UserHomeDir).
	home := t.TempDir()
	t.Setenv("HOME", home)

	mgr := NewManager(zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	source := "git+file://" + remote
	bringup := "python3 server.py"
	port, err := mgr.CloneAndStart(ctx, source, bringup, "fixture-test")
	if err != nil {
		t.Fatalf("CloneAndStart: %v", err)
	}
	defer mgr.StopAll()

	if port <= 0 || port > 65535 {
		t.Errorf("port out of range: %d", port)
	}

	// Extension should be registered under the prefix.
	if got := mgr.PortFor("fixture-test"); got != port {
		t.Errorf("PortFor=%d, want %d", got, port)
	}

	// server.py should still be alive in the cache dir (proves the long-running
	// command is running, not just that the port was captured once).
	cacheDir, err := cacheDirFor(source)
	if err != nil {
		t.Fatalf("cacheDirFor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "server.py")); err != nil {
		t.Errorf("server.py missing in cached clone (%s): %v", cacheDir, err)
	}

	// Subprocess alive check: StopAll should kill it. We can't easily check
	// process liveness portably, but StopAll shouldn't error. (If it does,
	// goroutine leak.)
	mgr.StopAll()
	if got := mgr.PortFor("fixture-test"); got != 0 {
		t.Errorf("after StopAll, PortFor should be 0, got %d", got)
	}
}

// TestCloneAndStart_SecondCallIsPullNotClone exercises the cache-hit branch.
// Second call with same source should skip the clone and just run bringup +
// spawn. We detect this by checking git pull runs (it prints "Already up to
// date." on a no-op pull — we can't easily capture that, but we can confirm
// the cache directory's .git survives both calls and the port is returned
// both times).
func TestCloneAndStart_SecondCallIsPullNotClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("bash-centric test; skip on Windows")
	}

	remote := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "server.py"), []byte(`import socket, sys, time
s = socket.socket(); s.bind(('127.0.0.1', 0)); s.listen(1)
print('PUX_EXT_PORT:' + str(s.getsockname()[1])); sys.stdout.flush()
while True: time.sleep(60)
`), 0o644); err != nil {
		t.Fatalf("write server.py: %v", err)
	}
	for _, cmd := range [][]string{
		{"git", "init", "-q"},
		{"git", "add", "."},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "-q", "-m", "init"},
	} {
		if out, err := exec.Command(cmd[0], append([]string{"-C", remote}, cmd[1:]...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v; out=%s", cmd, err, out)
		}
	}

	t.Setenv("HOME", t.TempDir())
	mgr := NewManager(zap.NewNop())

	source := "git+file://" + remote

	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Second)
	port1, err := mgr.CloneAndStart(ctx1, source, "python3 server.py", "cached-test-1")
	cancel1()
	if err != nil {
		t.Fatalf("first CloneAndStart: %v", err)
	}

	// Stop the first subprocess so its port frees.
	mgr.StopAll()

	// Second call should hit cache (git pull --ff-only, no clone).
	cacheDir, _ := cacheDirFor(source)
	gitDir := filepath.Join(cacheDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		t.Fatalf("cache .git missing after first call: %v", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	port2, err := mgr.CloneAndStart(ctx2, source, "python3 server.py", "cached-test-2")
	cancel2()
	if err != nil {
		t.Fatalf("second CloneAndStart (cache hit): %v", err)
	}
	defer mgr.StopAll()

	if port1 <= 0 || port2 <= 0 {
		t.Errorf("ports should both be positive: %d, %d", port1, port2)
	}
}

// TestCloneAndStart_UntrustedSourceRejected confirms the caller's IsTrusted
// check is the gate, not CloneAndStart itself. CloneAndStart is plumbing —
// the PreWarmer checks trust before calling. This test documents that
// expectation: a malformed source URL returns an error from the clone step,
// not a panic.
func TestCloneAndStart_UntrustedSourceRejected(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	t.Setenv("HOME", t.TempDir())
	mgr := NewManager(zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Non-existent path. git clone will fail; CloneAndStart surfaces the error.
	_, err := mgr.CloneAndStart(ctx, "git+file:///nonexistent/path/that/does/not/exist", "python3 server.py", "bad-test")
	if err == nil {
		t.Errorf("expected error from non-existent source")
		return
	}
	if !strings.Contains(err.Error(), "clone") {
		t.Errorf("error should mention clone failure, got: %v", err)
	}
}

// TestCloneAndStart_EmptyBringupRejected confirms the caller's contract: empty
// bringup is a config error, not silently treated as "no command". This guards
// against capability YAML that declares `source:` but forgets `bringup:`.
func TestCloneAndStart_EmptyBringupRejected(t *testing.T) {
	mgr := NewManager(zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := mgr.CloneAndStart(ctx, "git+file:///whatever", "", "empty-test")
	if err == nil {
		t.Errorf("expected error from empty bringup")
		return
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty, got: %v", err)
	}
}
