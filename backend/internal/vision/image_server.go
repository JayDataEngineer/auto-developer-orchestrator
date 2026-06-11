package vision

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ImageServer serves temp image files for MCP vision tools.
// Writes base64 images to disk and serves them via HTTP so the
// remote MCP server can fetch them by URL instead of receiving
// a data URI (which blows past URL length limits).
type ImageServer struct {
	dir     string
	baseURL string // e.g. "http://100.99.57.110:3847"
	mu      sync.Mutex
	seq     atomic.Uint64
}

// NewImageServer creates a temp image file server.
// baseURL is the externally-reachable URL of this Go backend (e.g. http://<tailscale-ip>:3847).
func NewImageServer(baseURL string, logger *log.Logger) *ImageServer {
	dir := filepath.Join(os.TempDir(), "pux-vision")
	if err := os.MkdirAll(dir, 0755); err != nil {
		if logger != nil {
			logger.Printf("vision: warning: could not create temp dir %s: %v", dir, err)
		}
	}

	// Clean up any stale files from previous runs
	if logger != nil {
		logger.Printf("vision: image server ready, dir=%s, baseURL=%s", dir, baseURL)
	}

	return &ImageServer{
		dir:     dir,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// Save writes a base64 image to a temp file and returns the HTTP URL.
// The caller should use this URL as the imageSource for MCP tools.
func (s *ImageServer) Save(b64 string, mimeType string) (string, error) {
	ext := ".png"
	if strings.Contains(mimeType, "jpeg") || strings.Contains(mimeType, "jpg") {
		ext = ".jpg"
	}

	id := s.seq.Add(1)
	filename := fmt.Sprintf("img-%d-%d%s", time.Now().UnixMilli(), id, ext)
	path := filepath.Join(s.dir, filename)

	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return s.baseURL + "/api/vision/tmp/" + filename, nil
}

// SetBaseURL updates the externally-reachable base URL (called after Tailscale IP is known).
func (s *ImageServer) SetBaseURL(url string) {
	s.baseURL = strings.TrimRight(url, "/")
}

// CleanupOld removes temp files older than maxAge.
func (s *ImageServer) CleanupOld(maxAge time.Duration) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

// ServeHTTP serves temp image files. Mount at /api/vision/tmp/.
func (s *ImageServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// chi strips the prefix, so we get just the filename
	filename := filepath.Base(r.URL.Path)
	if filename == "." || filename == "/" {
		http.Error(w, "filename required", http.StatusBadRequest)
		return
	}

	path := filepath.Join(s.dir, filename)
	// Security: ensure we don't escape the temp dir
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(s.dir)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	if _, err := os.Stat(path); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Set content type based on extension
	if strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg") {
		w.Header().Set("Content-Type", "image/jpeg")
	} else {
		w.Header().Set("Content-Type", "image/png")
	}
	w.Header().Set("Cache-Control", "no-cache")

	http.ServeFile(w, r, path)
}
