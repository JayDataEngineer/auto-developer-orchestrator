package env

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ── FileSyncManager tests ──

func TestFileSyncManager_FirstSync(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("world"), 0o644)

	var uploaded []string
	var mu sync.Mutex

	mgr := NewFileSyncManager(
		func() []FilePair {
			return []FilePair{
				{filepath.Join(tmpDir, "a.txt"), "/remote/a.txt"},
				{filepath.Join(tmpDir, "b.txt"), "/remote/b.txt"},
			}
		},
		nil, // no bulk transfer
		0,   // default interval
	)
	mgr.SetSingleUpload(func(ctx context.Context, p FilePair) error {
		mu.Lock()
		uploaded = append(uploaded, p[1])
		mu.Unlock()
		return nil
	})

	if err := mgr.Sync(context.Background(), true); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(uploaded) != 2 {
		t.Errorf("expected 2 uploads, got %d: %v", len(uploaded), uploaded)
	}
	if mgr.SyncedCount() != 2 {
		t.Errorf("SyncedCount = %d, want 2", mgr.SyncedCount())
	}
}

func TestFileSyncManager_SkipsUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello"), 0o644)

	uploadCount := 0

	mgr := NewFileSyncManager(
		func() []FilePair {
			return []FilePair{
				{filepath.Join(tmpDir, "a.txt"), "/remote/a.txt"},
			}
		},
		nil,
		0,
	)
	mgr.SetSingleUpload(func(ctx context.Context, p FilePair) error {
		uploadCount++
		return nil
	})

	// First sync — should upload
	mgr.Sync(context.Background(), true)
	if uploadCount != 1 {
		t.Fatalf("first sync: expected 1 upload, got %d", uploadCount)
	}

	// Second sync — file unchanged, should skip
	mgr.Sync(context.Background(), true)
	if uploadCount != 1 {
		t.Errorf("second sync: expected 1 upload (unchanged), got %d", uploadCount)
	}
}

func TestFileSyncManager_DetectsModification(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "a.txt")
	os.WriteFile(file, []byte("v1"), 0o644)

	uploadCount := 0

	mgr := NewFileSyncManager(
		func() []FilePair {
			return []FilePair{{file, "/remote/a.txt"}}
		},
		nil,
		0,
	)
	mgr.SetSingleUpload(func(ctx context.Context, p FilePair) error {
		uploadCount++
		return nil
	})

	// First sync
	mgr.Sync(context.Background(), true)
	if uploadCount != 1 {
		t.Fatalf("first sync: expected 1 upload, got %d", uploadCount)
	}

	// Modify the file (ensure mtime changes)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(file, []byte("v2"), 0o644)

	// Second sync — should re-upload
	mgr.Sync(context.Background(), true)
	if uploadCount != 2 {
		t.Errorf("second sync after modification: expected 2 uploads, got %d", uploadCount)
	}
}

func TestFileSyncManager_DetectsDeletion(t *testing.T) {
	tmpDir := t.TempDir()
	fileA := filepath.Join(tmpDir, "a.txt")
	fileB := filepath.Join(tmpDir, "b.txt")
	os.WriteFile(fileA, []byte("a"), 0o644)
	os.WriteFile(fileB, []byte("b"), 0o644)

	var deleted []string
	getFiles := func() []FilePair {
		return []FilePair{
			{fileA, "/remote/a.txt"},
			{fileB, "/remote/b.txt"},
		}
	}

	mgr := NewFileSyncManager(getFiles, nil, 0)
	mgr.SetSingleUpload(func(ctx context.Context, p FilePair) error { return nil })
	mgr.SetDelete(func(ctx context.Context, paths []string) error {
		deleted = append(deleted, paths...)
		return nil
	})

	// First sync — both files
	mgr.Sync(context.Background(), true)

	// Now file B is removed from the listing
	getFiles = func() []FilePair {
		return []FilePair{{fileA, "/remote/a.txt"}}
	}
	mgr.getFiles = getFiles

	// Second sync — should delete b.txt
	mgr.Sync(context.Background(), true)

	if len(deleted) != 1 || deleted[0] != "/remote/b.txt" {
		t.Errorf("expected delete of /remote/b.txt, got %v", deleted)
	}
	if mgr.SyncedCount() != 1 {
		t.Errorf("SyncedCount = %d, want 1", mgr.SyncedCount())
	}
}

func TestFileSyncManager_RateLimiting(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello"), 0o644)

	uploadCount := 0
	mgr := NewFileSyncManager(
		func() []FilePair {
			return []FilePair{{filepath.Join(tmpDir, "a.txt"), "/remote/a.txt"}}
		},
		nil,
		10*time.Second, // long interval
	)
	mgr.SetSingleUpload(func(ctx context.Context, p FilePair) error {
		uploadCount++
		return nil
	})

	// First sync (force) — should upload
	mgr.Sync(context.Background(), true)
	if uploadCount != 1 {
		t.Fatalf("first sync: expected 1, got %d", uploadCount)
	}

	// Modify file
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("changed"), 0o644)

	// Non-forced sync — should be rate-limited
	mgr.Sync(context.Background(), false)
	if uploadCount != 1 {
		t.Errorf("rate-limited sync should skip, got %d uploads", uploadCount)
	}

	// Forced sync — should upload
	mgr.Sync(context.Background(), true)
	if uploadCount != 2 {
		t.Errorf("forced sync should upload, got %d uploads", uploadCount)
	}
}

func TestFileSyncManager_RollbackOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	fileA := filepath.Join(tmpDir, "a.txt")
	fileB := filepath.Join(tmpDir, "b.txt")
	os.WriteFile(fileA, []byte("a"), 0o644)
	os.WriteFile(fileB, []byte("b"), 0o644)

	mgr := NewFileSyncManager(
		func() []FilePair {
			return []FilePair{
				{fileA, "/remote/a.txt"},
				{fileB, "/remote/b.txt"},
			}
		},
		nil,
		0,
	)
	mgr.SetSingleUpload(func(ctx context.Context, p FilePair) error {
		if p[1] == "/remote/b.txt" {
			return fmt.Errorf("upload failed")
		}
		return nil
	})

	// Sync should fail and roll back
	err := mgr.Sync(context.Background(), true)
	if err == nil {
		t.Fatal("expected error from failed upload")
	}

	// State should be rolled back — no files tracked
	if mgr.SyncedCount() != 0 {
		t.Errorf("SyncedCount after rollback = %d, want 0", mgr.SyncedCount())
	}
}

func TestFileSyncManager_BulkUpload(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("world"), 0o644)

	var bulkUploaded [][2]string
	mockBulk := &mockBulkTransfer{
		uploadFn: func(ctx context.Context, files [][2]string) error {
			bulkUploaded = files
			return nil
		},
	}

	mgr := NewFileSyncManager(
		func() []FilePair {
			return []FilePair{
				{filepath.Join(tmpDir, "a.txt"), "/remote/a.txt"},
				{filepath.Join(tmpDir, "b.txt"), "/remote/b.txt"},
			}
		},
		mockBulk,
		0,
	)

	if err := mgr.Sync(context.Background(), true); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(bulkUploaded) != 2 {
		t.Errorf("expected 2 bulk-uploaded files, got %d", len(bulkUploaded))
	}
}

func TestFileSyncManager_PushedHashes(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "a.txt")
	os.WriteFile(file, []byte("hello"), 0o644)

	mgr := NewFileSyncManager(
		func() []FilePair {
			return []FilePair{{file, "/remote/a.txt"}}
		},
		&mockBulkTransfer{
			uploadFn: func(ctx context.Context, files [][2]string) error { return nil },
		},
		0,
	)

	mgr.Sync(context.Background(), true)

	hashes := mgr.PushedHashes()
	if len(hashes) != 1 {
		t.Fatalf("expected 1 pushed hash, got %d", len(hashes))
	}

	// Verify the hash is a valid SHA-256 hex string
	hash, ok := hashes["/remote/a.txt"]
	if !ok {
		t.Fatal("missing hash for /remote/a.txt")
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA-256 hex)", len(hash))
	}
}

// ── SyncBack tests ──

func TestSyncBack_SkipsWhenNothingPushed(t *testing.T) {
	sb := NewSyncBack(SyncBackConfig{
		PushedHashes: map[string]string{}, // empty
	})

	// Should return nil immediately without downloading
	if err := sb.Run(context.Background(), "/remote"); err != nil {
		t.Errorf("expected nil when nothing pushed, got: %v", err)
	}
}

func TestSyncBack_AppliesChangedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Host files
	hostFile := filepath.Join(tmpDir, "host", "a.txt")
	os.MkdirAll(filepath.Dir(hostFile), 0o755)
	os.WriteFile(hostFile, []byte("original"), 0o644)

	// Simulate remote changed the file
	remoteContent := []byte("remote-modified")

	mockBulk := &mockBulkTransfer{
		downloadFn: func(ctx context.Context, remoteBase string) (string, error) {
			staging := filepath.Join(t.TempDir(), "sync-back-staging")
			os.MkdirAll(staging, 0o755)
			os.WriteFile(filepath.Join(staging, "a.txt"), remoteContent, 0o644)
			return staging, nil
		},
	}

	sb := NewSyncBack(SyncBackConfig{
		Transfer: mockBulk,
		PushedHashes: map[string]string{
			"/a.txt": "different-hash", // hash differs from remote → will apply
		},
		GetFiles: func() []FilePair {
			return []FilePair{{hostFile, "/a.txt"}}
		},
	})

	os.WriteFile(hostFile, []byte("original"), 0o644)

	if err := sb.Run(context.Background(), "/remote"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Host file should be updated with remote content
	data, _ := os.ReadFile(hostFile)
	if string(data) != "remote-modified" {
		t.Errorf("host file = %q, want 'remote-modified'", data)
	}
}

// ── Mock implementations ──

type mockBulkTransfer struct {
	uploadFn   func(ctx context.Context, files [][2]string) error
	downloadFn func(ctx context.Context, remoteBase string) (string, error)
	deleteFn   func(ctx context.Context, remotePaths []string) error
}

func (m *mockBulkTransfer) Upload(ctx context.Context, files [][2]string) error {
	if m.uploadFn != nil {
		return m.uploadFn(ctx, files)
	}
	return nil
}

func (m *mockBulkTransfer) Download(ctx context.Context, remoteBase string) (string, error) {
	if m.downloadFn != nil {
		return m.downloadFn(ctx, remoteBase)
	}
	return "", nil
}

func (m *mockBulkTransfer) Delete(ctx context.Context, remotePaths []string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, remotePaths)
	}
	return nil
}

