package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLoggerDisabledWhenPathEmpty proves the opt-in contract: empty path
// means no file is opened, Log is a no-op, Close is a no-op.
func TestLoggerDisabledWhenPathEmpty(t *testing.T) {
	l, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): unexpected error %v", err)
	}
	if l != nil {
		t.Fatalf("Open(\"\") should return nil logger, got %v", l)
	}
	// All methods must be safe on nil *Logger.
	l.Log(Entry{Tool: "bash"})
	if err := l.Close(); err != nil {
		t.Fatalf("nil.Close() should be no-op, got %v", err)
	}
}

// TestLoggerAppendsEntries proves a basic happy-path write: open, log two
// entries, read them back as JSONL.
func TestLoggerAppendsEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	l.Log(Entry{
		Timestamp:  time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
		SessionID:  "abc123",
		Tool:       "bash",
		Args:       map[string]any{"command": "echo hi"},
		Result:     "hi\n",
		DurationMs: 5,
	})
	l.Log(Entry{
		Timestamp: time.Date(2026, 6, 26, 12, 0, 1, 0, time.UTC),
		Tool:      "python",
		Args:      map[string]any{"code": "print(1)"},
		Error:     "SyntaxError",
	})

	entries := readJSONL(t, path)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0]["tool"] != "bash" {
		t.Errorf("entry[0] tool: got %v want bash", entries[0]["tool"])
	}
	if entries[1]["error"] != "SyntaxError" {
		t.Errorf("entry[1] error: got %v want SyntaxError", entries[1]["error"])
	}
}

// TestLoggerScrubsSecrets proves a secret in args/result gets redacted
// before hitting the file. The audit log is for humans to inspect; we
// don't want to also be the leak vector.
func TestLoggerScrubsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	// Fake fixture — matches scrubber regex `sk-[a-zA-Z0-9\-_]{20,}` but
	// not a real key. See secret_leak_test.go for the rationale.
	l.Log(Entry{
		Tool:   "bash",
		Args:   map[string]any{"command": "echo sk-test-FAKE-FIXTURE-not-a-real-key-1234567890"},
		Result: "sk-test-FAKE-FIXTURE-not-a-real-key-1234567890\n",
	})

	entries := readJSONL(t, path)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	args, _ := entries[0]["args"].(string)
	if strings.Contains(args, "sk-test-FAKE-FIXTURE") {
		t.Errorf("LEAK: secret in args: %q", args)
	}
	result, _ := entries[0]["result"].(string)
	if strings.Contains(result, "sk-test-FAKE-FIXTURE") {
		t.Errorf("LEAK: secret in result: %q", result)
	}
}

// TestLoggerTruncatesLargeValues proves bash output that's 50MB doesn't
// produce a 50MB audit line. The cap is 4096 bytes per field.
func TestLoggerTruncatesLargeValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	big := strings.Repeat("x", 1024*1024) // 1 MiB
	l.Log(Entry{Tool: "bash", Args: big, Result: big})

	// Read raw bytes — we want to check file size, not parsed length.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// One JSONL line. With cap=4096 + truncation marker + JSON overhead,
	// total line should be well under 10 KiB even though inputs were 1 MiB.
	if len(data) > 20*1024 {
		t.Errorf("audit line too long: %d bytes (cap should keep it ~10KiB)", len(data))
	}
	if !strings.Contains(string(data), "...[truncated]") {
		t.Errorf("expected truncation marker in audit line")
	}
}

// TestLoggerConcurrentWriters proves the mutex works — N goroutines
// hammering Log produce exactly N well-formed JSONL lines, no interleaving.
func TestLoggerConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			l.Log(Entry{Tool: "bash", Args: i})
		}(i)
	}
	wg.Wait()

	entries := readJSONL(t, path)
	if len(entries) != n {
		t.Errorf("expected %d entries, got %d (some writes lost or interleaved)", n, len(entries))
	}
}

// readJSONL parses a JSONL file into a slice of generic maps. Fails the
// test if any line is malformed JSON — that's the interleaving signal.
func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var out []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var m map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			t.Fatalf("malformed JSONL line (likely concurrent interleaving): %v\nline=%q", err, scanner.Text())
		}
		out = append(out, m)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return out
}
