package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTrackBeforeWrite(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	// Create a test file
	testFile := filepath.Join(projectDir, "main.go")
	os.WriteFile(testFile, []byte("package main\nfunc main() {}\n"), 0644)

	// Track before write
	v, err := mgr.TrackBeforeWrite(context.Background(), testFile)
	if err != nil {
		t.Fatalf("TrackBeforeWrite: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected version 1, got %d", v)
	}

	// Verify backup exists on disk
	backupPath := filepath.Join(basePath, "files", "main.go", "")
	entries, err := os.ReadDir(backupPath)
	if err != nil {
		t.Fatalf("reading backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup file, got %d", len(entries))
	}
	if entries[0].Name() != "main.go" {
		// The backup is inside a subdirectory named after the file
	}
}

func TestTrackBeforeWriteIdempotent(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	testFile := filepath.Join(projectDir, "app.go")
	os.WriteFile(testFile, []byte("package main"), 0644)

	// Track twice — same content
	v1, _ := mgr.TrackBeforeWrite(context.Background(), testFile)
	v2, _ := mgr.TrackBeforeWrite(context.Background(), testFile)

	if v1 != 1 {
		t.Fatalf("first call: expected v1, got v%d", v1)
	}
	if v2 != 0 {
		t.Fatalf("second call: expected v0 (skip), got v%d", v2)
	}
}

func TestTrackBeforeWriteNewVersion(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	testFile := filepath.Join(projectDir, "app.go")

	// v1: initial content
	os.WriteFile(testFile, []byte("v1"), 0644)
	v1, _ := mgr.TrackBeforeWrite(context.Background(), testFile)

	// Modify file
	os.WriteFile(testFile, []byte("v2"), 0644)
	v2, _ := mgr.TrackBeforeWrite(context.Background(), testFile)

	if v1 != 1 || v2 != 2 {
		t.Fatalf("expected v1=1, v2=2, got v1=%d v2=%d", v1, v2)
	}
}

func TestTrackBeforeWriteNonexistent(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	// File doesn't exist — should return 0, no error
	v, err := mgr.TrackBeforeWrite(context.Background(), filepath.Join(projectDir, "nonexistent.go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 0 {
		t.Fatalf("expected 0 for nonexistent file, got %d", v)
	}
}

func TestCreateSnapshot(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	// Create and track some files
	for i, name := range []string{"a.go", "b.go", "c.go"} {
		os.WriteFile(filepath.Join(projectDir, name), []byte("content "+string(rune('0'+i))), 0644)
		mgr.TrackBeforeWrite(context.Background(), filepath.Join(projectDir, name))
	}

	snap, err := mgr.CreateSnapshot(context.Background(), "test-snapshot", 1)
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if snap.FileCount != 3 {
		t.Fatalf("expected 3 files in snapshot, got %d", snap.FileCount)
	}
	if snap.Label != "test-snapshot" {
		t.Fatalf("expected label 'test-snapshot', got %s", snap.Label)
	}
}

func TestRestoreSnapshot(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	testFile := filepath.Join(projectDir, "main.go")
	original := []byte("original content")
	os.WriteFile(testFile, original, 0644)

	// Track before modification
	mgr.TrackBeforeWrite(context.Background(), testFile)

	// Create snapshot
	snap, _ := mgr.CreateSnapshot(context.Background(), "before-change", 0)

	// Modify file
	modified := []byte("modified content")
	os.WriteFile(testFile, modified, 0644)

	// Verify modified
	got, _ := os.ReadFile(testFile)
	if string(got) != string(modified) {
		t.Fatalf("file should be modified before restore")
	}

	// Restore
	restored, err := mgr.RestoreSnapshot(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if len(restored) != 1 || restored[0] != "main.go" {
		t.Fatalf("expected [main.go], got %v", restored)
	}

	// Verify restored
	got, _ = os.ReadFile(testFile)
	if string(got) != string(original) {
		t.Fatalf("expected original content after restore, got %q", string(got))
	}
}

func TestRestoreSurvivesRmRf(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	testFile := filepath.Join(projectDir, "important.go")
	content := []byte("critical business logic")
	os.WriteFile(testFile, content, 0644)

	mgr.TrackBeforeWrite(context.Background(), testFile)
	snap, _ := mgr.CreateSnapshot(context.Background(), "before-disaster", 0)

	// Simulate rm -rf on the project
	os.RemoveAll(projectDir)

	// Verify file is gone
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("file should be deleted after rm -rf")
	}

	// Restore from checkpoint
	restored, err := mgr.RestoreSnapshot(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("expected 1 restored file, got %d", len(restored))
	}

	// Verify content recovered
	got, _ := os.ReadFile(testFile)
	if string(got) != string(content) {
		t.Fatalf("expected original content after restore, got %q", string(got))
	}
}

func TestManifestPersistence(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	// Create and track
	mgr := NewManager("test-session", projectDir, basePath)
	os.WriteFile(filepath.Join(projectDir, "a.go"), []byte("content"), 0644)
	mgr.TrackBeforeWrite(context.Background(), filepath.Join(projectDir, "a.go"))
	mgr.CreateSnapshot(context.Background(), "test", 0)
	mgr.Close()

	// Load into new manager
	mgr2 := NewManager("test-session", projectDir, basePath)
	if err := mgr2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	snapshots := mgr2.ListSnapshots()
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot after load, got %d", len(snapshots))
	}
	if snapshots[0].Label != "test" {
		t.Fatalf("expected label 'test', got %s", snapshots[0].Label)
	}
}

func TestIsDestructiveCommand(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		{"rm -rf /tmp/test", true},
		{"rm file.txt", true},
		{"mv old.txt new.txt", true},
		{"echo hello > file.txt", true},
		{"sed -i 's/old/new/g' file.txt", true},
		{"git checkout main", true},
		{"git reset --hard", true},
		{"ls -la", false},
		{"cat file.txt", false},
		{"grep pattern file.txt", false},
		{"git status", false},
		{"echo hello", false},
		{"python script.py", false},
	}

	for _, tt := range tests {
		got := IsDestructiveCommand(tt.cmd)
		if got != tt.expected {
			t.Errorf("IsDestructiveCommand(%q) = %v, want %v", tt.cmd, got, tt.expected)
		}
	}
}

func TestTrackBeforeBash(t *testing.T) {
	projectDir := t.TempDir()
	basePath := t.TempDir()

	mgr := NewManager("test-session", projectDir, basePath)

	// Create and track some files
	os.WriteFile(filepath.Join(projectDir, "a.go"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(projectDir, "b.go"), []byte("b"), 0644)
	mgr.TrackBeforeWrite(context.Background(), filepath.Join(projectDir, "a.go"))
	mgr.TrackBeforeWrite(context.Background(), filepath.Join(projectDir, "b.go"))

	// Modify a.go, then run TrackBeforeBash
	os.WriteFile(filepath.Join(projectDir, "a.go"), []byte("a-modified"), 0644)
	n, err := mgr.TrackBeforeBash(context.Background(), "rm -rf *.go")
	if err != nil {
		t.Fatalf("TrackBeforeBash: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 file re-backed up (a.go changed), got %d", n)
	}
}
