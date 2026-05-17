package truncate

import (
	"strings"
	"testing"
)

// ── Head truncation ──────────────────────────────────────────────────

func TestHead_NoTruncation(t *testing.T) {
	content := "line1\nline2\nline3"
	r := Head(content, 10, 1024)
	if r.Truncated {
		t.Error("should not truncate small content")
	}
	if r.OutputLines != 3 {
		t.Errorf("expected 3 output lines, got %d", r.OutputLines)
	}
	if r.Content != content {
		t.Error("content should be unchanged")
	}
}

func TestHead_TruncationByLines(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "short line"
	}
	content := strings.Join(lines, "\n")

	r := Head(content, 10, 1024)
	if !r.Truncated {
		t.Error("should truncate")
	}
	if r.TruncatedBy != "lines" {
		t.Errorf("expected truncation by lines, got %s", r.TruncatedBy)
	}
	if r.OutputLines != 10 {
		t.Errorf("expected 10 output lines, got %d", r.OutputLines)
	}
	if r.TotalLines != 100 {
		t.Errorf("expected 100 total lines, got %d", r.TotalLines)
	}
}

func TestHead_TruncationByBytes(t *testing.T) {
	lines := []string{"a long line with lots of content that should exceed the byte limit when combined"}
	for i := 0; i < 20; i++ {
		lines = append(lines, "another line that adds bytes to the total")
	}
	content := strings.Join(lines, "\n")

	r := Head(content, 2000, 100) // tiny byte limit
	if !r.Truncated {
		t.Error("should truncate")
	}
	if r.TruncatedBy != "bytes" {
		t.Errorf("expected truncation by bytes, got %s", r.TruncatedBy)
	}
	if r.OutputBytes > 100 {
		t.Errorf("output should be under byte limit, got %d", r.OutputBytes)
	}
}

func TestHead_FirstLineExceedsLimit(t *testing.T) {
	longLine := strings.Repeat("x", 60000)
	content := longLine + "\nline2\nline3"

	r := Head(content, 2000, 50000)
	if !r.Truncated {
		t.Error("should truncate")
	}
	if !r.FirstLineExceedsLimit {
		t.Error("first line should exceed limit")
	}
	if r.Content != "" {
		t.Error("content should be empty when first line exceeds limit")
	}
}

func TestHead_NeverSplitsMidLine(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "this is a complete line"
	}
	content := strings.Join(lines, "\n")

	r := Head(content, 10, 50000)
	for _, line := range strings.Split(r.Content, "\n") {
		if line != "this is a complete line" {
			t.Errorf("line was split: %q", line)
		}
	}
}

func TestHead_Defaults(t *testing.T) {
	content := "single line"
	r := Head(content, 0, 0) // should use defaults
	if r.Truncated {
		t.Error("single line should not be truncated with defaults")
	}
}

// ── Tail truncation ──────────────────────────────────────────────────

func TestTail_NoTruncation(t *testing.T) {
	content := "line1\nline2\nline3"
	r := Tail(content, 10, 1024)
	if r.Truncated {
		t.Error("should not truncate small content")
	}
	if r.Content != content {
		t.Error("content should be unchanged")
	}
}

func TestTail_KeepsEnd(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	lines[90] = "TARGET_LINE_90"
	lines[99] = "TARGET_LINE_99"
	content := strings.Join(lines, "\n")

	r := Tail(content, 10, 1024)
	if !strings.Contains(r.Content, "TARGET_LINE_90") {
		t.Error("should keep line 90")
	}
	if !strings.Contains(r.Content, "TARGET_LINE_99") {
		t.Error("should keep line 99")
	}
	if strings.Contains(r.Content, "line0") {
		// First 90 lines of just "line" — some may be there, but the early ones shouldn't
		t.Log("Note: early lines may be present if they fit")
	}
}

func TestTail_PartialFirstLine(t *testing.T) {
	// Single huge line
	longLine := strings.Repeat("z", 60000)
	content := longLine

	r := Tail(content, 2000, 1000)
	if !r.Truncated {
		t.Error("should truncate")
	}
	if len(r.Content) == 0 {
		t.Error("should have partial content")
	}
	// Should be the tail of the line
	if !strings.HasSuffix(r.Content, "zzzz") {
		t.Errorf("partial should be from end, got: ...%s", r.Content[len(r.Content)-20:])
	}
}

// ── MiddleOut ────────────────────────────────────────────────────────

func TestMiddleOut_NoTruncation(t *testing.T) {
	content := "short"
	r := MiddleOut(content, 100)
	if r.Truncated {
		t.Error("should not truncate short content")
	}
}

func TestMiddleOut_Truncates(t *testing.T) {
	content := strings.Repeat("abcdefghij", 10000) // 100KB
	r := MiddleOut(content, 1000)
	if !r.Truncated {
		t.Error("should truncate large content")
	}
	if !strings.Contains(r.Content, "lines truncated") {
		t.Error("should contain truncation notice")
	}
	if !strings.HasPrefix(r.Content, "abcdefghij") {
		t.Error("should start with beginning of content")
	}
}

// ── Line truncation ──────────────────────────────────────────────────

func TestLine_NoTruncation(t *testing.T) {
	result := Line("short line", 100)
	if result != "short line" {
		t.Errorf("short line should not be truncated, got %q", result)
	}
}

func TestLine_Truncation(t *testing.T) {
	longLine := strings.Repeat("x", 5000)
	result := Line(longLine, 100)
	if !strings.HasSuffix(result, "... [truncated]") {
		t.Error("should have truncation suffix")
	}
	// Should be approximately 100 chars + suffix
	if len(result) > 200 {
		t.Errorf("result too long: %d chars", len(result))
	}
}

func TestLine_MultiByteChars(t *testing.T) {
	// Japanese characters — 3 bytes each in UTF-8
	japanese := strings.Repeat("あ", 500) // 1500 bytes, 500 runes
	result := Line(japanese, 100)
	if !strings.HasSuffix(result, "... [truncated]") {
		t.Error("should have truncation suffix")
	}
	// Should not break mid-character
	runes := []rune(strings.TrimSuffix(result, "... [truncated]"))
	if len(runes) != 100 {
		t.Errorf("expected 100 runes, got %d", len(runes))
	}
}

// ── FormatFileContinuation ───────────────────────────────────────────

func TestFormatFileContinuation_NoTruncation(t *testing.T) {
	r := Result{Truncated: false, OutputLines: 10, TotalLines: 10}
	msg := FormatFileContinuation(r, 1, 0, 10)
	if msg != "" {
		t.Errorf("no message expected without truncation, got %q", msg)
	}
}

func TestFormatFileContinuation_TruncatedByLines(t *testing.T) {
	r := Result{
		Truncated:   true,
		TruncatedBy: "lines",
		OutputLines: 42,
		TotalLines:  380,
	}
	msg := FormatFileContinuation(r, 1, 0, 380)
	if !strings.Contains(msg, "offset=43") {
		t.Errorf("should suggest next offset, got %q", msg)
	}
	if !strings.Contains(msg, "of 380") {
		t.Errorf("should show total lines, got %q", msg)
	}
}

func TestFormatFileContinuation_TruncatedByBytes(t *testing.T) {
	r := Result{
		Truncated:   true,
		TruncatedBy: "bytes",
		OutputLines: 30,
		TotalLines:  100,
	}
	msg := FormatFileContinuation(r, 1, 0, 100)
	if !strings.Contains(msg, "50.0KB limit") {
		t.Errorf("should mention byte limit, got %q", msg)
	}
}

func TestFormatFileContinuation_FirstLineExceeds(t *testing.T) {
	r := Result{
		Truncated:            true,
		FirstLineExceedsLimit: true,
		TotalBytes:           60000,
	}
	msg := FormatFileContinuation(r, 5, 0, 100)
	if !strings.Contains(msg, "sed") {
		t.Errorf("should suggest bash fallback, got %q", msg)
	}
}

func TestFormatFileContinuation_UserLimit(t *testing.T) {
	r := Result{
		Truncated:   false,
		OutputLines: 50,
		TotalLines:  200,
	}
	msg := FormatFileContinuation(r, 1, 50, 200)
	if !strings.Contains(msg, "150 more lines") {
		t.Errorf("should show remaining lines, got %q", msg)
	}
}

// ── FormatBashTruncation ─────────────────────────────────────────────

func TestFormatBashTruncation(t *testing.T) {
	r := Result{Truncated: true, TotalLines: 100, OutputLines: 20}
	msg := FormatBashTruncation(r)
	if !strings.Contains(msg, "80 lines truncated") {
		t.Errorf("should show removed count, got %q", msg)
	}
	if !strings.Contains(msg, "last 20") {
		t.Errorf("should show kept count, got %q", msg)
	}
}

// ── FormatSize ───────────────────────────────────────────────────────

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int
		want  string
	}{
		{100, "100B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
	}
	for _, tt := range tests {
		got := FormatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

// ── splitLines ───────────────────────────────────────────────────────

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		count int
	}{
		{"", 0},
		{"one line", 1},
		{"line1\nline2", 2},
		{"line1\nline2\n", 2}, // trailing newline should not add empty element
		{"a\nb\nc\nd", 4},
	}
	for _, tt := range tests {
		lines := splitLines(tt.input)
		if len(lines) != tt.count {
			t.Errorf("splitLines(%q): got %d lines, want %d", tt.input, len(lines), tt.count)
		}
	}
}
