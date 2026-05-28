package adapters

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// SSHFileOps implements file.SandboxFileOps over an SSH connection.
type SSHFileOps struct {
	exec    *SSHExecutor
	baseDir string
}

func NewSSHFileOps(exec *SSHExecutor, baseDir string) *SSHFileOps {
	return &SSHFileOps{exec: exec, baseDir: baseDir}
}

func (s *SSHFileOps) absPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return path.Join(s.baseDir, p)
}

func (s *SSHFileOps) ReadFile(ctx context.Context, p string) (string, error) {
	out, err := s.exec.Exec(ctx, fmt.Sprintf("cat %s", shQuote(s.absPath(p))))
	return out, err
}

func (s *SSHFileOps) WriteFile(ctx context.Context, p string, content string, overwrite bool) (string, error) {
	fullPath := s.absPath(p)
	// Ensure parent directory exists
	s.exec.Exec(ctx, fmt.Sprintf("mkdir -p %s", shQuote(path.Dir(fullPath))))

	// Write via heredoc to handle special characters
	cmd := fmt.Sprintf("cat > %s <<'PUXEOF'\n%s\nPUXEOF", shQuote(fullPath), content)
	_, err := s.exec.Exec(ctx, cmd)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s", fullPath), nil
}

func (s *SSHFileOps) EditFile(ctx context.Context, p string, oldStr, newStr string, replaceAll bool) (string, error) {
	fullPath := s.absPath(p)
	// Use sed for edit. Escape single quotes in strings.
	oldEsc := strings.ReplaceAll(oldStr, "'", "'\\''")
	newEsc := strings.ReplaceAll(newStr, "'", "'\\''")
	sedFlag := ""
	if replaceAll {
		sedFlag = "g"
	}
	cmd := fmt.Sprintf("sed -i 's%s%s%s%s%s' %s", "'", oldEsc, newEsc, sedFlag, "'", shQuote(fullPath))
	out, err := s.exec.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("edit failed: %w\n%s", err, out)
	}
	return fmt.Sprintf("Edited %s", fullPath), nil
}

func (s *SSHFileOps) Grep(ctx context.Context, p string, pattern string) (string, error) {
	cmd := fmt.Sprintf("grep -rn %s %s 2>/dev/null || true", shQuote(pattern), shQuote(s.absPath(p)))
	return s.exec.Exec(ctx, cmd)
}

func (s *SSHFileOps) Glob(ctx context.Context, p string, pattern string) (string, error) {
	cmd := fmt.Sprintf("find %s -name %s -type f -maxdepth 6 2>/dev/null || true", shQuote(s.absPath(p)), shQuote(pattern))
	return s.exec.Exec(ctx, cmd)
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
