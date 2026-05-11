package context

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// SpillStore manages offloaded tool results on disk.
// Each spill creates a file under the configured spill directory.
type SpillStore struct {
	mu       sync.Mutex
	spillDir string
	index    map[string]*SpillEntry
}

// SpillEntry tracks a single offloaded result.
type SpillEntry struct {
	Ref        string
	FilePath   string
	ToolName   string
	ToolCallID string
	Size       int64
	Preview    string
}

func NewSpillStore(spillDir string) *SpillStore {
	return &SpillStore{
		spillDir: spillDir,
		index:    make(map[string]*SpillEntry),
	}
}

// Spill writes content to a spill file and returns a SpillEntry.
func (s *SpillStore) Spill(toolName, toolCallID, content, preview string) (*SpillEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.spillDir, 0755); err != nil {
		return nil, fmt.Errorf("spill: create dir: %w", err)
	}

	hash := sha256.Sum256([]byte(content))
	ref := "spill-" + hex.EncodeToString(hash[:3])[:6]

	filePath := filepath.Join(s.spillDir, ref+".txt")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("spill: write file: %w", err)
	}

	entry := &SpillEntry{
		Ref:        ref,
		FilePath:   filePath,
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Size:       int64(len(content)),
		Preview:    preview,
	}
	s.index[ref] = entry
	return entry, nil
}

// Load retrieves the full content for a spill reference.
func (s *SpillStore) Load(ref string) (string, error) {
	s.mu.Lock()
	entry, ok := s.index[ref]
	s.mu.Unlock()

	if ok {
		data, err := os.ReadFile(entry.FilePath)
		if err != nil {
			return "", fmt.Errorf("spill: read %q: %w", ref, err)
		}
		return string(data), nil
	}

	// Fallback: try loading from disk directly (survives restart)
	filePath := filepath.Join(s.spillDir, ref+".txt")
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("spill ref %q not found", ref)
	}
	return string(data), nil
}

// TotalBytes returns the total size of all spilled content.
func (s *SpillStore) TotalBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, e := range s.index {
		total += e.Size
	}
	return total
}

// Count returns the number of spilled entries.
func (s *SpillStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.index)
}

// Cleanup removes all spill files.
func (s *SpillStore) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.index {
		os.Remove(e.FilePath)
	}
	s.index = make(map[string]*SpillEntry)
	return os.RemoveAll(s.spillDir)
}
