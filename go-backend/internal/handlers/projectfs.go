package handlers

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/env"
	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"github.com/pkg/sftp"
)

// ── Shared types ──

// FileNode represents a file or directory in the project tree.
type FileNode struct {
	Name     string     `json:"name"`
	Type     string     `json:"type"` // "file" or "dir"
	Path     string     `json:"path"`
	Children []FileNode `json:"children,omitempty"`
}

// skipNames lists directories/files to exclude from tree listings.
var skipNames = map[string]bool{
	"node_modules": true,
	".git":         true,
	"__pycache__":  true,
	".next":        true,
	"dist":         true,
	".cache":       true,
	"vendor":       true,
	".pux":         true,
	".DS_Store":    true,
}

// ProjectFS abstracts filesystem operations for either local or SSH-backed
// projects. All paths are relative to the project root. Implementations
// handle path cleaning and security boundary enforcement internally.
type ProjectFS interface {
	// Type returns "local" or "ssh" for frontend routing decisions.
	Type() string

	// Root returns the absolute root path (local path or SSH remote path).
	Root() string

	// SSHInfo returns non-nil if this is an SSH project.
	SSHInfo() *SSHProjectInfo

	// BuildTree returns a recursive file tree rooted at the project root.
	BuildTree(maxDepth int) ([]FileNode, error)

	// ReadFile reads a file's content relative to the project root.
	ReadFile(relPath string) ([]byte, error)

	// WriteFile writes content to a file relative to the project root.
	WriteFile(relPath string, content []byte) error

	// CreateFile creates an empty file relative to the project root.
	CreateFile(relPath string) error

	// MoveFile renames/moves a file within the project.
	MoveFile(fromRel, toRel string) error

	// DeleteFile removes a file relative to the project root.
	// Returns the trash path for undo, or empty string if unsupported.
	DeleteFile(relPath string) (trashPath string, err error)

	// Stat returns file info for a path relative to the project root.
	Stat(relPath string) (os.FileInfo, error)

	// MkdirAll creates directory hierarchy relative to the project root.
	MkdirAll(relPath string) error

	// Resolve joins relPath to root and returns the absolute path.
	// Also validates the path stays within project boundaries.
	Resolve(relPath string) (string, error)
}

// SSHProjectInfo carries the parsed SSH URL components.
type SSHProjectInfo struct {
	User string
	Host string
	Port string
	Path string // remote directory path
}

// ParseSSHURL parses "ssh://user@host[:port]/path" into components.
func ParseSSHURL(raw string) (info SSHProjectInfo, ok bool) {
	if !strings.HasPrefix(raw, "ssh://") {
		return info, false
	}
	rest := raw[6:] // strip "ssh://"
	user, afterAt, ok := strings.Cut(rest, "@")
	if !ok {
		return info, false
	}
	info.User = user
	rest = afterAt

	var hostPart string
	if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
		hostPart = rest[:slashIdx]
		info.Path = rest[slashIdx:]
	} else {
		hostPart = rest
	}

	if host, port, ok := strings.Cut(hostPart, ":"); ok {
		info.Host = host
		info.Port = port
	} else {
		info.Host = hostPart
		info.Port = "22"
	}

	if info.Path == "" {
		info.Path = "/"
	}

	return info, true
}

// ── LocalFS ──

// LocalFS implements ProjectFS using the local filesystem.
type LocalFS struct {
	root     string
	security *env.SecurityGuard
}

// NewLocalFS creates a local filesystem ProjectFS rooted at rootPath.
func NewLocalFS(rootPath string) *LocalFS {
	return &LocalFS{root: rootPath, security: env.NewSecurityGuard()}
}

func (fs *LocalFS) Type() string          { return "local" }
func (fs *LocalFS) Root() string          { return fs.root }
func (fs *LocalFS) SSHInfo() *SSHProjectInfo { return nil }

func (fs *LocalFS) Resolve(relPath string) (string, error) {
	absPath := filepath.Join(fs.root, relPath)
	absPath, err := filepath.Abs(absPath)
	if err != nil || !strings.HasPrefix(absPath, fs.root) {
		return "", fmt.Errorf("path escapes project directory")
	}
	return absPath, nil
}

func (fs *LocalFS) BuildTree(maxDepth int) ([]FileNode, error) {
	return buildLocalFileTree(fs.root, fs.root, maxDepth), nil
}

func (fs *LocalFS) ReadFile(relPath string) ([]byte, error) {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(absPath)
}

func (fs *LocalFS) WriteFile(relPath string, content []byte) error {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return err
	}
	if err := fs.security.CheckWritePath(absPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(absPath, content, 0o644)
}

func (fs *LocalFS) CreateFile(relPath string) error {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return err
	}
	if err := fs.security.CheckWritePath(absPath); err != nil {
		return err
	}
	if _, err := os.Stat(absPath); err == nil {
		return os.ErrExist
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(absPath, []byte{}, 0o644)
}

func (fs *LocalFS) MoveFile(fromRel, toRel string) error {
	from, err := fs.Resolve(fromRel)
	if err != nil {
		return err
	}
	to, err := fs.Resolve(toRel)
	if err != nil {
		return err
	}
	if err := fs.security.CheckWritePath(to); err != nil {
		return err
	}
	if _, err := os.Stat(from); os.IsNotExist(err) {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return os.Rename(from, to)
}

func (fs *LocalFS) DeleteFile(relPath string) (string, error) {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", os.ErrNotExist
	}

	trashDir := filepath.Join(fs.root, ".pux", "trash")
	os.MkdirAll(trashDir, 0o755)

	name := filepath.Base(absPath)
	trashPath := filepath.Join(trashDir, fmt.Sprintf("%d_%s", time.Now().UnixMilli(), name))

	if err := os.Rename(absPath, trashPath); err != nil {
		return "", err
	}
	return trashPath, nil
}

func (fs *LocalFS) Stat(relPath string) (os.FileInfo, error) {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return nil, err
	}
	return os.Stat(absPath)
}

func (fs *LocalFS) MkdirAll(relPath string) error {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return err
	}
	return os.MkdirAll(absPath, 0o755)
}

func buildLocalFileTree(root, currentPath string, maxDepth int) []FileNode {
	if maxDepth <= 0 {
		return nil
	}

	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil
	}

	var nodes []FileNode
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || skipNames[name] {
			continue
		}

		fullPath := filepath.Join(currentPath, name)
		relPath, _ := filepath.Rel(root, fullPath)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.IsDir() {
			children := buildLocalFileTree(root, fullPath, maxDepth-1)
			nodes = append(nodes, FileNode{
				Name:     name,
				Type:     "dir",
				Path:     relPath,
				Children: children,
			})
		} else {
			if info.Size() > 1_000_000 {
				continue
			}
			nodes = append(nodes, FileNode{
				Name: name,
				Type: "file",
				Path: relPath,
			})
		}
	}
	return nodes
}

// ── SshFS ──

// SshFS implements ProjectFS using SFTP over an SSH connection.
type SshFS struct {
	info     SSHProjectInfo
	sessions *puxssh.SessionManager
	security *env.SecurityGuard
}

// NewSshFS creates an SSH-backed ProjectFS.
func NewSshFS(info SSHProjectInfo, sessions *puxssh.SessionManager) *SshFS {
	return &SshFS{info: info, sessions: sessions, security: env.NewSecurityGuard()}
}

func (fs *SshFS) Type() string            { return "ssh" }
func (fs *SshFS) Root() string            { return fs.info.Path }
func (fs *SshFS) SSHInfo() *SSHProjectInfo { return &fs.info }

func (fs *SshFS) Resolve(relPath string) (string, error) {
	cleaned := path.Clean("/" + relPath)
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("path escapes project directory")
	}
	return cleaned, nil
}

// sftpClient opens a new SFTP sub-session on the shared SSH connection.
func (fs *SshFS) sftpClient() (*sftp.Client, error) {
	// Look up the client key for this host
	clientKey := fmt.Sprintf("%s@%s:%s", fs.info.User, fs.info.Host, fs.info.Port)

	clientVal, ok := fs.sessions.GetClientByKey(clientKey)
	if !ok {
		return nil, fmt.Errorf("SSH not connected to %s — open the project in the terminal first", clientKey)
	}
	return sftp.NewClient(clientVal)
}

func (fs *SshFS) BuildTree(maxDepth int) ([]FileNode, error) {
	client, err := fs.sftpClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return fs.buildTree(client, fs.info.Path, maxDepth), nil
}

func (fs *SshFS) buildTree(client *sftp.Client, currentPath string, maxDepth int) []FileNode {
	if maxDepth <= 0 {
		return nil
	}

	entries, err := client.ReadDir(currentPath)
	if err != nil {
		return nil
	}

	var nodes []FileNode
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || skipNames[name] {
			continue
		}

		fullPath := path.Join(currentPath, name)
		relPath := strings.TrimPrefix(fullPath, fs.info.Path)
		if relPath == "" {
			relPath = name
		} else {
			relPath = strings.TrimPrefix(relPath, "/")
		}

		if entry.IsDir() {
			children := fs.buildTree(client, fullPath, maxDepth-1)
			nodes = append(nodes, FileNode{
				Name:     name,
				Type:     "dir",
				Path:     relPath,
				Children: children,
			})
		} else {
			if entry.Size() > 1_000_000 {
				continue
			}
			nodes = append(nodes, FileNode{
				Name: name,
				Type: "file",
				Path: relPath,
			})
		}
	}
	return nodes
}

func (fs *SshFS) ReadFile(relPath string) ([]byte, error) {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return nil, err
	}

	client, err := fs.sftpClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	f, err := client.Open(path.Join(fs.info.Path, absPath))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (fs *SshFS) WriteFile(relPath string, content []byte) error {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return err
	}

	client, err := fs.sftpClient()
	if err != nil {
		return err
	}
	defer client.Close()

	fullPath := path.Join(fs.info.Path, absPath)
	if err := fs.security.CheckWritePath(fullPath); err != nil {
		return err
	}
	if err := client.MkdirAll(path.Dir(fullPath)); err != nil {
		return err
	}

	f, err := client.Create(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(content)
	return err
}

func (fs *SshFS) CreateFile(relPath string) error {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return err
	}

	client, err := fs.sftpClient()
	if err != nil {
		return err
	}
	defer client.Close()

	fullPath := path.Join(fs.info.Path, absPath)
	if err := fs.security.CheckWritePath(fullPath); err != nil {
		return err
	}
	if _, err := client.Stat(fullPath); err == nil {
		return os.ErrExist
	}
	if err := client.MkdirAll(path.Dir(fullPath)); err != nil {
		return err
	}
	f, err := client.Create(fullPath)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func (fs *SshFS) MoveFile(fromRel, toRel string) error {
	fromAbs, err := fs.Resolve(fromRel)
	if err != nil {
		return err
	}
	toAbs, err := fs.Resolve(toRel)
	if err != nil {
		return err
	}

	client, err := fs.sftpClient()
	if err != nil {
		return err
	}
	defer client.Close()

	fromPath := path.Join(fs.info.Path, fromAbs)
	toPath := path.Join(fs.info.Path, toAbs)
	if err := fs.security.CheckWritePath(toPath); err != nil {
		return err
	}

	if _, err := client.Stat(fromPath); err != nil {
		return os.ErrNotExist
	}
	if err := client.MkdirAll(path.Dir(toPath)); err != nil {
		return err
	}
	return client.Rename(fromPath, toPath)
}

func (fs *SshFS) DeleteFile(relPath string) (string, error) {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return "", err
	}

	client, err := fs.sftpClient()
	if err != nil {
		return "", err
	}
	defer client.Close()

	fullPath := path.Join(fs.info.Path, absPath)
	if _, err := client.Stat(fullPath); err != nil {
		return "", os.ErrNotExist
	}

	// No trash on remote — just remove
	return "", client.Remove(fullPath)
}

func (fs *SshFS) Stat(relPath string) (os.FileInfo, error) {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return nil, err
	}

	client, err := fs.sftpClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.Stat(path.Join(fs.info.Path, absPath))
}

func (fs *SshFS) MkdirAll(relPath string) error {
	absPath, err := fs.Resolve(relPath)
	if err != nil {
		return err
	}

	client, err := fs.sftpClient()
	if err != nil {
		return err
	}
	defer client.Close()

	return client.MkdirAll(path.Join(fs.info.Path, absPath))
}
