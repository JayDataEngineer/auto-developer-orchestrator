package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	sessionVersion      = 1
	sessionFilePattern  = "session-%s.json"
	sessionDirName      = ".pi_sessions"
	maxRotatedFiles     = 3
	rotateAfterBytes    = 256 * 1024 // 256KB
	maxSessionChars     = 50000      // max chars to replay per session
)

// PersistedSession represents a persistable session snapshot.
type PersistedSession struct {
	Version      uint   `json:"version"`
	SessionID    string `json:"sessionId"`
	ProjectDir   string `json:"projectDir"`
	AgentID      string `json:"agentId"`
	CreatedAtMs  int64  `json:"createdAtMs"`
	UpdatedAtMs  int64  `json:"updatedAtMs"`
	MessageCount int    `json:"messageCount"`
	// Compaction tracking
	CompactionCount    int    `json:"compactionCount"`
	RemovedMessages    int    `json:"removedMessages"`
	CompactionSummary  string `json:"compactionSummary,omitempty"`
	// Sub-agent tracking
	SubAgents []SubAgentResult `json:"subAgents,omitempty"`
	// Fork tracking
	ParentSessionID string `json:"parentSessionId,omitempty"`
}

// SessionManager handles session persistence and resume.
type SessionManager struct {
	mu      sync.Mutex
	logger  *zap.Logger
	baseDir string // directory for session files
}

// NewSessionManager creates a new session manager.
func NewSessionManager(logger *zap.Logger) *SessionManager {
	baseDir := filepath.Join(os.TempDir(), sessionDirName)
	os.MkdirAll(baseDir, 0755)
	return &SessionManager{
		logger:  logger,
		baseDir: baseDir,
	}
}

// Save persists a session state to disk.
func (sm *SessionManager) Save(ctx context.Context, state PersistedSession) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if state.Version == 0 {
		state.Version = sessionVersion
	}
	state.UpdatedAtMs = time.Now().UnixMilli()

	sessionPath := sm.sessionPath(state.SessionID)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Rotate if file is too large
	if info, err := os.Stat(sessionPath); err == nil && info.Size() > rotateAfterBytes {
		sm.rotateLocked(sessionPath)
	}

	if err := os.WriteFile(sessionPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	sm.logger.Debug("Session saved",
		zap.String("sessionId", state.SessionID),
		zap.Int("messageCount", state.MessageCount),
	)
	return nil
}

// Load reads a session state from disk.
func (sm *SessionManager) Load(ctx context.Context, sessionID string) (*PersistedSession, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionPath := sm.sessionPath(sessionID)
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // session not found is not an error
		}
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var state PersistedSession
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}

	return &state, nil
}

// Delete removes a session from disk.
func (sm *SessionManager) Delete(ctx context.Context, sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionPath := sm.sessionPath(sessionID)
	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Clean up rotated files too
	for i := 1; i <= maxRotatedFiles; i++ {
		rotated := fmt.Sprintf("%s.%d", sessionPath, i)
		os.Remove(rotated)
	}

	return nil
}

// List returns all sessions for a given project directory.
func (sm *SessionManager) List(ctx context.Context, projectDir string) ([]PersistedSession, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entries, err := os.ReadDir(sm.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []PersistedSession
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "session-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sm.baseDir, entry.Name()))
		if err != nil {
			continue
		}
		var state PersistedSession
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}
		if projectDir == "" || state.ProjectDir == projectDir {
			sessions = append(sessions, state)
		}
	}

	return sessions, nil
}

// RecordSubAgent adds a sub-agent result to a session.
func (sm *SessionManager) RecordSubAgent(ctx context.Context, sessionID string, result SubAgentResult) error {
	state, err := sm.Load(ctx, sessionID)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	state.SubAgents = append(state.SubAgents, result)
	return sm.Save(ctx, *state)
}

// RecordCompaction updates session with compaction info.
func (sm *SessionManager) RecordCompaction(ctx context.Context, sessionID string, removedMessages int, summary string) error {
	state, err := sm.Load(ctx, sessionID)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	state.CompactionCount++
	state.RemovedMessages += removedMessages
	state.CompactionSummary = summary
	return sm.Save(ctx, *state)
}

// NewPersistedSession creates a new session state with a unique ID.
func NewPersistedSession(projectDir, agentID string) PersistedSession {
	now := time.Now().UnixMilli()
	return PersistedSession{
		Version:     sessionVersion,
		SessionID:   fmt.Sprintf("sess-%d", now),
		ProjectDir:  projectDir,
		AgentID:     agentID,
		CreatedAtMs: now,
		UpdatedAtMs: now,
	}
}

// sessionPath returns the file path for a session.
func (sm *SessionManager) sessionPath(sessionID string) string {
	// Sanitize session ID for filesystem safety
	safeID := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, sessionID)
	return filepath.Join(sm.baseDir, fmt.Sprintf(sessionFilePattern, safeID))
}

// rotateLocked rotates session files (keeps up to maxRotatedFiles).
func (sm *SessionManager) rotateLocked(path string) {
	// Delete oldest rotated file
	oldest := fmt.Sprintf("%s.%d", path, maxRotatedFiles)
	os.Remove(oldest)

	// Shift rotated files
	for i := maxRotatedFiles - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", path, i)
		new := fmt.Sprintf("%s.%d", path, i+1)
		os.Rename(old, new)
	}

	// Rotate current file to .1
	os.Rename(path, fmt.Sprintf("%s.1", path))
}
