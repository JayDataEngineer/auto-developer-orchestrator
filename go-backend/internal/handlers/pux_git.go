package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FileDiff represents a single file's git diff.
type FileDiff struct {
	Path     string `json:"path"`
	Status   string `json:"status"`   // M=modified, A=added, D=deleted, ?=untracked
	Original string `json:"original"` // content from HEAD (empty for new files)
	Modified string `json:"modified"` // content on disk (empty for deleted files)
}

// GetGitDiff handles GET /api/pux/git/diff — returns changed files with original + modified content.
func (h *PuxHandler) GetGitDiff(w http.ResponseWriter, r *http.Request) {
	projectPath := requireProject(w, r, h.db)
	if projectPath == "" {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get list of changed files: tracked (diff HEAD) + untracked
	changedFiles, err := getChangedFiles(ctx, projectPath)
	if err != nil {
		writeJSON(w, http.StatusOK, []FileDiff{})
		return
	}

	if len(changedFiles) == 0 {
		writeJSON(w, http.StatusOK, []FileDiff{})
		return
	}

	// Build diffs: original from git HEAD, modified from disk
	var diffs []FileDiff
	for _, f := range changedFiles {
		diff := FileDiff{Path: f.path, Status: f.status}

		// Original: from HEAD (skip for new/untracked files)
		if f.status != "A" && f.status != "?" {
			orig, err := gitShow(ctx, projectPath, f.path)
			if err == nil {
				diff.Original = orig
			}
		}

		// Modified: from disk (skip for deleted files)
		if f.status != "D" {
			absPath := filepath.Join(projectPath, f.path)
			data, err := os.ReadFile(absPath)
			if err == nil && int64(len(data)) < 1_000_000 { // skip files >1MB
				diff.Modified = string(data)
			}
		}

		diffs = append(diffs, diff)
	}

	writeJSON(w, http.StatusOK, diffs)
}

type changedFile struct {
	path   string
	status string
}

// getChangedFiles returns all files that differ from HEAD (tracked + untracked).
func getChangedFiles(ctx context.Context, dir string) ([]changedFile, error) {
	// Tracked changes vs HEAD
	out, err := runGit(ctx, dir, "diff", "HEAD", "--name-status")
	if err != nil {
		// Might be a fresh repo with no commits — try --cached
		out, err = runGit(ctx, dir, "diff", "--cached", "--name-status")
		if err != nil {
			return nil, err
		}
	}

	var files []changedFile
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		status := strings.TrimSpace(parts[0])
		path := parts[1]
		// Rename entries have "Rxxx\told\tnew" format — skip the old path
		if strings.HasPrefix(status, "R") {
			// path is the old name, grab new name from next field
			renameParts := strings.Split(line, "\t")
			if len(renameParts) >= 3 {
				path = renameParts[2]
				status = "M" // treat renames as modified
			}
		}
		files = append(files, changedFile{path: path, status: status})
	}

	// Untracked files
	untracked, err := runGit(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err == nil {
		for _, line := range strings.Split(untracked, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			files = append(files, changedFile{path: line, status: "?"})
		}
	}

	return files, nil
}

// gitShow returns the content of a file at HEAD.
func gitShow(ctx context.Context, dir, path string) (string, error) {
	return runGit(ctx, dir, "show", "HEAD:"+path)
}

// runGit executes a git command and returns stdout.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %v: %w (%s)", args, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
