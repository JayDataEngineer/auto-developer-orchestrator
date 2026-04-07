package scheduler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RunLogEntry is a single entry in a job's run log.
type RunLogEntry struct {
	Ts             int64  `json:"ts"`
	JobID          string `json:"jobId"`
	Action         string `json:"action"`
	Status         string `json:"status,omitempty"`
	Error          string `json:"error,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Delivered      bool   `json:"delivered,omitempty"`
	DeliveryStatus string `json:"deliveryStatus,omitempty"`
	DeliveryError  string `json:"deliveryError,omitempty"`
	SessionID      string `json:"sessionId,omitempty"`
	RunAtMs        int64  `json:"runAtMs,omitempty"`
	DurationMs     int64  `json:"durationMs,omitempty"`
	NextRunAtMs    int64  `json:"nextRunAtMs,omitempty"`
	Model          string `json:"model,omitempty"`
	Provider       string `json:"provider,omitempty"`
	InputTokens    int    `json:"inputTokens,omitempty"`
	OutputTokens   int    `json:"outputTokens,omitempty"`
	CacheTokens    int    `json:"cacheTokens,omitempty"`
	JobName        string `json:"jobName,omitempty"` // Added when reading "all" scope
}

const (
	defaultRunLogMaxBytes  = 2_000_000 // 2MB
	defaultRunLogKeepLines = 2_000
)

// RunLogManager manages JSONL run logs per job.
type RunLogManager struct {
	baseDir string
	mu      sync.Mutex // serializes writes per path
}

// NewRunLogManager creates a run log manager.
func NewRunLogManager(baseDir string) (*RunLogManager, error) {
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home dir: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".auto-developer-orchestrator", "scheduler", "runs")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create run log dir: %w", err)
	}
	return &RunLogManager{baseDir: baseDir}, nil
}

// jobPath returns the JSONL file path for a job.
func (m *RunLogManager) jobPath(jobID string) string {
	safe := strings.NewReplacer("/", "", "\\", "", "\x00", "").Replace(jobID)
	return filepath.Join(m.baseDir, safe+".jsonl")
}

// Append writes a run log entry to the job's log file.
func (m *RunLogManager) Append(entry RunLogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry.Action = "finished"
	if entry.Ts == 0 {
		entry.Ts = time.Now().UnixMilli()
	}
	if entry.RunAtMs == 0 {
		entry.RunAtMs = time.Now().UnixMilli()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal run log entry: %w", err)
	}

	path := m.jobPath(entry.JobID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open run log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write run log: %w", err)
	}

	// Prune if file is too large
	return m.prune(path, defaultRunLogMaxBytes, defaultRunLogKeepLines)
}

// Read reads the last N entries for a job.
func (m *RunLogManager) Read(jobID string, limit int) ([]RunLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	path := m.jobPath(jobID)
	return m.readEntries(path, limit, "", "", "", "")
}

// ReadAll reads entries across all jobs, filtered and paginated.
func (m *RunLogManager) ReadAll(limit int, statusFilter, jobIDFilter string) ([]RunLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return nil, err
	}

	var all []RunLogEntry
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(m.baseDir, e.Name())
		entries, err := m.readEntries(path, 1000, statusFilter, jobIDFilter, "", "")
		if err != nil {
			continue
		}
		all = append(all, entries...)
	}

	// Sort by timestamp descending
	sort.Slice(all, func(i, j int) bool {
		return all[i].Ts > all[j].Ts
	})

	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// readEntries reads the last N entries from a JSONL file, optionally filtered.
func (m *RunLogManager) readEntries(path string, limit int, statusFilter, jobIDFilter, _, _ string) ([]RunLogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []RunLogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		var entry RunLogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		// Apply filters
		if statusFilter != "" && entry.Status != statusFilter && statusFilter != "all" {
			continue
		}
		if jobIDFilter != "" && entry.JobID != jobIDFilter {
			continue
		}
		entries = append(entries, entry)
	}

	// Reverse to get newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// prune keeps only the last keepLines entries if the file exceeds maxBytes.
func (m *RunLogManager) prune(path string, maxBytes, keepLines int) error {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= int64(maxBytes) {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Read all lines
	var lines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := make([]byte, len(scanner.Bytes()))
		copy(line, scanner.Bytes())
		lines = append(lines, line)
	}

	if len(lines) <= keepLines {
		return nil
	}

	// Keep only the last keepLines
	kept := lines[len(lines)-keepLines:]

	tmpPath := path + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	for _, line := range kept {
		if _, err := out.Write(append(line, '\n')); err != nil {
			out.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	out.Close()

	return os.Rename(tmpPath, path)
}
