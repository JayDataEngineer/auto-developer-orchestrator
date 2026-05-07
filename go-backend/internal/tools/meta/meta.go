package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

// WaitTool implements core.Tool for waiting/pausing.
type WaitTool struct{}

func NewWaitTool() *WaitTool { return &WaitTool{} }

func (t *WaitTool) Name() string        { return "wait" }
func (t *WaitTool) Description() string { return "Wait/pause for a specified number of seconds" }

func (t *WaitTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"seconds": {"type": "integer", "description": "Number of seconds to wait (max 30)"}
		}
	}`)
}

func (t *WaitTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	seconds := 2
	if s, ok := args["seconds"].(float64); ok && s > 0 && s <= 30 {
		seconds = int(s)
	}
	time.Sleep(time.Duration(seconds) * time.Second)
	return map[string]any{"output": "Waited " + itoa(seconds) + " seconds"}, nil
}

// ArtifactStore is the subset of storage.Database needed for artifact persistence.
type ArtifactStore interface {
	SaveArtifact(ctx context.Context, a *storage.DBArtifact) error
}

// YieldArtifactTool writes a memo file to the sandbox and persists it to the artifact DB.
// This is the "Staff Memo" system — employees write structured outputs that other
// employees can read via file_read, and the CEO can see in the frontend.
type YieldArtifactTool struct {
	db        ArtifactStore
	sandboxDir string // base dir for memos, e.g. project path
	agentID   string
}

func NewYieldArtifactTool() *YieldArtifactTool {
	return &YieldArtifactTool{}
}

// NewYieldArtifactToolWithDB creates a yield_artifact tool that persists to both
// the filesystem and the artifact database.
func NewYieldArtifactToolWithDB(db ArtifactStore, sandboxDir, agentID string) *YieldArtifactTool {
	return &YieldArtifactTool{
		db:         db,
		sandboxDir: sandboxDir,
		agentID:    agentID,
	}
}

func (t *YieldArtifactTool) Name() string { return "yield_artifact" }
func (t *YieldArtifactTool) Description() string {
	return "Write a memo/artifact that other employees can read. Persists to file and artifact store. Use this for research reports, specs, or any structured output another employee needs."
}

func (t *YieldArtifactTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {"type": "string", "description": "Short title for the artifact (e.g. 'Research Report', 'API Spec')"},
			"type": {"type": "string", "description": "Artifact type: memo, report, spec, notes, plan", "enum": ["memo", "report", "spec", "notes", "plan"]},
			"content": {"type": "string", "description": "The full content of the artifact (markdown)"},
			"path": {"type": "string", "description": "Optional custom file path. Defaults to /sandbox/workspace/memos/<type>-<title>.md"}
		},
		"required": ["title", "type", "content"]
	}`)
}

func (t *YieldArtifactTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	title, _ := args["title"].(string)
	artifactType, _ := args["type"].(string)
	content, _ := args["content"].(string)
	customPath, _ := args["path"].(string)

	if title == "" || content == "" {
		return nil, fmt.Errorf("yield_artifact: title and content are required")
	}
	if artifactType == "" {
		artifactType = "memo"
	}

	var filePath string
	if customPath != "" {
		filePath = customPath
	} else {
		// Default: /sandbox/workspace/memos/<type>-<slugified-title>.md
		slug := slugify(title)
		filePath = fmt.Sprintf("/sandbox/workspace/memos/%s-%s.md", artifactType, slug)
	}

	// 1. Write file to sandbox
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("yield_artifact: failed to create memo dir: %w", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("yield_artifact: failed to write file: %w", err)
	}

	// 2. Persist to artifact DB (if configured)
	if t.db != nil {
		artifactID := t.agentID + ":" + artifactType + ":" + slugify(title)
		if err := t.db.SaveArtifact(ctx, &storage.DBArtifact{
			ID:      artifactID,
			AgentID: t.agentID,
			Type:    artifactType,
			Title:   title,
			Content: content,
		}); err != nil {
			// Non-fatal — file is the primary storage
			// Log but don't fail the tool call
			_ = err
		}
	}

	return map[string]any{
		"yielded":  true,
		"title":    title,
		"type":     artifactType,
		"filepath": filePath,
	}, nil
}

func slugify(s string) string {
	result := make([]byte, 0, len(s))
	prevDash := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			result = append(result, byte(r))
			prevDash = false
		} else if r >= 'A' && r <= 'Z' {
			result = append(result, byte(r+32))
			prevDash = false
		} else if !prevDash && len(result) > 0 {
			result = append(result, '-')
			prevDash = true
		}
	}
	// Trim trailing dash
	if len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	return string(result)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
