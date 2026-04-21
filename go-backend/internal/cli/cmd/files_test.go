package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFilesList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/sandbox/test-repo/files/list") {
			t.Errorf("expected files/list path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "main.go", "is_dir": false, "size": 1024.0},
			{"name": "src", "is_dir": true, "size": 0},
		})
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "files", "list", "test-repo")
	if err != nil {
		t.Fatalf("files list failed: %v", err)
	}
	if !strings.Contains(stdout, "main.go") {
		t.Errorf("expected main.go in output, got: %s", stdout)
	}
}

func TestFilesUpload(t *testing.T) {
	// Create a temp file to upload
	tmpFile, err := os.CreateTemp("", "upload-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("test content")
	tmpFile.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["path"] != "/remote/file.txt" {
			t.Errorf("expected path=/remote/file.txt, got %v", body)
		}
		if body["encoding"] != "base64" {
			t.Errorf("expected encoding=base64, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	stdout, _, err := runCommand(t, srv.URL, "files", "upload", "test-repo",
		"--src", tmpFile.Name(), "--dst", "/remote/file.txt")
	if err != nil {
		t.Fatalf("files upload failed: %v", err)
	}
	if !strings.Contains(stdout, "Uploaded") {
		t.Errorf("expected upload message, got: %s", stdout)
	}
}

func TestFilesUploadMissingFlags(t *testing.T) {
	_, _, err := runCommand(t, "http://unused:9999", "files", "upload", "test-repo")
	if err == nil {
		t.Fatal("expected error when --src --dst missing")
	}
}

func TestFilesDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "path=/remote/file.txt") {
			t.Errorf("expected path query param, got %s", r.URL.RawQuery)
		}
		if !strings.Contains(r.URL.RawQuery, "format=raw") {
			t.Errorf("expected format=raw query param, got %s", r.URL.RawQuery)
		}
		w.Write([]byte("file contents here"))
	}))
	defer srv.Close()

	// Download to a temp file
	tmpFile, err := os.CreateTemp("", "download-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	stdout, _, err := runCommand(t, srv.URL, "files", "download", "test-repo",
		"--path", "/remote/file.txt", "--out", tmpFile.Name())
	if err != nil {
		t.Fatalf("files download failed: %v", err)
	}
	if !strings.Contains(stdout, "Downloaded") {
		t.Errorf("expected download message, got: %s", stdout)
	}
}
