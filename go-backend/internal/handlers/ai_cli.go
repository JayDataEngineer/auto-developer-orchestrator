package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
)

// AICLIHandler handles AI CLI integration
type AICLIHandler struct {
	logger *zap.Logger
}

// NewAICLIHandler creates a new AICLIHandler
func NewAICLIHandler(logger *zap.Logger) *AICLIHandler {
	return &AICLIHandler{logger: logger}
}

// ChatRequest represents a chat request
type ChatRequest struct {
	Provider string `json:"provider"` // openai, claude, gemini
	Message  string `json:"message"`
	Context  string `json:"context"`
}

// ChatResponse represents a chat response
type ChatResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
	Provider string `json:"provider"`
}

// Chat handles AI chat via CLI tools
func (h *AICLIHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Provider == "" || req.Message == "" {
		http.Error(w, "Provider and message are required", http.StatusBadRequest)
		return
	}

	h.logger.Info("AI chat request",
		zap.String("provider", req.Provider),
		zap.String("message", req.Message))

	// Execute CLI tool based on provider
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	response, err := h.executeCLICmd(ctx, req.Provider, req.Message)
	if err != nil {
		h.logger.Error("CLI execution failed", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ChatResponse{
			Error:    err.Error(),
			Provider: req.Provider,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		Response: response,
		Provider: req.Provider,
	})
}

// executeCLICmd executes the appropriate CLI tool
func (h *AICLIHandler) executeCLICmd(ctx context.Context, provider, message string) (string, error) {
	var cmd *exec.Cmd

	switch provider {
	case "openai":
		// Use OpenAI CLI (already authenticated)
		// openai api chat.completions.create --model gpt-4o --messages '[{"role":"user","content":"..."}]'
		cmd = exec.CommandContext(ctx, "openai", "api", "chat.completions.create",
			"--model", "gpt-4o",
			"--messages", fmt.Sprintf(`[{"role":"user","content":"%s"}]`, escapeJSON(message)))
		
	case "claude":
		// Use Claude CLI (already authenticated via Anthropic)
		// claude --model claude-3-5-sonnet --message "..."
		cmd = exec.CommandContext(ctx, "claude",
			"--model", "claude-3-5-sonnet",
			"--message", message)
		
	case "gemini":
		// Use gcloud AI or gemini CLI (already authenticated via gcloud)
		// gcloud ai platforms call ... or gemini cli
		cmd = exec.CommandContext(ctx, "gcloud", "ai", "platforms", "call",
			"--model", "gemini-pro",
			"--prompt", message)
		
	default:
		return "", fmt.Errorf("unknown provider: %s", provider)
	}

	// Set up environment
	cmd.Env = os.Environ()
	
	// Execute
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("CLI failed: %w, output: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// escapeJSON escapes special characters for JSON
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
