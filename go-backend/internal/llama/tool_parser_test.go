package llama

import (
	"testing"
)

func TestParseToolCalls(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantCalls    int
		wantText     string
		wantToolName string
		wantArgsKey  string
	}{
		{
			name:      "no tool calls",
			input:     "Hello, I'll help you with that.",
			wantCalls: 0,
			wantText:  "Hello, I'll help you with that.",
		},
		{
			name:         "simple tool call",
			input:        `I'll read that file.<|tool_call>read_file{"path": "/tmp/test.go"}<|tool_call|>`,
			wantCalls:    1,
			wantText:     "I'll read that file.",
			wantToolName: "read_file",
			wantArgsKey:  "path",
		},
		{
			name:         "tool call with call: prefix",
			input:        `<|tool_call>call:bash{"command": "ls -la"}<|tool_call|>`,
			wantCalls:    1,
			wantToolName: "bash",
			wantArgsKey:  "command",
		},
		{
			name: "multiple tool calls",
			input: `<|tool_call>read_file{"path": "/a.go"}<|tool_call|>` +
				` and then ` +
				`<|tool_call>bash{"command": "go build"}<|tool_call|>`,
			wantCalls: 2,
			wantText:  " and then ",
		},
		{
			name:      "text before and after",
			input:     "Let me check." + `<|tool_call>bash{"command": "pwd"}<|tool_call|>` + "Here's the result.",
			wantCalls: 1,
			wantText:  "Let me check.Here's the result.",
		},
		{
			name:         "tool call with complex args",
			input:        `<|tool_call>computer_use_click{"element_id": 42, "button": "left"}<|tool_call|>`,
			wantCalls:    1,
			wantToolName: "computer_use_click",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, cleanText := ParseToolCalls(tt.input)

			if len(calls) != tt.wantCalls {
				t.Errorf("ParseToolCalls() calls = %d, want %d", len(calls), tt.wantCalls)
			}

			if tt.wantText != "" && cleanText != tt.wantText {
				t.Errorf("ParseToolCalls() text = %q, want %q", cleanText, tt.wantText)
			}

			if tt.wantCalls > 0 && tt.wantToolName != "" {
				if calls[0].Name != tt.wantToolName {
					t.Errorf("ParseToolCalls() tool name = %q, want %q", calls[0].Name, tt.wantToolName)
				}
				if tt.wantArgsKey != "" {
					if _, ok := calls[0].Args[tt.wantArgsKey]; !ok {
						t.Errorf("ParseToolCalls() missing args key %q in %+v", tt.wantArgsKey, calls[0].Args)
					}
				}
			}
		})
	}
}

func TestHasPartialToolCall(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"no tags", "hello world", false},
		{"complete tool call", "<|tool_call>bash{cmd}<|tool_call|>", false},
		{"partial - open only", "<|tool_call>bash{", true},
		{"partial - multiple open", "<|tool_call>a{}<|tool_call|><|tool_call>b{", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasPartialToolCall(tt.input)
			if got != tt.want {
				t.Errorf("HasPartialToolCall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStripToolCallTags(t *testing.T) {
	input := "before" + `<|tool_call>` + `bash{"command": "ls"}` + `<|tool_call|>` + "after"
	got := StripToolCallTags(input)
	if contains(got, "<|tool_call>") || contains(got, "<|tool_call|>") {
		t.Errorf("StripToolCallTags() still contains tags: %q", got)
	}
}

func TestBuildFullPrompt(t *testing.T) {
	system := "You are helpful."
	turns := []Turn{
		{Role: "user", Content: "Read main.go"},
		{Role: "model", Content: "Here's the file."},
		{Role: "user", Content: "Add error handling"},
	}

	result := BuildFullPrompt(system, turns)

	// Check structure
	if !contains(result, "<start_of_turn>system\n") {
		t.Error("Missing system turn start")
	}
	if !contains(result, "<start_of_turn>user\n") {
		t.Error("Missing user turn start")
	}
	if !contains(result, "<start_of_turn>model\n") {
		t.Error("Missing model turn start")
	}
	if !contains(result, "<end_of_turn>\n") {
		t.Error("Missing turn end tags")
	}
	// Should end with model turn (waiting for model to generate)
	if !endsWith(result, "<start_of_turn>model\n") {
		t.Error("Prompt should end with model turn start")
	}
}

func TestChatFormatFunctions(t *testing.T) {
	// formatSystemPrompt
	sys := formatSystemPrompt("test system")
	if !contains(sys, "<start_of_turn>system\ntest system<end_of_turn>\n") {
		t.Errorf("formatSystemPrompt() = %q", sys)
	}

	// formatUserTurn
	usr := formatUserTurn("hello")
	if !contains(usr, "<start_of_turn>user\nhello<end_of_turn>\n<start_of_turn>model\n") {
		t.Errorf("formatUserTurn() = %q", usr)
	}

	// formatUserTurnWithResult
	res := formatUserTurnWithResult("tool output", "what's next?")
	if !contains(res, "tool output") || !contains(res, "what's next?") {
		t.Errorf("formatUserTurnWithResult() = %q", res)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
