package checkpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileVersion represents a single backed-up version of a file.
type FileVersion struct {
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	Version   int       `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Size      int64     `json:"size"`
}

// Snapshot represents a point-in-time collection of tracked file versions.
type Snapshot struct {
	ID        string                  `json:"id"`
	SessionID string                  `json:"sessionId"`
	Timestamp time.Time               `json:"timestamp"`
	Files     map[string]*FileVersion `json:"files"`
	Round     int                     `json:"round"`
	Label     string                  `json:"label"`
	FileCount int                     `json:"fileCount"`
}

// Manager orchestrates file checkpointing for a single session.
type Manager struct {
	sessionID  string
	projectDir string
	basePath   string

	mu        sync.RWMutex
	versions  map[string][]*FileVersion // path -> ordered version list
	snapshots []*Snapshot
	hashCache map[string]string // path -> current hash (avoids redundant reads)

	maxSnapshots int
	maxVersions  int
	logger       *log.Logger
}

// NewManager creates a checkpoint manager for a session.
// basePath is typically ~/.pi/agent/checkpoints/{sessionID}/
func NewManager(sessionID, projectDir, basePath string) *Manager {
	return &Manager{
		sessionID:    sessionID,
		projectDir:   projectDir,
		basePath:     basePath,
		versions:     make(map[string][]*FileVersion),
		snapshots:    nil,
		hashCache:    make(map[string]string),
		maxSnapshots: 100,
		maxVersions:  50,
		logger:       log.Default(),
	}
}

// SessionID returns the session ID this manager is scoped to.
func (m *Manager) SessionID() string { return m.sessionID }

// TrackBeforeWrite backs up a file before it gets modified.
// Reads current content, hashes it, skips if unchanged since last backup.
// filePath can be absolute or relative to projectDir.
func (m *Manager) TrackBeforeWrite(ctx context.Context, filePath string) (int, error) {
	absPath := m.resolvePath(filePath)

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet — no pre-version to save.
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", absPath, err)
	}

	if info.IsDir() {
		return 0, nil
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", absPath, err)
	}

	hash := hashBytes(content)
	relPath := m.relPath(absPath)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Skip if unchanged since last backup
	if lastHash, ok := m.hashCache[relPath]; ok && lastHash == hash {
		return 0, nil
	}

	// Find next version number
	vNum := 1
	if vs, ok := m.versions[relPath]; ok && len(vs) > 0 {
		vNum = vs[len(vs)-1].Version + 1
	}

	fv := &FileVersion{
		Path:      relPath,
		Hash:      hash,
		Version:   vNum,
		Timestamp: time.Now(),
		Size:      info.Size(),
	}

	// Save to disk
	if err := m.saveVersion(relPath, hash, vNum, content); err != nil {
		return 0, fmt.Errorf("save version: %w", err)
	}

	m.versions[relPath] = append(m.versions[relPath], fv)
	m.hashCache[relPath] = hash

	// Trim versions if over limit
	if len(m.versions[relPath]) > m.maxVersions {
		m.trimVersions(relPath)
	}

	return vNum, nil
}

// TrackBeforeBash backs up tracked files before a destructive bash command.
// Since we can't know which files a bash command will touch, we re-backup
// all currently tracked files that have changed since their last version.
func (m *Manager) TrackBeforeBash(ctx context.Context, command string) (int, error) {
	m.mu.RLock()
	tracked := make([]string, 0, len(m.versions))
	for p := range m.versions {
		tracked = append(tracked, p)
	}
	m.mu.RUnlock()

	// Also check hashCache for files read but not yet versioned
	m.mu.RLock()
	for p := range m.hashCache {
		if _, ok := m.versions[p]; !ok {
			tracked = append(tracked, p)
		}
	}
	m.mu.RUnlock()

	backedUp := 0
	for _, relPath := range tracked {
		absPath := filepath.Join(m.projectDir, relPath)
		if _, err := os.Stat(absPath); err != nil {
			continue
		}
		v, err := m.TrackBeforeWrite(ctx, absPath)
		if err != nil {
			m.logger.Printf("checkpoint: failed to backup %s before bash: %v", relPath, err)
			continue
		}
		if v > 0 {
			backedUp++
		}
	}
	return backedUp, nil
}

// CreateSnapshot captures the current state of all tracked files.
// Called at agent start. Idempotent: skips unchanged files.
func (m *Manager) CreateSnapshot(ctx context.Context, label string, round int) (*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := &Snapshot{
		ID:        fmt.Sprintf("snap-%d", time.Now().UnixNano()),
		SessionID: m.sessionID,
		Timestamp: time.Now(),
		Files:     make(map[string]*FileVersion),
		Round:     round,
		Label:     label,
	}

	// Snapshot all tracked files with their latest version
	for relPath, versions := range m.versions {
		if len(versions) > 0 {
			snap.Files[relPath] = versions[len(versions)-1]
		}
	}

	snap.FileCount = len(snap.Files)
	m.snapshots = append(m.snapshots, snap)

	// Prune oldest if over limit
	if len(m.snapshots) > m.maxSnapshots {
		m.snapshots = m.snapshots[len(m.snapshots)-m.maxSnapshots:]
	}

	return snap, nil
}

// RestoreSnapshot restores all files to the state captured by a snapshot.
// Returns the list of files restored.
func (m *Manager) RestoreSnapshot(ctx context.Context, snapshotID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := m.findSnapshot(snapshotID)
	if snap == nil {
		return nil, fmt.Errorf("snapshot %s not found", snapshotID)
	}

	var restored []string
	for relPath, fv := range snap.Files {
		absPath := filepath.Join(m.projectDir, relPath)

		// Read current content
		current, err := os.ReadFile(absPath)
		if err != nil && !os.IsNotExist(err) {
			m.logger.Printf("checkpoint: skip %s: %v", relPath, err)
			continue
		}
		currentHash := hashBytes(current)

		// Skip if already at this version
		if currentHash == fv.Hash {
			continue
		}

		// Load backup (use locked variant since we hold the write lock)
		backupContent, err := m.loadVersionLocked(relPath, fv.Version)
		if err != nil {
			m.logger.Printf("checkpoint: load version %s@v%d failed: %v", relPath, fv.Version, err)
			continue
		}

		// Write back
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			continue
		}
		if err := os.WriteFile(absPath, backupContent, 0644); err != nil {
			m.logger.Printf("checkpoint: restore %s failed: %v", relPath, err)
			continue
		}
		restored = append(restored, relPath)
	}

	return restored, nil
}

// ListSnapshots returns all snapshots in chronological order.
func (m *Manager) ListSnapshots() []*Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Snapshot, len(m.snapshots))
	copy(out, m.snapshots)
	return out
}

// ListFileVersions returns all versions of a specific file.
func (m *Manager) ListFileVersions(relPath string) []*FileVersion {
	m.mu.RLock()
	defer m.mu.RUnlock()
	vs, ok := m.versions[relPath]
	if !ok {
		return nil
	}
	out := make([]*FileVersion, len(vs))
	copy(out, vs)
	return out
}

// GetSnapshot returns a specific snapshot by ID.
func (m *Manager) GetSnapshot(id string) *Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.findSnapshot(id)
}

// LoadFileVersion loads the content of a specific file version.
// Used by the HTTP API to preview/download backed up content.
func (m *Manager) LoadFileVersion(relPath string, version int) ([]byte, error) {
	return m.loadVersion(relPath, version)
}

// Close persists the manifest to disk.
func (m *Manager) Close() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveManifest()
}

// Load restores state from a previously saved manifest.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadManifest()
}

// --- internal helpers ---

func (m *Manager) findSnapshot(id string) *Snapshot {
	for _, s := range m.snapshots {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (m *Manager) resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(m.projectDir, p)
}

func (m *Manager) relPath(absPath string) string {
	rel, err := filepath.Rel(m.projectDir, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

func (m *Manager) trimVersions(relPath string) {
	vs := m.versions[relPath]
	if len(vs) <= m.maxVersions {
		return
	}
	// Remove oldest versions from disk
	for _, fv := range vs[:len(vs)-m.maxVersions] {
		p := m.versionDiskPath(relPath, fv.Version)
		os.Remove(p)
	}
	m.versions[relPath] = vs[len(vs)-m.maxVersions:]
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:16]
}
