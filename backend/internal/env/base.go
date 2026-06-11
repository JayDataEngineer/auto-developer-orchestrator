package env

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// BackendResult is the raw result from a backend's runBash call.
type BackendResult struct {
	Output   string
	ExitCode int
}

// baseEnvironment provides shared session management logic for all backends.
// Subclasses implement runBash() to handle the actual process spawning.
//
// Session state model (ported from Hermes BaseEnvironment):
//  1. InitSession() runs `bash -l -c "export -p > /tmp/pux-snap-<id>.sh"` inside
//     the target environment. This captures env vars, functions, aliases.
//  2. Each Execute() call wraps the command:
//     a. source /tmp/pux-snap-<id>.sh  (restore env vars)
//     b. cd <tracked-cwd>              (restore working directory)
//     c. eval '<user-command>'         (run the actual command)
//     d. export -p > /tmp/pux-snap-<id>.sh  (re-dump env for next call)
//     e. pwd -P > /tmp/pux-cwd-<id>.txt     (persist CWD)
//     f. emit __PUX_CWD_<id>__ marker to stdout
//  3. After execution, updateCWD parses the marker from output.
type baseEnvironment struct {
	mu             sync.Mutex
	sessionID      string
	cwd            string
	defaultTimeout time.Duration
	snapshotPath   string // /tmp/pux-snap-<id>.sh
	cwdFilePath    string // /tmp/pux-cwd-<id>.txt
	cwdMarker      string // __PUX_CWD_<id>__
	snapshotReady  bool
	extraEnv       map[string]string // user-set env vars
	security       *SecurityGuard    // nil = no checks
}

func newBaseEnvironment(cwd string, timeout time.Duration, security *SecurityGuard) baseEnvironment {
	sessionID := generateSessionID()
	return baseEnvironment{
		sessionID:      sessionID,
		cwd:            cwd,
		defaultTimeout: timeout,
		snapshotPath:   fmt.Sprintf("/tmp/pux-snap-%s.sh", sessionID),
		cwdFilePath:    fmt.Sprintf("/tmp/pux-cwd-%s.txt", sessionID),
		cwdMarker:      fmt.Sprintf("__PUX_CWD_%s__", sessionID),
		snapshotReady:  false,
		extraEnv:       make(map[string]string),
		security:       security,
	}
}

// runBash is the abstract method each backend implements.
// It runs a bash command string and returns raw output + exit code.
// The base class handles wrapping; the subclass handles transport.
type runBashFunc func(ctx context.Context, cmd string, login bool, timeout time.Duration, stdinData string) (*BackendResult, error)

// Execute is the unified entry point. It wraps the command, runs it via
// the backend, parses CWD markers, and applies security checks.
func (b *baseEnvironment) Execute(ctx context.Context, command string, opts ExecuteOptions, runBash runBashFunc) (*ExecuteResult, error) {
	b.mu.Lock()
	cwd := b.cwd
	if opts.CWD != "" {
		cwd = opts.CWD
	}
	b.mu.Unlock()

	// Security check: scan command for path violations
	if b.security != nil {
		if err := b.security.CheckCommand(command); err != nil {
			return &ExecuteResult{ExitCode: 126, Output: err.Error()}, nil
		}
	}

	// Wrap the command with session snapshot sourcing + CWD tracking
	wrapped := b.wrapCommand(command, cwd)

	timeout := b.defaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	result, err := runBash(ctx, wrapped, false, timeout, opts.StdinData)
	if err != nil {
		return nil, err
	}

	// Parse CWD marker from output
	output := b.extractCWD(result.Output)

	// Read CWD from file (local backends) or use marker-parsed value
	b.updateCWD(output, result.Output)

	return &ExecuteResult{
		Output:   output,
		ExitCode: result.ExitCode,
	}, nil
}

// InitSession captures login shell environment into a snapshot file.
// Falls back gracefully if the snapshot fails — subsequent commands
// will use `bash -l` instead of sourcing the snapshot.
func (b *baseEnvironment) InitSession(ctx context.Context, runBash runBashFunc) error {
	quotedCWD := shellQuote(b.cwd)
	quotedSnap := shellQuote(b.snapshotPath)
	quotedCwdFile := shellQuote(b.cwdFilePath)

	bootstrap := strings.Join([]string{
		fmt.Sprintf("export -p > %s", quotedSnap),
		"declare -f >> " + quotedSnap,
		"alias -p >> " + quotedSnap,
		"echo 'shopt -s expand_aliases' >> " + quotedSnap,
		"echo 'set +e' >> " + quotedSnap,
		"echo 'set +u' >> " + quotedSnap,
		fmt.Sprintf("builtin cd %s 2>/dev/null || true", quotedCWD),
		fmt.Sprintf("pwd -P > %s 2>/dev/null || true", quotedCwdFile),
		fmt.Sprintf("printf '\\n%s%%s%s\\n' \"$(pwd -P)\"", b.cwdMarker, b.cwdMarker),
	}, "\n")

	result, err := runBash(ctx, bootstrap, true, 30*time.Second, "")
	if err != nil {
		// Non-fatal — commands will work without snapshot, just no env persistence
		b.snapshotReady = false
		return fmt.Errorf("session snapshot failed: %w (commands will run without env persistence)", err)
	}

	b.snapshotReady = true

	// Parse CWD from the init output
	cleanedOutput := b.extractCWD(result.Output)
	b.updateCWD(cleanedOutput, result.Output)

	return nil
}

// wrapCommand builds the full bash script that sources snapshot, cd's,
// runs command, re-dumps env vars, and emits CWD markers.
//
// Ported from Hermes BaseEnvironment._wrap_command().
func (b *baseEnvironment) wrapCommand(command string, cwd string) string {
	escaped := strings.ReplaceAll(command, "'", "'\\''")

	quotedSnap := shellQuote(b.snapshotPath)
	quotedCwdFile := shellQuote(b.cwdFilePath)
	quotedCWD := quoteCWDForCD(cwd)

	var parts []string

	// Source snapshot (env vars from previous commands)
	if b.snapshotReady {
		parts = append(parts, fmt.Sprintf("source %s >/dev/null 2>&1 || true", quotedSnap))
	}

	// Inject extra env vars set via SetEnv
	b.mu.Lock()
	for k, v := range b.extraEnv {
		parts = append(parts, fmt.Sprintf("export %s=%s", shellQuote(k), shellQuote(v)))
	}
	b.mu.Unlock()

	// cd to tracked directory
	parts = append(parts, fmt.Sprintf("builtin cd -- %s || exit 126", quotedCWD))

	// Run the actual command
	parts = append(parts, fmt.Sprintf("eval '%s'", escaped))
	parts = append(parts, "__pux_ec=$?")

	// Re-dump env vars to snapshot
	if b.snapshotReady {
		parts = append(parts, fmt.Sprintf("export -p > %s 2>/dev/null || true", quotedSnap))
	}

	// Write CWD to file and stdout marker
	parts = append(parts, fmt.Sprintf("pwd -P > %s 2>/dev/null || true", quotedCwdFile))
	parts = append(parts, fmt.Sprintf("printf '\\n%s%%s%s\\n' \"$(pwd -P)\"", b.cwdMarker, b.cwdMarker))
	parts = append(parts, "exit $__pux_ec")

	return strings.Join(parts, "\n")
}

// extractCWD parses the CWD marker from output, strips it, and returns
// the cleaned output. The marker format is:
//
//	__PUX_CWD_<id>__/some/path__PUX_CWD_<id>__

func (b *baseEnvironment) extractCWD(output string) string {
	pattern := fmt.Sprintf(`\n?%s([^\n]+)%s\n?`,
		regexp.QuoteMeta(b.cwdMarker),
		regexp.QuoteMeta(b.cwdMarker),
	)
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(output)
	if len(matches) >= 2 {
		// Strip the marker from output
		cleaned := re.ReplaceAllString(output, "")
		// Update tracked CWD
		b.mu.Lock()
		b.cwd = matches[1]
		b.mu.Unlock()
		return cleaned
	}
	return output
}

// updateCWD is called after extractCWD. For remote backends the CWD is
// already updated via the marker. Local backends can override this to
// read from the cwdFilePath instead.
func (b *baseEnvironment) updateCWD(cleanedOutput, rawOutput string) {
	// Marker already parsed in extractCWD. This is a hook for subclasses.
}

func (b *baseEnvironment) getCWD() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cwd
}

func (b *baseEnvironment) setEnv(key, value string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.extraEnv[key] = value
}

// quoteCWDForCD quotes a cd target while preserving ~ expansion.
func quoteCWDForCD(cwd string) string {
	if cwd == "~" {
		return cwd
	}
	if cwd == "~/" {
		return "$HOME"
	}
	if strings.HasPrefix(cwd, "~/") {
		return "$HOME/" + shellQuote(cwd[2:])
	}
	return shellQuote(cwd)
}

// shellQuote wraps a string in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func generateSessionID() string {
	b := make([]byte, 6) // 12 hex chars
	rand.Read(b)
	return hex.EncodeToString(b)
}
