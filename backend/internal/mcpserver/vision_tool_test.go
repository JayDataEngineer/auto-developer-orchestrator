package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestDescribeImageSuccessPath verifies the happy path: backbone script
// returns JSON, tool surfaces {description, model, success:true}.
func TestDescribeImageSuccessPath(t *testing.T) {
	fake := &fakeSandboxExec{
		out: `{"description": "a cat sitting on a windowsill", "model": "Qwen3.5-2B-ONNX-OPT"}` + "\n",
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	result, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/cat.png",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result not a map: %T", result)
	}
	if m["success"] != true {
		t.Errorf("success = %v, want true (full result: %v)", m["success"], m)
	}
	if m["description"] != "a cat sitting on a windowsill" {
		t.Errorf("description wrong: %q", m["description"])
	}
	if m["model"] != "Qwen3.5-2B-ONNX-OPT" {
		t.Errorf("model wrong: %q", m["model"])
	}

	// Verify command shape: must invoke describe_image.py with --image.
	if !strings.Contains(fake.lastCmd, "describe_image.py") {
		t.Errorf("command missing describe_image.py: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "--image") {
		t.Errorf("command missing --image flag: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "/sandbox/workspace/cat.png") {
		t.Errorf("image path not in command: %q", fake.lastCmd)
	}
}

// TestDescribeImageModelMissingNotError is the load-bearing test: a missing
// model MUST return success:false, NOT a Go-level error. This is so an
// agent loop that doesn't care about vision doesn't crash on the missing
// dep. The Go-side error path is reserved for genuine failures.
func TestDescribeImageModelMissingNotError(t *testing.T) {
	fake := &fakeSandboxExec{
		out: "model not found at /sandbox/workspace/.pux/models/Qwen3.5-2B-ONNX-OPT; run scripts/bootstrap-vision.sh\n",
		err: errors.New("exit status 2"),
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	result, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/cat.png",
	})
	if err != nil {
		t.Fatalf("model-missing must NOT be a Go error (got %v); it's an expected state", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result not a map: %T", result)
	}
	if m["success"] != false {
		t.Errorf("success = %v, want false on model-missing", m["success"])
	}
	if m["reason"] != "unavailable" {
		t.Errorf("reason = %v, want 'unavailable'", m["reason"])
	}
	explanation, _ := m["explanation"].(string)
	if !strings.Contains(explanation, "bootstrap-vision.sh") {
		t.Errorf("explanation should mention bootstrap-vision.sh: %q", explanation)
	}
}

// TestDescribeImageDepsMissing covers exit code 3 — sandbox image is
// missing onnxruntime-genai. Same "not an error" semantics as model-missing
// because it's a fixable configuration state.
func TestDescribeImageDepsMissing(t *testing.T) {
	fake := &fakeSandboxExec{
		out: "onnxruntime-genai not installed\n",
		err: errors.New("exit status 3"),
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	result, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/cat.png",
	})
	if err != nil {
		t.Fatalf("deps-missing must NOT be a Go error (got %v)", err)
	}
	m := result.(map[string]any)
	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if m["reason"] != "deps_missing" {
		t.Errorf("reason = %v, want 'deps_missing'", m["reason"])
	}
}

// TestDescribeImageInferenceFailure covers exit code 1 — the model ran
// but inference itself failed. This is NOT model-missing — it's a real
// operational failure that should surface as a structured false result.
func TestDescribeImageInferenceFailure(t *testing.T) {
	fake := &fakeSandboxExec{
		out: "inference failed: CUDA error: out of memory\n",
		err: errors.New("exit status 1"),
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	result, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/cat.png",
	})
	if err != nil {
		t.Fatalf("inference failure should be a structured result, not Go error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if m["reason"] != "inference_failed" {
		t.Errorf("reason = %v, want 'inference_failed'", m["reason"])
	}
	if !strings.Contains(m["error"].(string), "out of memory") {
		t.Errorf("error message wrong: %v", m["error"])
	}
}

// TestDescribeImageTimeout verifies the timeout path returns a structured
// result, not a Go error (same "expected state" rationale).
func TestDescribeImageTimeout(t *testing.T) {
	fake := &fakeSandboxExec{
		out:   "",
		delay: 5 * time.Second,
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})
	tool.timeout = 100 * time.Millisecond

	result, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/cat.png",
	})
	if err != nil {
		t.Fatalf("timeout should NOT be a Go error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != false {
		t.Errorf("success = %v, want false on timeout", m["success"])
	}
	if m["reason"] != "timeout" {
		t.Errorf("reason = %v, want 'timeout'", m["reason"])
	}
}

// TestDescribeImageRequiresSource verifies the arg-validation contract:
// at least one of image_path or image_url is mandatory.
func TestDescribeImageRequiresSource(t *testing.T) {
	fake := &fakeSandboxExec{}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatalf("expected Go error when no image source provided")
	}
}

// TestDescribeImageRejectsBothSources verifies mutual exclusivity of
// image_path and image_url — passing both is a usage error.
func TestDescribeImageRejectsBothSources(t *testing.T) {
	fake := &fakeSandboxExec{}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/cat.png",
		"image_url":  "https://example.com/cat.png",
	})
	if err == nil {
		t.Fatalf("expected Go error when both image sources provided")
	}
	if fake.lastCmd != "" {
		t.Errorf("executor should not have been called, cmd=%q", fake.lastCmd)
	}
}

// TestDescribeImageUsesURLPath verifies the --image-url flag is wired
// (mutually exclusive with --image).
func TestDescribeImageUsesURLPath(t *testing.T) {
	fake := &fakeSandboxExec{
		out: `{"description": "remote image", "model": "Qwen3.5-2B-ONNX-OPT"}` + "\n",
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"image_url": "https://example.com/cat.png",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "--image-url") {
		t.Errorf("command missing --image-url: %q", fake.lastCmd)
	}
	if strings.Contains(fake.lastCmd, "--image /") {
		t.Errorf("--image (file mode) should NOT appear when --image-url used: %q", fake.lastCmd)
	}
}

// TestDescribeImageCustomPrompt verifies the prompt arg flows through.
func TestDescribeImageCustomPrompt(t *testing.T) {
	fake := &fakeSandboxExec{
		out: `{"description": "stop sign", "model": "Qwen3.5-2B-ONNX-OPT"}` + "\n",
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/sign.png",
		"prompt":     "What text is on the sign?",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "--prompt") {
		t.Errorf("command missing --prompt: %q", fake.lastCmd)
	}
}

// TestDescribeImageShellEscaping verifies the shQ escape handles single
// quotes in image paths (e.g. a file named "it's a test.png").
func TestDescribeImageShellEscaping(t *testing.T) {
	fake := &fakeSandboxExec{
		out: `{"description": "x", "model": "m"}` + "\n",
	}
	tool := NewDescribeImageTool(fake, VisionToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{
		"image_path": "/sandbox/workspace/it's a test.png",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	// POSIX single-quote escape: ' → '\'' (close, backslash-quote, open)
	if !strings.Contains(fake.lastCmd, `'\''`) {
		t.Errorf("single quote not escaped with POSIX idiom: %q", fake.lastCmd)
	}
}

// TestInferExitCode verifies the exit-code parser recognizes all the
// markers we care about.
func TestInferExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"exit_status_2", errors.New("exit status 2"), 2},
		{"exit_status_3", errors.New("exit status 3"), 3},
		{"exit_status_1", errors.New("exit status 1"), 1},
		{"exit_code_2", errors.New("exit code 2"), 2},
		{"embedded_in_message", errors.New("command failed: exit status 2: see logs"), 2},
		// Real format produced by sandbox.Manager.execInContainer line 129,
		// then re-wrapped by ExecInSandbox. This is what production callers
		// actually see — must parse correctly.
		{"docker_exited_with_code", errors.New("exec exited with code 2: model not found"), 2},
		{"docker_wrapped", errors.New("exec failed in sandbox mcp-default: exec exited with code 2: model not found"), 2},
		{"docker_exited_code_3", errors.New("exec failed in sandbox x: exec exited with code 3: genai missing"), 3},
		{"no_marker", errors.New("connection refused"), -1},
		{"empty_error", errors.New(""), -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inferExitCode(tc.err)
			if got != tc.want {
				t.Errorf("inferExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestTailOutput verifies the output-trim helper.
func TestTailOutput(t *testing.T) {
	if got := tailOutput("short", 800); got != "short" {
		t.Errorf("short string should pass through: %q", got)
	}
	long := strings.Repeat("a", 1000)
	got := tailOutput(long, 100)
	if !strings.HasPrefix(got, "...") {
		t.Errorf("long string should start with '...': %q", got[:10])
	}
	if len(got) != 103 { // "..." + 100 bytes
		t.Errorf("trimmed length = %d, want 103", len(got))
	}
}
