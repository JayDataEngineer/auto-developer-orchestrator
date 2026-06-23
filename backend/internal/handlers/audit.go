package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// AuditHandler exposes transcript auditing over HTTP. The actual classification
// runs in scripts/audit_lib.py via audit_transcript.py — the Go side just
// shells out and streams JSON back. This keeps the Fable/Mythos six-pattern
// classifier in pure Python (portable, testable without Go rebuilds).
//
// The agent can call this endpoint directly to self-audit, or a human can
// curl it from the TUI's session list. SSE format is one event per
// classified turn, then a final `summary` event.
type AuditHandler struct {
	scriptsDir string // typically <repo>/scripts
	logger     *zap.Logger
}

func NewAuditHandler(scriptsDir string, logger *zap.Logger) *AuditHandler {
	return &AuditHandler{scriptsDir: scriptsDir, logger: logger}
}

func (h *AuditHandler) RegisterRoutes(r chi.Router) {
	r.Get("/audit/{sessionId}", h.AuditSession)
	r.Post("/audit/{sessionId}", h.AuditSession)
}

// resolveProjectRoot finds the repo root (where .pux/ and scripts/ live).
// Falls back to cwd if PROJECT_ROOT is unset and no ancestor has scripts/.
func resolveProjectRoot() string {
	if root := os.Getenv("PROJECT_ROOT"); root != "" {
		return root
	}
	// Walk up from cwd to find scripts/audit_transcript.py
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "scripts", "audit_transcript.py")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

// AuditSession runs audit_transcript.py against .pux/sessions/<id>.jsonl
// and streams results back as SSE.
//
// Query params:
//
//	fast-only=true   Skip LLM classifier (regex-only, cheap)
//
// SSE events (text/event-stream):
//
//	event: classification
//	data: {sequence, tags, severity, evidence, classifier_note}
//
//	event: summary
//	data: {total_turns, flagged_turns, tag_counts, regressions, ...}
//
//	event: done
//	data: {}
//
//	event: error
//	data: {"error": "..."}
func (h *AuditHandler) AuditSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	if sessionID == "" {
		JSONError(w, "sessionId required", http.StatusBadRequest)
		return
	}

	projectRoot := resolveProjectRoot()
	sessionPath := filepath.Join(projectRoot, ".pux", "sessions", sessionID+".jsonl")
	scriptPath := filepath.Join(h.scriptsDir, "audit_transcript.py")

	// Fall back to <projectRoot>/scripts/ if the configured scriptsDir misses.
	if _, err := os.Stat(scriptPath); err != nil {
		fallback := filepath.Join(projectRoot, "scripts", "audit_transcript.py")
		if _, err2 := os.Stat(fallback); err2 == nil {
			scriptPath = fallback
		}
	}

	args := []string{scriptPath, "classify", sessionPath}
	if r.URL.Query().Get("fast-only") == "true" {
		args = append(args, "--fast-only")
	}

	cmd := exec.CommandContext(r.Context(), "python3", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		JSONError(w, fmt.Sprintf("stdout pipe: %v", err), http.StatusInternalServerError)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		JSONError(w, fmt.Sprintf("stderr pipe: %v", err), http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		JSONError(w, fmt.Sprintf("start audit_transcript.py: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	// Drain stderr for diagnostics on failure.
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := stderr.Read(buf)
			if err != nil || n == 0 {
				return
			}
			h.logger.Debug("audit_transcript.py stderr",
				zap.ByteString("line", buf[:n]))
		}
	}()

	// audit_transcript.py emits one JSON object (the full report). Decode it
	// and emit one SSE event per classification, then the summary, then done.
	var report struct {
		SessionID       string `json:"session_id"`
		TotalTurns      int    `json:"total_turns"`
		Classifications []struct {
			Sequence       int      `json:"sequence"`
			Tags           []string `json:"tags"`
			Severity       string   `json:"severity"`
			Evidence       string   `json:"evidence"`
			ClassifierNote string   `json:"classifier_note"`
		} `json:"classifications"`
		Summary map[string]any `json:"summary"`
	}

	if err := json.NewDecoder(stdout).Decode(&report); err != nil {
		h.logger.Error("audit_transcript.py output decode failed", zap.Error(err))
		_ = cmd.Wait()
		fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	for _, c := range report.Classifications {
		payload, _ := json.Marshal(c)
		fmt.Fprintf(w, "event: classification\ndata: %s\n\n", payload)
	}
	if flusher != nil {
		flusher.Flush()
	}

	summaryPayload, _ := json.Marshal(report.Summary)
	fmt.Fprintf(w, "event: summary\ndata: %s\n\n", summaryPayload)
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	if flusher != nil {
		flusher.Flush()
	}

	if err := cmd.Wait(); err != nil {
		h.logger.Warn("audit_transcript.py exited non-zero", zap.Error(err))
	}
}
