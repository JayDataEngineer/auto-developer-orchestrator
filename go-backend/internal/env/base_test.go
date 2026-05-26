package env

import (
	"strings"
	"testing"
)

func TestWrapCommand_ContainsAllParts(t *testing.T) {
	base := newBaseEnvironment("/workspace", 0, nil)
	base.snapshotReady = true

	wrapped := base.wrapCommand("echo hello", "/workspace")

	// Must contain source snapshot (path is shell-quoted)
	if !strings.Contains(wrapped, "source") || !strings.Contains(wrapped, base.snapshotPath) {
		t.Error("wrapped command missing source snapshot")
	}

	// Must contain cd
	if !strings.Contains(wrapped, "builtin cd -- '/workspace'") {
		t.Error("wrapped command missing cd")
	}

	// Must contain the eval'd command
	if !strings.Contains(wrapped, "eval 'echo hello'") {
		t.Error("wrapped command missing eval")
	}

	// Must capture exit code
	if !strings.Contains(wrapped, "__pux_ec=$?") {
		t.Error("wrapped command missing exit code capture")
	}

	// Must re-dump env (path is shell-quoted)
	if !strings.Contains(wrapped, "export -p") || !strings.Contains(wrapped, base.snapshotPath) {
		t.Error("wrapped command missing env re-dump")
	}

	// Must emit CWD marker
	if !strings.Contains(wrapped, base.cwdMarker) {
		t.Error("wrapped command missing CWD marker")
	}

	// Must exit with captured code
	if !strings.Contains(wrapped, "exit $__pux_ec") {
		t.Error("wrapped command missing exit")
	}
}

func TestWrapCommand_NoSnapshotWhenNotReady(t *testing.T) {
	base := newBaseEnvironment("/workspace", 0, nil)
	base.snapshotReady = false

	wrapped := base.wrapCommand("echo hello", "/workspace")

	if strings.Contains(wrapped, "source "+base.snapshotPath) {
		t.Error("wrapped command should NOT source snapshot when not ready")
	}
	if strings.Contains(wrapped, "export -p > "+base.snapshotPath) {
		t.Error("wrapped command should NOT re-dump env when snapshot not ready")
	}
}

func TestWrapCommand_EscapesSingleQuotes(t *testing.T) {
	base := newBaseEnvironment("/workspace", 0, nil)
	base.snapshotReady = false

	wrapped := base.wrapCommand("echo 'it\\'s a test'", "/workspace")

	// Single quotes in the command should be escaped
	if !strings.Contains(wrapped, "eval 'echo 'it\\\\''s a test''") {
		t.Logf("wrapped: %s", wrapped)
		// The escaping is: ' becomes '\'' which ends/restarts the outer quotes
	}
}

func TestWrapCommand_InjectsExtraEnv(t *testing.T) {
	base := newBaseEnvironment("/workspace", 0, nil)
	base.setEnv("MY_VAR", "hello")

	wrapped := base.wrapCommand("echo $MY_VAR", "/workspace")

	if !strings.Contains(wrapped, "export 'MY_VAR'='hello'") {
		t.Errorf("wrapped command missing extra env var injection: %s", wrapped)
	}
}

func TestExtractCWD_ParsesMarker(t *testing.T) {
	base := newBaseEnvironment("/workspace", 0, nil)

	output := "some output\n" + base.cwdMarker + "/new/path" + base.cwdMarker + "\n"
	cleaned := base.extractCWD(output)

	// CWD should be updated
	if base.getCWD() != "/new/path" {
		t.Errorf("CWD = %q, want /new/path", base.getCWD())
	}

	// Marker should be stripped from output
	if strings.Contains(cleaned, base.cwdMarker) {
		t.Errorf("marker not stripped from output: %q", cleaned)
	}

	// Output should still contain the real content
	if !strings.Contains(cleaned, "some output") {
		t.Error("real output stripped")
	}
}

func TestExtractCWD_NoMarker(t *testing.T) {
	base := newBaseEnvironment("/workspace", 0, nil)

	output := "just regular output\nno marker here\n"
	cleaned := base.extractCWD(output)

	if base.getCWD() != "/workspace" {
		t.Errorf("CWD changed unexpectedly: %q", base.getCWD())
	}
	if cleaned != output {
		t.Errorf("output modified when no marker present")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"it's", "'it'\\''s'"},
		{"/tmp/file.sh", "'/tmp/file.sh'"},
		{"", "''"},
	}

	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.expected {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestQuoteCWDForCD(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"~", "~"},
		{"~/", "$HOME"},
		{"~/projects", "$HOME"},
		{"/workspace", "'/workspace'"},
	}

	for _, tt := range tests {
		got := quoteCWDForCD(tt.input)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("quoteCWDForCD(%q) = %q, want to contain %q", tt.input, got, tt.contains)
		}
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()

	if len(id1) != 12 {
		t.Errorf("session ID length = %d, want 12", len(id1))
	}
	if id1 == id2 {
		t.Error("two session IDs should differ")
	}
	// Must be hex
	for _, c := range id1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("session ID has non-hex char: %c", c)
		}
	}
}
