package util

import "testing"

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello"},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcdef", 6, "abcdef"},
		{"abcdef", 3, "abc"},
	}

	for _, tt := range tests {
		got := Truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestTruncateEllipsis(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc..."},
	}

	for _, tt := range tests {
		got := TruncateEllipsis(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("TruncateEllipsis(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
