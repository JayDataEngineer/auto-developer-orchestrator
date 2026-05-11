package orchestration

import (
	"context"
	"fmt"
	"strings"
)

// ChangeSet represents file changes detected between snapshot and current state.
type ChangeSet struct {
	Files   []string `json:"files"`
	Summary string   `json:"summary"`
	Diff    string   `json:"diff"`
}

// HasChanges returns true if the changeset contains any modifications.
func (cs *ChangeSet) HasChanges() bool {
	return len(cs.Files) > 0
}

// Snapshotter captures and restores filesystem state for subagent change tracking.
// The git-based implementation respects .gitignore automatically.
type Snapshotter interface {
	// Snapshot captures the current working tree state and returns an identifier.
	Snapshot(ctx context.Context, projectDir string) (string, error)
	// Diff returns changes since the given snapshot.
	Diff(ctx context.Context, projectDir, snapshotID string) (*ChangeSet, error)
	// Revert restores the filesystem to the given snapshot state.
	Revert(ctx context.Context, projectDir, snapshotID string) error
}

// GitSnapshotter uses git to track changes. Respects .gitignore by default
// because git only tracks files it knows about.
type GitSnapshotter struct {
	bashExec bashExecutor
}

// NewGitSnapshotter creates a git-based snapshotter.
func NewGitSnapshotter(bashExec bashExecutor) *GitSnapshotter {
	return &GitSnapshotter{bashExec: bashExec}
}

// Snapshot creates a git stash commit object and returns its SHA.
// If the working tree is clean (matches HEAD), returns HEAD's SHA.
func (g *GitSnapshotter) Snapshot(ctx context.Context, projectDir string) (string, error) {
	// git stash create returns a commit SHA without saving it to the stash list.
	// Returns empty string if the working tree matches HEAD.
	output, err := g.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git stash create", projectDir))
	if err != nil {
		return "", fmt.Errorf("git stash create: %w", err)
	}
	sha := strings.TrimSpace(output)
	if sha != "" {
		return sha, nil
	}

	// No changes to stash — working tree matches HEAD. Use HEAD as the baseline.
	headOutput, err := g.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git rev-parse HEAD", projectDir))
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(headOutput), nil
}

// Diff returns the file changes between the snapshot and current working tree.
func (g *GitSnapshotter) Diff(ctx context.Context, projectDir, snapshotID string) (*ChangeSet, error) {
	// Get changed file names
	filesOutput, err := g.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git diff %s --name-only", projectDir, snapshotID))
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only: %w", err)
	}
	if strings.TrimSpace(filesOutput) == "" {
		return &ChangeSet{Files: []string{}, Summary: "no changes", Diff: ""}, nil
	}

	files := strings.Split(strings.TrimSpace(filesOutput), "\n")

	// Get stat summary (last line has "N files changed, +M -K")
	statOutput, err := g.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git diff %s --stat", projectDir, snapshotID))
	if err != nil {
		return nil, fmt.Errorf("git diff --stat: %w", err)
	}
	summary := "files changed"
	if lines := strings.Split(strings.TrimSpace(statOutput), "\n"); len(lines) > 0 {
		summary = lines[len(lines)-1]
	}

	// Get full diff (truncated for LLM context)
	diffOutput, err := g.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git diff %s", projectDir, snapshotID))
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	diff := diffOutput
	if len(diff) > 8000 {
		// Keep the tail where error-relevant changes typically appear
		diff = diff[len(diff)-8000:]
		// Find the next diff header to avoid cutting mid-hunk
		if idx := strings.Index(diff, "\ndiff --git"); idx >= 0 {
			diff = diff[idx+1:]
		}
	}

	return &ChangeSet{
		Files:   files,
		Summary: summary,
		Diff:    diff,
	}, nil
}

// Revert restores all files to the snapshot state using git checkout.
func (g *GitSnapshotter) Revert(ctx context.Context, projectDir, snapshotID string) error {
	_, err := g.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git checkout %s -- .", projectDir, snapshotID))
	if err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	// Also clean any untracked files created since the snapshot
	_, _ = g.bashExec.Exec(ctx, fmt.Sprintf("cd %s && git clean -fd", projectDir))
	return nil
}
