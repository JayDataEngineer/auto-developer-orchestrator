// vision_tool.go exposes describe_image as an MCP tool. The tool shells out
// to /usr/local/bin/describe_image.py (a backbone script shipped with the
// sandbox image), which loads Qwen3.5-2B-ONNX-OPT fp16 and runs vision
// inference locally — no external MCP dependency.
//
// The model is OPTIONAL. Operators run scripts/bootstrap-vision.sh from the
// host to download weights to <project>/.pux/models/. When the model isn't
// present, the Python script exits 2 with a friendly message; this tool
// translates that into a clean MCP tool result (isError=false, body explains
// how to enable) so the model can fall back to text-only reasoning.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// VisionToolConfig tunes the describe_image tool. Zero-value defaults are
// sensible; operators don't need to set this unless they want to override
// the model directory or timeout.
type VisionToolConfig struct {
	// ModelDir overrides where describe_image.py looks for the ONNX model.
	// Empty (default) → script's built-in path
	// (/sandbox/workspace/.pux/models/Qwen3.5-2B-ONNX-OPT/).
	ModelDir string
	// Timeout caps the underlying inference call. Default 120s — vision
	// inference is slower than bash; a stuck model shouldn't hang the
	// server forever.
	Timeout time.Duration
}

// DescribeImageTool runs vision inference inside the sandbox via
// describe_image.py. Returns a description of the image, or a friendly
// "vision unavailable" message when the model isn't downloaded.
type DescribeImageTool struct {
	exec    SandboxExecutor
	cfg     VisionToolConfig
	timeout time.Duration
}

// NewDescribeImageTool wires the tool to a sandbox executor. cfg may be
// zero-valued for defaults.
func NewDescribeImageTool(exec SandboxExecutor, cfg VisionToolConfig) *DescribeImageTool {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &DescribeImageTool{exec: exec, cfg: cfg, timeout: timeout}
}

func (t *DescribeImageTool) Name() string { return "describe_image" }

func (t *DescribeImageTool) Description() string {
	return "Describe an image using local vision inference (Qwen3.5-2B-ONNX-OPT). " +
		"Use when the driving LLM can't see the image directly, or when you want " +
		"a fast local description without an external round-trip. " +
		"Pass either an in-sandbox image path OR a URL (the script fetches it). " +
		"Optional --prompt customizes the question; default is generic description. " +
		"Vision is OPTIONAL — if the model isn't downloaded, returns a friendly " +
		"'run scripts/bootstrap-vision.sh' message instead of an error."
}

func (t *DescribeImageTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"image_path": {
				"type": "string",
				"description": "Absolute path to image file inside the sandbox (e.g. /sandbox/workspace/foo.png)"
			},
			"image_url": {
				"type": "string",
				"description": "URL of image to download and describe. Mutually exclusive with image_path."
			},
			"prompt": {
				"type": "string",
				"description": "Optional instruction for the model (default: 'describe this image concisely'). Useful for asking specific questions like 'what text is on the sign?'"
			}
		},
		"oneOf": [
			{"required": ["image_path"]},
			{"required": ["image_url"]}
		]
	}`)
}

// Execute dispatches the inference call. Three result paths:
//
//   - Success → {description, model, success:true}
//   - Model missing → {success:false, reason:"unavailable", ...bootstrap hint}
//     (NOT isError — this is an expected state, not a failure)
//   - Inference error → returns Go error → MCP wraps as isError=true
//
// The split between "model missing" and "inference error" is load-bearing:
// the model is genuinely optional, and a model-missing state shouldn't
// break agent loops that don't need vision at all. Real failures (corrupt
// image, ONNX runtime crash, OOM) surface as isError.
func (t *DescribeImageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	imagePath, _ := args["image_path"].(string)
	imageURL, _ := args["image_url"].(string)
	prompt, _ := args["prompt"].(string)

	if imagePath == "" && imageURL == "" {
		return nil, core.NewToolError("describe_image",
			"one of image_path or image_url is required")
	}
	if imagePath != "" && imageURL != "" {
		return nil, core.NewToolError("describe_image",
			"image_path and image_url are mutually exclusive")
	}

	// Build the python invocation. describe_image.py takes --image XOR --image-url.
	// Use shQ to safely quote values — image paths and URLs may contain quotes/spaces.
	parts := []string{"python3 /usr/local/bin/describe_image.py"}
	if imagePath != "" {
		parts = append(parts, "--image", shQ(imagePath))
	} else {
		parts = append(parts, "--image-url", shQ(imageURL))
	}
	if prompt != "" {
		parts = append(parts, "--prompt", shQ(prompt))
	}
	if t.cfg.ModelDir != "" {
		parts = append(parts, "--model-dir", shQ(t.cfg.ModelDir))
	}
	cmd := strings.Join(parts, " ")

	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	out, err := t.exec.Exec(execCtx, cmd)
	if execCtx.Err() == context.DeadlineExceeded {
		return map[string]any{
			"success": false,
			"reason":  "timeout",
			"error":   fmt.Sprintf("describe_image timed out after %v", t.timeout),
		}, nil
	}

	// Exit-code dispatch. describe_image.py contract:
	//   0 → success (JSON on stdout)
	//   1 → inference error (message on stderr)
	//   2 → model missing (friendly bootstrap hint on stderr)
	//   3 → onnxruntime-genai not installed (sandbox-image rebuild needed)
	if err != nil {
		stderrTail := tailOutput(out, 800)
		switch inferExitCode(err) {
		case 2:
			return map[string]any{
				"success":     false,
				"reason":      "unavailable",
				"explanation": "Vision model is not downloaded. Run scripts/bootstrap-vision.sh from the host to enable. Until then, image-aware reasoning falls back to whatever the driving LLM provides natively.",
				"detail":      stderrTail,
			}, nil
		case 3:
			return map[string]any{
				"success":     false,
				"reason":      "deps_missing",
				"explanation": "Sandbox image is missing onnxruntime-genai. Rebuild with `task build` after pulling latest sandbox/Dockerfile.",
				"detail":      stderrTail,
			}, nil
		}
		// Exit code 1 (or anything else) — real inference failure. Surface the
		// error message as the result so the model can react.
		return map[string]any{
			"success": false,
			"reason":  "inference_failed",
			"error":   stderrTail,
		}, nil
	}

	// Success. Parse the JSON envelope from stdout.
	var parsed struct {
		Description string `json:"description"`
		Model       string `json:"model"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		// Malformed JSON from the script is a real bug, not a model error.
		return nil, fmt.Errorf("describe_image: malformed JSON output: %w (raw=%q)", err, tailOutput(out, 400))
	}
	return map[string]any{
		"success":     true,
		"description": parsed.Description,
		"model":       parsed.Model,
	}, nil
}

// inferExitCode extracts the exit code from the bash-exec error. The repo's
// sandbox.Manager.execInContainer formats non-zero exits as
// "exec exited with code N: ...", which ExecInSandbox further wraps as
// "exec failed in sandbox X: ...". Go's stdlib *exec.ExitError uses the
// "exit status N" form. We match all three patterns rather than depending
// on the sandbox package's error type — keeps this package decoupled.
//
// Falls back to -1 (unknown) for non-exit errors. Anything unknown is
// treated as a generic inference failure by the caller.
func inferExitCode(err error) int {
	if err == nil {
		return 0
	}
	s := err.Error()
	// Markers from worst-specificity to best. Order matters only for the
	// "exit code " vs "exited with code " overlap — Cut returns the first
	// occurrence, and "exited with code 2" contains "code 2" so the more
	// specific marker must be tried first to avoid confusion. (In practice
	// both yield the same number, but explicit ordering documents intent.)
	for _, marker := range []string{"exit status ", "exited with code ", "exit code "} {
		_, rest, ok := strings.Cut(s, marker)
		if !ok {
			continue
		}
		// Take leading digits; ignore trailing punctuation.
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n == 0 {
			continue
		}
		var code int
		fmt.Sscanf(rest[:n], "%d", &code)
		return code
	}
	return -1
}

// tailOutput trims a long output blob to its last N bytes for inclusion in
// error messages. Keeps the most-recent stderr (traceback tails, etc.)
// without leaking megabytes of model output into MCP responses.
func tailOutput(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// interface assertion
var _ core.Tool = (*DescribeImageTool)(nil)
