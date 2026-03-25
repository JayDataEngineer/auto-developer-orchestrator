package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
)

// GitOps provides hybrid Git operations using go-git for reads and CLI for writes
type GitOps struct {
	logger *zap.Logger
	mu     sync.Mutex
}

// NewGitOps creates a new GitOps instance
func NewGitOps(logger *zap.Logger) *GitOps {
	return &GitOps{
		logger: logger,
	}
}

// CloneOptions represents options for cloning a repository
type CloneOptions struct {
	URL      string
	Dir      string
	Depth    int
	Branch   string
	Progress bool
}

// Clone clones a repository using git CLI for performance
func (g *GitOps) Clone(ctx context.Context, opts CloneOptions) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	args := []string{"clone"}

	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", opts.Depth))
	}

	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
	}

	args = append(args, opts.URL, opts.Dir)

	g.logger.Info("Cloning repository",
		zap.String("url", opts.URL),
		zap.String("dir", opts.Dir),
		zap.Int("depth", opts.Depth))

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w, output: %s", err, string(output))
	}

	g.logger.Info("Repository cloned successfully", zap.String("dir", opts.Dir))
	return nil
}

// StatusOptions represents options for git status
type StatusOptions struct {
	Dir string
}

// StatusResult represents the result of git status
type StatusResult struct {
	IsClean  bool
	Branch   string
	Modified []string
	Added    []string
	Deleted  []string
}

// Status returns the git status using go-git for safety
func (g *GitOps) Status(ctx context.Context, opts StatusOptions) (*StatusResult, error) {
	repo, err := git.PlainOpen(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	result := &StatusResult{
		IsClean:  status.IsClean(),
		Branch:   head.Name().Short(),
		Modified: []string{},
		Added:    []string{},
		Deleted:  []string{},
	}

	for path, s := range status {
		switch s.Worktree {
		case git.Modified:
			result.Modified = append(result.Modified, path)
		case git.Added:
			result.Added = append(result.Added, path)
		case git.Deleted:
			result.Deleted = append(result.Deleted, path)
		}
	}

	return result, nil
}

// CommitOptions represents options for committing
type CommitOptions struct {
	Dir     string
	Message string
	Author  string
	Email   string
}

// Commit commits changes using git CLI
func (g *GitOps) Commit(ctx context.Context, opts CommitOptions) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Stage all changes
	if err := g.runGitCmd(ctx, opts.Dir, "add", "-A"); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	// Commit
	args := []string{"commit", "-m", opts.Message}
	if opts.Author != "" {
		args = append(args, "--author", fmt.Sprintf("%s <%s>", opts.Author, opts.Email))
	}

	if err := g.runGitCmd(ctx, opts.Dir, args...); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	g.logger.Info("Changes committed",
		zap.String("dir", opts.Dir),
		zap.String("message", opts.Message))

	return nil
}

// PushOptions represents options for pushing
type PushOptions struct {
	Dir      string
	Remote   string
	Branch   string
	Force    bool
	Progress bool
}

// Push pushes changes using git CLI
func (g *GitOps) Push(ctx context.Context, opts PushOptions) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	args := []string{"push"}

	if opts.Force {
		args = append(args, "--force")
	}

	if opts.Remote != "" {
		args = append(args, opts.Remote)
	} else {
		args = append(args, "origin")
	}

	if opts.Branch != "" {
		args = append(args, opts.Branch)
	}

	g.logger.Info("Pushing changes",
		zap.String("dir", opts.Dir),
		zap.String("remote", opts.Remote),
		zap.String("branch", opts.Branch))

	if err := g.runGitCmd(ctx, opts.Dir, args...); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	g.logger.Info("Changes pushed successfully", zap.String("dir", opts.Dir))
	return nil
}

// PullOptions represents options for pulling
type PullOptions struct {
	Dir      string
	Remote   string
	Branch   string
	Rebase   bool
	Progress bool
}

// Pull pulls changes using git CLI
func (g *GitOps) Pull(ctx context.Context, opts PullOptions) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	args := []string{"pull"}

	if opts.Rebase {
		args = append(args, "--rebase")
	}

	if opts.Remote != "" {
		args = append(args, opts.Remote)
	} else {
		args = append(args, "origin")
	}

	if opts.Branch != "" {
		args = append(args, opts.Branch)
	}

	g.logger.Info("Pulling changes",
		zap.String("dir", opts.Dir),
		zap.String("remote", opts.Remote),
		zap.String("branch", opts.Branch))

	if err := g.runGitCmd(ctx, opts.Dir, args...); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}

	g.logger.Info("Changes pulled successfully", zap.String("dir", opts.Dir))
	return nil
}

// CheckoutOptions represents options for checking out a branch
type CheckoutOptions struct {
	Dir       string
	Branch    string
	CreateNew bool
	Force     bool
}

// Checkout checks out a branch using git CLI
func (g *GitOps) Checkout(ctx context.Context, opts CheckoutOptions) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	args := []string{"checkout"}

	if opts.Force {
		args = append(args, "--force")
	}

	if opts.CreateNew {
		args = append(args, "-b")
	} else {
		args = append(args, "-B")
	}

	args = append(args, opts.Branch)

	g.logger.Info("Checking out branch",
		zap.String("dir", opts.Dir),
		zap.String("branch", opts.Branch),
		zap.Bool("create_new", opts.CreateNew))

	if err := g.runGitCmd(ctx, opts.Dir, args...); err != nil {
		return fmt.Errorf("failed to checkout: %w", err)
	}

	g.logger.Info("Branch checked out successfully", zap.String("dir", opts.Dir))
	return nil
}

// CreateWorktree creates a git worktree for isolated agent operations
func (g *GitOps) CreateWorktree(ctx context.Context, repoDir, worktreeDir, branch string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.logger.Info("Creating worktree",
		zap.String("repo", repoDir),
		zap.String("worktree", worktreeDir),
		zap.String("branch", branch))

	// Remove existing worktree if it exists
	if _, err := os.Stat(worktreeDir); err == nil {
		if err := g.runGitCmd(ctx, repoDir, "worktree", "remove", "-f", worktreeDir); err != nil {
			g.logger.Warn("Failed to remove existing worktree", zap.Error(err))
		}
	}

	// Create new worktree
	if err := g.runGitCmd(ctx, repoDir, "worktree", "add", "-b", branch, worktreeDir); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	g.logger.Info("Worktree created successfully", zap.String("worktree", worktreeDir))
	return nil
}

// RemoveWorktree removes a git worktree
func (g *GitOps) RemoveWorktree(ctx context.Context, repoDir, worktreeDir string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.logger.Info("Removing worktree",
		zap.String("repo", repoDir),
		zap.String("worktree", worktreeDir))

	if err := g.runGitCmd(ctx, repoDir, "worktree", "remove", "-f", worktreeDir); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	g.logger.Info("Worktree removed successfully", zap.String("worktree", worktreeDir))
	return nil
}

// GetLogOptions represents options for git log
type GetLogOptions struct {
	Dir   string
	Count int
}

// LogEntry represents a git log entry
type LogEntry struct {
	Hash      string
	ShortHash string
	Author    string
	Email     string
	Date      time.Time
	Message   string
}

// GetLog returns the git log using go-git
func (g *GitOps) GetLog(ctx context.Context, opts GetLogOptions) ([]LogEntry, error) {
	repo, err := git.PlainOpen(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository: %w", err)
	}

	ref, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("failed to get HEAD: %w", err)
	}

	logIter, err := repo.Log(&git.LogOptions{
		From: ref.Hash(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get log: %w", err)
	}

	entries := []LogEntry{}
	count := 0
	err = logIter.ForEach(func(commit *object.Commit) error {
		if opts.Count > 0 && count >= opts.Count {
			return errors.New("limit reached")
		}
		entries = append(entries, LogEntry{
			Hash:      commit.Hash.String(),
			ShortHash: commit.Hash.String()[:7],
			Author:    commit.Author.Name,
			Email:     commit.Author.Email,
			Date:      commit.Author.When,
			Message:   commit.Message,
		})
		count++
		return nil
	})

	if err != nil && err.Error() != "limit reached" {
		return nil, fmt.Errorf("failed to iterate log: %w", err)
	}

	return entries, nil
}

// GetCurrentBranch returns the current branch name
func (g *GitOps) GetCurrentBranch(ctx context.Context, dir string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	return head.Name().Short(), nil
}

// runGitCmd runs a git command using CLI
func (g *GitOps) runGitCmd(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v failed: %w, output: %s", args, err, string(output))
	}

	return nil
}

// SanitizeInput sanitizes git input to prevent injection attacks
func SanitizeInput(input string) string {
	// Remove any shell metacharacters
	badChars := []string{";", "|", "&", "$", "`", "(", ")", "{", "}", "[", "]", "<", ">", "!", "\\", "\n", "\r"}
	for _, char := range badChars {
		input = strings.ReplaceAll(input, char, "")
	}

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Limit length
	if len(input) > 256 {
		input = input[:256]
	}

	return input
}

// IsGitRepository checks if a directory is a git repository
func IsGitRepository(dir string) bool {
	_, err := git.PlainOpen(dir)
	return err == nil
}

// InitRepository initializes a git repository
func (g *GitOps) InitRepository(ctx context.Context, dir string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	_, err := git.PlainInit(dir, false)
	if err != nil {
		return fmt.Errorf("failed to init repository: %w", err)
	}

	g.logger.Info("Repository initialized", zap.String("dir", dir))
	return nil
}

// GetRemoteURL returns the remote URL
func (g *GitOps) GetRemoteURL(ctx context.Context, dir string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("failed to open repository: %w", err)
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("failed to get remote: %w", err)
	}

	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("no remote URLs found")
	}

	return urls[0], nil
}

// ResolvePath resolves a project path safely
func ResolvePath(baseDir, projectName string) (string, error) {
	// Clean the project name to prevent directory traversal
	projectName = filepath.Clean(projectName)
	projectName = filepath.Base(projectName)

	// Construct the full path
	fullPath := filepath.Join(baseDir, projectName)

	// Ensure the path is within baseDir
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(absPath, absBase) {
		return "", fmt.Errorf("invalid project path: %s", projectName)
	}

	return absPath, nil
}
