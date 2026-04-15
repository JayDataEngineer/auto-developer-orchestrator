// agent-loop-demo: Prove llama-go library-mode KV cache persistence.
//
// Uses raw Generate() with manual Gemma 4 chat formatting to bypass
// the complex Jinja2 chat template that llama-go can't parse.
//
// The key test: we use the SAME Context for all 4 turns.
// If KV cache persists, turns 2-4 only process the NEW tokens.
// If not, they re-process everything (same as HTTP mode).
//
// Build:
//   export LIBRARY_PATH=/home/ubuntu/Documents/programs/llama-go
//   export C_INCLUDE_PATH=$LIBRARY_PATH
//   export LD_LIBRARY_PATH=$LIBRARY_PATH
//   cd go-backend && go build -o /tmp/agent-loop-demo ./cmd/agent-loop-demo/
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	llama "github.com/tcpipuk/llama-go"
)

func main() {
	modelPath := flag.String("model", "", "Path to GGUF model file")
	ctxSize := flag.Int("ctx", 8192, "Context size")
	maxTokens := flag.Int("max-tokens", 150, "Max tokens per generation")
	flag.Parse()

	if *modelPath == "" {
		*modelPath = "/home/ubuntu/Documents/programs/shared-docker-infra/models/llm/gemma-4-26B-A4B-it-UD-IQ4_NL.gguf"
	}

	fmt.Printf("Loading model: %s\n", *modelPath)
	fmt.Printf("Context size:  %d\n", *ctxSize)

	t0 := time.Now()
	model, err := llama.LoadModel(*modelPath, llama.WithGPULayers(-1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load model: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()
	fmt.Printf("Model loaded in %s\n", time.Since(t0).Round(time.Millisecond))

	// Create ONE persistent context — the KV cache lives here
	ctx, err := model.NewContext(
		llama.WithContext(*ctxSize),
		llama.WithPrefixCaching(true),
		llama.WithBatch(512),
		llama.WithF16Memory(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create context: %v\n", err)
		os.Exit(1)
	}
	defer ctx.Close()

	fmt.Println("\n=== Agent Loop Demo (Library Mode — Raw Generate) ===\n")

	// ---- APPROACH A: Naive (full prompt each turn, like HTTP) ----
	// This is what the current HTTP architecture does.
	fmt.Println("--- Approach A: Naive (full prompt re-sent each turn) ---")
	runNaiveBenchmark(ctx, *maxTokens)

	// Create a fresh context for approach B
	ctx.Close()
	ctx, err = model.NewContext(
		llama.WithContext(*ctxSize),
		llama.WithPrefixCaching(true),
		llama.WithBatch(512),
		llama.WithF16Memory(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to recreate context: %v\n", err)
		os.Exit(1)
	}
	defer ctx.Close()

	// ---- APPROACH B: Incremental (append only new tokens) ----
	// This is what library mode enables — the KV cache persists.
	fmt.Println("\n--- Approach B: Incremental (append only new tokens, KV cache persists) ---")
	runIncrementalBenchmark(ctx, *maxTokens)
}

// buildGemma4Prompt manually formats messages for Gemma 4.
// Format: <start_of_turn>role\ncontent<end_of_turn>\n<start_of_turn>model\n
func buildGemma4Prompt(system string, turns [][2]string) string {
	var sb strings.Builder
	if system != "" {
		sb.WriteString("<start_of_turn>system\n")
		sb.WriteString(system)
		sb.WriteString("<end_of_turn>\n")
	}
	for _, turn := range turns {
		sb.WriteString("<start_of_turn>user\n")
		sb.WriteString(turn[0])
		sb.WriteString("<end_of_turn>\n")
		if turn[1] != "" {
			sb.WriteString("<start_of_turn>model\n")
			sb.WriteString(turn[1])
			sb.WriteString("<end_of_turn>\n")
		}
	}
	// Start model's turn
	sb.WriteString("<start_of_turn>model\n")
	return sb.String()
}

func runNaiveBenchmark(ctx *llama.Context, maxTokens int) {
	// Each turn rebuilds the full prompt from scratch (like HTTP does)
	system := "You are a coding assistant with access to tools. Read files, run commands, and help with coding tasks."

	turns1 := [][2]string{
		{"Read the file /sandbox/workspace/main.go and tell me what it does.", ""},
	}
	prompt1 := buildGemma4Prompt(system, turns1)
	t1, out1, tok1 := generate(ctx, prompt1, maxTokens, "Turn 1")

	// Turn 2: include turn 1 in history + tool result
	turns2 := [][2]string{
		{"Read the file /sandbox/workspace/main.go and tell me what it does.", out1},
		{"Tool result for read_file:\n```go\npackage main\n\nimport (\n    \"fmt\"\n    \"net/http\"\n)\n\nfunc main() {\n    http.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) {\n        fmt.Fprintf(w, \"Hello, World!\")\n    })\n    http.ListenAndServe(\":8080\", nil)\n}\n```\nWhat does this code do?", ""},
	}
	prompt2 := buildGemma4Prompt(system, turns2)
	t2, _, tok2 := generate(ctx, prompt2, maxTokens, "Turn 2")

	turns3 := [][2]string{
		{"Read the file /sandbox/workspace/main.go and tell me what it does.", out1},
		{"Tool result:\n```go\npackage main\n```\nWhat does this code do?", "This is a basic Go HTTP server."},
		{"Now add error handling and a /health endpoint.", ""},
	}
	prompt3 := buildGemma4Prompt(system, turns3)
	t3, _, tok3 := generate(ctx, prompt3, maxTokens, "Turn 3")

	total := t1 + t2 + t3
	totalTok := tok1 + tok2 + tok3
	fmt.Printf("  TOTAL: %dms (%d tokens, %.1f tok/s)\n", total.Milliseconds(), totalTok, float64(totalTok)/total.Seconds())
}

func runIncrementalBenchmark(ctx *llama.Context, maxTokens int) {
	// Build the prompt incrementally.
	// Turn 1: system + first user message
	// Turn 2: append tool result to SAME context (no re-processing of turn 1)
	// Turn 3: append next user message to SAME context

	system := "You are a coding assistant with access to tools. Read files, run commands, and help with coding tasks."

	// Turn 1: Full initial prompt
	prompt1 := "<start_of_turn>system\n" + system + "<end_of_turn>\n" +
		"<start_of_turn>user\nRead the file /sandbox/workspace/main.go and tell me what it does.<end_of_turn>\n" +
		"<start_of_turn>model\n"
	t1, out1, tok1 := generate(ctx, prompt1, maxTokens, "Turn 1")

	// Turn 2: Append tool result + next user message to SAME context
	// The KV cache from turn 1 is still warm!
	prompt2 := out1 + "<end_of_turn>\n" +
		"<start_of_turn>user\nTool result for read_file:\n```go\npackage main\n\nimport (\n    \"fmt\"\n    \"net/http\"\n)\n\nfunc main() {\n    http.HandleFunc(\"/\", func(w http.ResponseWriter, r *http.Request) {\n        fmt.Fprintf(w, \"Hello, World!\")\n    })\n    http.ListenAndServe(\":8080\", nil)\n}\n```\nWhat does this code do?<end_of_turn>\n" +
		"<start_of_turn>model\n"
	t2, _, tok2 := generate(ctx, prompt2, maxTokens, "Turn 2")

	// Turn 3: Append next user message
	prompt3 := "<end_of_turn>\n" +
		"<start_of_turn>user\nNow add error handling and a /health endpoint.<end_of_turn>\n" +
		"<start_of_turn>model\n"
	t3, _, tok3 := generate(ctx, prompt3, maxTokens, "Turn 3")

	total := t1 + t2 + t3
	totalTok := tok1 + tok2 + tok3
	fmt.Printf("  TOTAL: %dms (%d tokens, %.1f tok/s)\n", total.Milliseconds(), totalTok, float64(totalTok)/total.Seconds())
}

func generate(ctx *llama.Context, prompt string, maxTokens int, label string) (time.Duration, string, int) {
	t0 := time.Now()

	var output strings.Builder
	tokenCount := 0

	err := ctx.GenerateStream(prompt, func(token string) bool {
		output.WriteString(token)
		tokenCount++
		return true // continue generating
	},
		llama.WithMaxTokens(maxTokens),
		llama.WithTemperature(0.7),
		llama.WithTopP(0.95),
		llama.WithTopK(64),
	)

	elapsed := time.Since(t0)

	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s error: %v\n", label, err)
		return elapsed, "", 0
	}

	text := output.String()
	tokPerSec := float64(tokenCount) / elapsed.Seconds()
	fmt.Printf("  %s: %dms | %d tokens | %.1f tok/s | %s\n",
		label, elapsed.Milliseconds(), tokenCount, tokPerSec, truncate(text, 80))

	return elapsed, text, tokenCount
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
