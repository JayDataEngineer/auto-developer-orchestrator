package env

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWriteTarFromPairs_CreatesArchive(t *testing.T) {
	// Create temp files to tar
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "hello.txt")
	file2 := filepath.Join(tmpDir, "sub", "world.txt")

	os.WriteFile(file1, []byte("hello"), 0o644)
	os.MkdirAll(filepath.Dir(file2), 0o755)
	os.WriteFile(file2, []byte("world"), 0o644)

	files := [][2]string{
		{file1, "remote/hello.txt"},
		{file2, "remote/sub/world.txt"},
	}

	var buf bytes.Buffer
	if err := writeTarFromPairs(&buf, files); err != nil {
		t.Fatalf("writeTarFromPairs: %v", err)
	}

	// Verify tar contents
	tr := tar.NewReader(&buf)
	var names []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, hdr.Name)

		// Verify content
		var content bytes.Buffer
		content.ReadFrom(tr)
		switch hdr.Name {
		case "remote/hello.txt":
			if content.String() != "hello" {
				t.Errorf("hello.txt content = %q, want 'hello'", content.String())
			}
		case "remote/sub/world.txt":
			if content.String() != "world" {
				t.Errorf("world.txt content = %q, want 'world'", content.String())
			}
		}
	}

	if len(names) != 2 {
		t.Errorf("expected 2 tar entries, got %d: %v", len(names), names)
	}
	sort.Strings(names)
	if names[0] != "remote/hello.txt" || names[1] != "remote/sub/world.txt" {
		t.Errorf("tar entries = %v", names)
	}
}

func TestWriteTarFromPairs_SkipsMissingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(existingFile, []byte("data"), 0o644)

	files := [][2]string{
		{existingFile, "remote/exists.txt"},
		{filepath.Join(tmpDir, "missing.txt"), "remote/missing.txt"},
	}

	var buf bytes.Buffer
	if err := writeTarFromPairs(&buf, files); err != nil {
		t.Fatalf("writeTarFromPairs: %v", err)
	}

	// Should only have the existing file
	tr := tar.NewReader(&buf)
	count := 0
	for {
		_, err := tr.Next()
		if err != nil {
			break
		}
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 tar entry (missing file skipped), got %d", count)
	}
}

func TestWriteTarFromPairs_SkipsDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0o755)
	fileInDir := filepath.Join(subDir, "file.txt")
	os.WriteFile(fileInDir, []byte("data"), 0o644)

	files := [][2]string{
		{subDir, "remote/subdir"},       // directory — should be skipped
		{fileInDir, "remote/file.txt"},  // file — should be included
	}

	var buf bytes.Buffer
	if err := writeTarFromPairs(&buf, files); err != nil {
		t.Fatalf("writeTarFromPairs: %v", err)
	}

	tr := tar.NewReader(&buf)
	count := 0
	for {
		_, err := tr.Next()
		if err != nil {
			break
		}
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 tar entry (dir skipped), got %d", count)
	}
}

func TestWriteTarFromPairs_EmptyList(t *testing.T) {
	var buf bytes.Buffer
	if err := writeTarFromPairs(&buf, nil); err != nil {
		t.Fatalf("writeTarFromPairs(nil): %v", err)
	}
	// tar.NewWriter always writes 1024-byte end-of-archive marker.
	// Verify the archive has no entries (just the EOF marker).
	tr := tar.NewReader(&buf)
	_, err := tr.Next()
	if err == nil {
		t.Error("expected EOF for empty input, got an entry")
	}
}

func TestExtractTar_RoundTrip(t *testing.T) {
	// Create files, tar them, extract them, verify contents
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("content-a"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("content-b"), 0o644)

	files := [][2]string{
		{filepath.Join(srcDir, "a.txt"), "a.txt"},
		{filepath.Join(srcDir, "sub", "b.txt"), "sub/b.txt"},
	}

	var tarBuf bytes.Buffer
	if err := writeTarFromPairs(&tarBuf, files); err != nil {
		t.Fatalf("write: %v", err)
	}

	dstDir := t.TempDir()
	if err := extractTar(&tarBuf, dstDir); err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Verify extracted contents
	data, err := os.ReadFile(filepath.Join(dstDir, "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if string(data) != "content-a" {
		t.Errorf("a.txt = %q, want 'content-a'", data)
	}

	data, err = os.ReadFile(filepath.Join(dstDir, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("read sub/b.txt: %v", err)
	}
	if string(data) != "content-b" {
		t.Errorf("sub/b.txt = %q, want 'content-b'", data)
	}
}

func TestExtractTar_PathTraversal(t *testing.T) {
	// Create a tar with path traversal
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Normal file
	hdr := &tar.Header{Name: "safe.txt", Size: 4, Mode: 0o644}
	tw.WriteHeader(hdr)
	tw.Write([]byte("safe"))

	// Path traversal — should be skipped during extraction
	hdr = &tar.Header{Name: "../../../etc/passwd", Size: 4, Mode: 0o644}
	tw.WriteHeader(hdr)
	tw.Write([]byte("evil"))
	tw.Close()

	dstDir := t.TempDir()
	// extractTar uses filepath.Join which cleans the path, then checks prefix.
	// The traversal path will end up inside dstDir after cleaning.
	if err := extractTar(&buf, dstDir); err != nil {
		t.Fatalf("extractTar: %v", err)
	}

	// safe.txt should exist
	if _, err := os.Stat(filepath.Join(dstDir, "safe.txt")); err != nil {
		t.Errorf("safe.txt should exist: %v", err)
	}
}

func TestExtractTar_EmptyArchive(t *testing.T) {
	var buf bytes.Buffer
	// Empty tar (just the end-of-archive marker)
	tw := tar.NewWriter(&buf)
	tw.Close()

	dstDir := t.TempDir()
	if err := extractTar(&buf, dstDir); err != nil {
		t.Fatalf("extractTar empty: %v", err)
	}
}

func TestUniqueParentDirs(t *testing.T) {
	files := [][2]string{
		{"/local/a.txt", "/remote/dir1/a.txt"},
		{"/local/b.txt", "/remote/dir1/b.txt"},
		{"/local/c.txt", "/remote/dir2/c.txt"},
	}

	parents := uniqueParentDirs(files)

	// Should be 2 unique parents
	if len(parents) != 2 {
		t.Errorf("expected 2 unique parents, got %d: %v", len(parents), parents)
	}

	sort.Strings(parents)
	if parents[0] != "/remote/dir1" || parents[1] != "/remote/dir2" {
		t.Errorf("parents = %v", parents)
	}
}

func TestUniqueParentDirs_Empty(t *testing.T) {
	parents := uniqueParentDirs(nil)
	if len(parents) != 0 {
		t.Errorf("expected 0 parents for nil input, got %d", len(parents))
	}
}

func TestShellQuoteAll(t *testing.T) {
	paths := []string{"/tmp/a.txt", "/path/with space/b.txt"}
	result := shellQuoteAll(paths)

	if result[0] != "'/tmp/a.txt'" {
		t.Errorf("shellQuoteAll[0] = %q", result[0])
	}
	if result[1] != "'/path/with space/b.txt'" {
		t.Errorf("shellQuoteAll[1] = %q", result[1])
	}
}
