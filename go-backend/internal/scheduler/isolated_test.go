package scheduler

import (
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/util"
)

func TestJsonStr(t *testing.T) {
	got := jsonStr("hello")
	want := `"hello"`
	if got != want {
		t.Errorf("jsonStr(%q) = %q, want %q", "hello", got, want)
	}

	got = jsonStr(`say "hi"`)
	if got != `"say \"hi\""` {
		t.Errorf("jsonStr with quotes = %q", got)
	}

	got = jsonStr("")
	if got != `""` {
		t.Errorf("jsonStr empty = %q", got)
	}
}

func TestShellEscape(t *testing.T) {
	// Note: sandbox.ShellEscape only replaces single quotes with '\'',
	// the caller wraps in single quotes separately.
	tests := []struct {
		input, want string
	}{
		{"simple", "simple"},
		{"it's", "it'\\''s"},
		{"", ""},
	}
	for _, tt := range tests {
		got := sandbox.ShellEscape(tt.input)
		if got != tt.want {
			t.Errorf("ShellEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestModelFlag(t *testing.T) {
	if got := modelFlag(""); got != "" {
		t.Errorf("modelFlag empty = %q, want empty", got)
	}
	if got := modelFlag("gpt-4"); got != "--model litellm/gpt-4" {
		t.Errorf("modelFlag gpt-4 = %q", got)
	}
}

func TestTruncateEllipsis(t *testing.T) {
	if got := util.TruncateEllipsis("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q", got)
	}
	if got := util.TruncateEllipsis("hello world", 5); got != "hello..." {
		t.Errorf("truncate long = %q", got)
	}
	if got := util.TruncateEllipsis("", 5); got != "" {
		t.Errorf("truncate empty = %q", got)
	}
}
