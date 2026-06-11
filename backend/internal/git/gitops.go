package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/go-git/go-git/v5"
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

	if err := g.runGitCmd(ctx, opts.Dir, "add", "-A"); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

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
