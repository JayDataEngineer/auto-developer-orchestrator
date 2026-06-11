package env

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// FilePair is a (host_path, remote_path) tuple for file sync operations.
type FilePair [2]string

// FileSyncManager tracks local file changes and syncs to a remote environment.
//
// Ported from Hermes FileSyncManager. Backends instantiate this with transport
// callbacks (upload, delete) and a file-source callable. The manager handles
// mtime+size change detection, deletion tracking, rate limiting, and
// transactional state rollback on failure.
//
// Not used by bind-mount backends (local filesystem) — those get live host FS
// views and don't need file sync.
type FileSyncManager struct {
	mu sync.Mutex

	// Transport callbacks provided by each backend
	getFiles       func() []FilePair                     // enumerate files to sync
	upload         func(ctx context.Context, p FilePair) error // single file upload
	bulkUpload     BulkTransfer                          // bulk upload (tar pipe)
	delete         func(ctx context.Context, paths []string) error // batch delete

	// Change tracking state
	syncedFiles map[string]fileMeta   // remote_path → (mtime, size)
	pushedHashes map[string]string    // remote_path → sha256 hex
	lastSync    time.Time             // monotonic; zero ensures first sync runs

	// Configuration
	syncInterval time.Duration // minimum time between syncs
}

type fileMeta struct {
	mtime int64 // modification time (unix nanoseconds)
	size  int64 // file size in bytes
}

// NewFileSyncManager creates a new sync manager with the given transport callbacks.
//
//   - getFiles: returns the current list of (host, remote) file pairs
//   - bulkTransfer: if non-nil, used for bulk upload; otherwise falls back to per-file upload
//   - syncInterval: minimum time between automatic syncs (0 = default 5s)
func NewFileSyncManager(
	getFiles func() []FilePair,
	bulkTransfer BulkTransfer,
	syncInterval time.Duration,
) *FileSyncManager {
	if syncInterval == 0 {
		syncInterval = 5 * time.Second
	}
	return &FileSyncManager{
		getFiles:     getFiles,
		bulkUpload:   bulkTransfer,
		syncedFiles:  make(map[string]fileMeta),
		pushedHashes: make(map[string]string),
		syncInterval: syncInterval,
	}
}

// SetSingleUpload sets the per-file upload callback.
// Used as fallback when BulkTransfer.Upload is unavailable.
func (m *FileSyncManager) SetSingleUpload(fn func(ctx context.Context, p FilePair) error) {
	m.upload = fn
}

// SetDelete sets the batch delete callback.
func (m *FileSyncManager) SetDelete(fn func(ctx context.Context, paths []string) error) {
	m.delete = fn
}

// Sync runs a sync cycle: upload changed files, delete removed files.
// Rate-limited to once per syncInterval unless force is true.
// Transactional: state only committed if ALL operations succeed.
// On failure, state rolls back so the next cycle retries everything.
func (m *FileSyncManager) Sync(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !force {
		if time.Since(m.lastSync) < m.syncInterval {
			return nil
		}
	}

	currentFiles := m.getFiles()
	currentRemotePaths := make(map[string]bool, len(currentFiles))
	for _, pair := range currentFiles {
		currentRemotePaths[pair[1]] = true
	}

	// --- Uploads: new or changed files ---
	var toUpload []FilePair
	newFiles := make(map[string]fileMeta, len(m.syncedFiles))
	for k, v := range m.syncedFiles {
		newFiles[k] = v
	}

	for _, pair := range currentFiles {
		hostPath := pair[0]
		remotePath := pair[1]

		meta, err := fileMetaForPath(hostPath)
		if err != nil {
			continue
		}

		if existing, ok := m.syncedFiles[remotePath]; ok && existing == meta {
			// Unchanged
			newFiles[remotePath] = meta
			continue
		}

		toUpload = append(toUpload, pair)
		newFiles[remotePath] = meta
	}

	// --- Deletes: synced paths no longer in current set ---
	var toDelete []string
	for remotePath := range m.syncedFiles {
		if !currentRemotePaths[remotePath] {
			toDelete = append(toDelete, remotePath)
		}
	}

	if len(toUpload) == 0 && len(toDelete) == 0 {
		m.lastSync = time.Now()
		return nil
	}

	// Snapshot for rollback
	prevFiles := make(map[string]fileMeta, len(m.syncedFiles))
	for k, v := range m.syncedFiles {
		prevFiles[k] = v
	}
	prevHashes := make(map[string]string, len(m.pushedHashes))
	for k, v := range m.pushedHashes {
		prevHashes[k] = v
	}

	// Execute operations
	if len(toUpload) > 0 {
		if m.bulkUpload != nil {
			pairs := make([][2]string, len(toUpload))
			for i, p := range toUpload {
				pairs[i] = [2]string(p)
			}
			if err := m.bulkUpload.Upload(ctx, pairs); err != nil {
				m.syncedFiles = prevFiles
				m.pushedHashes = prevHashes
				m.lastSync = time.Now()
				return fmt.Errorf("bulk upload failed, rolled back: %w", err)
			}
		} else if m.upload != nil {
			for _, pair := range toUpload {
				if err := m.upload(ctx, pair); err != nil {
					m.syncedFiles = prevFiles
					m.pushedHashes = prevHashes
					m.lastSync = time.Now()
					return fmt.Errorf("upload %s failed, rolled back: %w", pair[1], err)
				}
			}
		}
	}

	if len(toDelete) > 0 && m.delete != nil {
		if err := m.delete(ctx, toDelete); err != nil {
			m.syncedFiles = prevFiles
			m.pushedHashes = prevHashes
			m.lastSync = time.Now()
			return fmt.Errorf("delete failed, rolled back: %w", err)
		}
	}

	// Commit: update pushed hashes for uploaded files
	for _, pair := range toUpload {
		hash, err := sha256File(pair[0])
		if err == nil {
			m.pushedHashes[pair[1]] = hash
		}
	}

	// Remove deleted files from tracking
	for _, p := range toDelete {
		delete(newFiles, p)
		delete(m.pushedHashes, p)
	}

	m.syncedFiles = newFiles
	m.lastSync = time.Now()

	return nil
}

// PushedHashes returns a copy of the pushed hash map for sync-back.
func (m *FileSyncManager) PushedHashes() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string, len(m.pushedHashes))
	for k, v := range m.pushedHashes {
		result[k] = v
	}
	return result
}

// SyncedCount returns the number of tracked synced files.
func (m *FileSyncManager) SyncedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.syncedFiles)
}

// ── helpers ──

func fileMetaForPath(path string) (fileMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	if !info.Mode().IsRegular() {
		return fileMeta{}, fmt.Errorf("not a regular file: %s", path)
	}
	return fileMeta{
		mtime: info.ModTime().UnixNano(),
		size:  info.Size(),
	}, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
