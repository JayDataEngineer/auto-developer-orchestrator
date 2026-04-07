package scheduler

import "testing"

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
	tests := []struct {
		input, want string
	}{
		{"simple", "'simple'"},
		{"it's", "'it'\\''s'"},
		{"", "''"},
	}
	for _, tt := range tests {
		got := shellEscape(tt.input)
		if got != tt.want {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.want)
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

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate long = %q", got)
	}
	if got := truncate("", 5); got != "" {
		t.Errorf("truncate empty = %q", got)
	}
}
