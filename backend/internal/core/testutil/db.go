package testutil

import (
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

// NewTempDB returns a SQLite database in a fresh temp directory, auto-closing
// it via t.Cleanup. Use it to replace the repeated setup block in handler tests:
//
//	dir := t.TempDir()
//	db, err := storage.NewDatabase(dir + "/test.db")
//	if err != nil { t.Fatal(err) }
//	t.Cleanup(func() { db.Close() })
func NewTempDB(t *testing.T) *storage.Database {
	t.Helper()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewTempDB: create database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
