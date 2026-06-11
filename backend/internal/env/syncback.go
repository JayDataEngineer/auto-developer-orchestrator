package env

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SyncBack pulls remote changes to the host filesystem on environment teardown.
//
// Ported from Hermes FileSyncManager.sync_back(). Downloads the remote directory
// as a tar archive, extracts to staging, and applies only files that differ from
// what was originally pushed (based on SHA-256 content hashes).
//
// Uses exponential backoff retry (3 attempts with 2s, 4s, 8s delays).
// Last-write-wins conflict resolution when both host and remote changed.
type SyncBack struct {
	transfer    BulkTransfer
	pushedHashes map[string]string // remote_path → sha256 (from FileSyncManager)
	getFiles    func() []FilePair  // enumerate current file mapping
}

// SyncBackConfig configures a sync-back operation.
type SyncBackConfig struct {
	Transfer     BulkTransfer
	PushedHashes map[string]string
	GetFiles     func() []FilePair
}

// NewSyncBack creates a sync-back runner.
func NewSyncBack(cfg SyncBackConfig) *SyncBack {
	return &SyncBack{
		transfer:     cfg.Transfer,
		pushedHashes: cfg.PushedHashes,
		getFiles:     cfg.GetFiles,
	}
}

const syncBackMaxRetries = 3

var syncBackBackoff = [3]time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

// Run executes the sync-back with retry.
// Downloads remote files, diffs against pushed state, applies changes.
func (sb *SyncBack) Run(ctx context.Context, remoteBase string) error {
	// Nothing was ever pushed — skip to avoid retry storms
	if len(sb.pushedHashes) == 0 {
		return nil
	}

	var lastErr error
	for attempt := 0; attempt < syncBackMaxRetries; attempt++ {
		err := sb.runOnce(ctx, remoteBase)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < syncBackMaxRetries-1 {
			delay := syncBackBackoff[attempt]
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("sync_back: all %d attempts failed: %w", syncBackMaxRetries, lastErr)
}

func (sb *SyncBack) runOnce(ctx context.Context, remoteBase string) error {
	// Download remote as tar archive → staging dir
	staging, err := sb.transfer.Download(ctx, remoteBase)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer os.RemoveAll(staging)

	// Cache file mapping for host path resolution
	var fileMapping []FilePair
	if sb.getFiles != nil {
		fileMapping = sb.getFiles()
	}

	// Walk staging and apply changed files
	applied := 0
	err = filepath.WalkDir(staging, func(stagedPath string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		// Compute the remote path from the staging structure
		rel, err := filepath.Rel(staging, stagedPath)
		if err != nil {
			return nil
		}
		remotePath := "/" + filepath.ToSlash(rel)

		// Check against pushed hash
		pushedHash, wasPushed := sb.pushedHashes[remotePath]

		if wasPushed {
			remoteHash, err := sha256File(stagedPath)
			if err != nil {
				return nil
			}
			if remoteHash == pushedHash {
				// Unchanged from what we pushed — skip
				return nil
			}
		}

		// Resolve host path from file mapping
		hostPath := sb.resolveHostPath(remotePath, fileMapping)
		if hostPath == "" {
			hostPath = sb.inferHostPath(remotePath, fileMapping)
			if hostPath == "" {
				return nil // no mapping, skip
			}
		}

		// Conflict detection: host modified since push AND remote also changed
		if wasPushed {
			if hostHash, err := sha256File(hostPath); err == nil {
				if hostHash != pushedHash {
					// Host was modified too — last-write-wins (apply remote)
					// In practice we just overwrite, matching Hermes behavior
				}
			}
		}

		// Apply: copy remote version to host
		if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
			return nil
		}
		copyFile(stagedPath, hostPath)
		applied++

		return nil
	})
	if err != nil {
		return err
	}

	if applied == 0 {
		// No remote changes detected
	}

	return nil
}

// resolveHostPath finds the host path for a known remote path from the mapping.
func (sb *SyncBack) resolveHostPath(remotePath string, mapping []FilePair) string {
	for _, pair := range mapping {
		if pair[1] == remotePath {
			return pair[0]
		}
	}
	return ""
}

// inferHostPath infers a host path for a new remote file by matching parent
// directory prefixes from the existing file mapping.
func (sb *SyncBack) inferHostPath(remotePath string, mapping []FilePair) string {
	for _, pair := range mapping {
		remoteDir := filepath.Dir(pair[1])
		if strings.HasPrefix(remotePath, remoteDir+"/") {
			hostDir := filepath.Dir(pair[0])
			suffix := remotePath[len(remoteDir):]
			return hostDir + suffix
		}
	}
	return ""
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return os.WriteFile(dst, data, 0o644)
	}
	return os.WriteFile(dst, data, info.Mode())
}
