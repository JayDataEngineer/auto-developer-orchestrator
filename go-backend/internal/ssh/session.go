package ssh

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"go.uber.org/zap"
)

// SessionManager manages SSH client connections.
type SessionManager struct {
	mu       sync.Map // key: "user@host:port" → *ssh.Client
	sessions sync.Map // key: sessionKey → sessionKey (maps to same "user@host:port")
	log      *zap.Logger
}

// NewSessionManager creates a new SSH session manager.
func NewSessionManager(log *zap.Logger) *SessionManager {
	return &SessionManager{log: log}
}

// Connect establishes an SSH connection and returns a session key.
// Auth priority: (1) provided keyData, (2) SSH agent ($SSH_AUTH_SOCK),
// (3) auto-loaded ~/.ssh/id_* keys, (4) provided password.
func (sm *SessionManager) Connect(user, host, port, password, keyData string) (string, error) {
	if port == "" {
		port = "22"
	}
	addr := net.JoinHostPort(host, port)
	clientKey := fmt.Sprintf("%s@%s", user, addr)

	var authMethods []ssh.AuthMethod

	// 1. Explicit key auth
	if keyData != "" {
		signer, err := ssh.ParsePrivateKey([]byte(keyData))
		if err != nil {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(keyData), []byte(password))
			if err != nil {
				sm.log.Warn("Failed to parse SSH key, falling back", zap.Error(err))
			} else {
				authMethods = append(authMethods, ssh.PublicKeys(signer))
			}
		} else {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// 2. SSH agent auth
	if agentAuth := sm.tryAgentAuth(); agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
	}

	// 3. Auto-load keys from ~/.ssh/
	for _, signer := range sm.loadSSHKeys() {
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// 4. Password auth
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	if len(authMethods) == 0 {
		return "", fmt.Errorf("no authentication method available (no keyData, no SSH agent, no ~/.ssh keys, no password)")
	}

	// Host key callback — check known_hosts, auto-accept for first connection
	hostKeyCallback, err := sm.hostKeyCallback(host, port)
	if err != nil {
		return "", fmt.Errorf("host key setup failed: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("SSH dial failed: %w", err)
	}

	// Generate session key
	sessionKey := generateSessionKey()

	// Close existing connection if any
	if existing, loaded := sm.mu.LoadOrStore(clientKey, client); loaded {
		if oldClient, ok := existing.(*ssh.Client); ok {
			oldClient.Close()
		}
		sm.mu.Store(clientKey, client)
	}

	sm.sessions.Store(sessionKey, clientKey)
	sm.log.Info("SSH session connected", zap.String("key", clientKey), zap.String("session", sessionKey))

	return sessionKey, nil
}

// GetClient returns the SSH client for a session key.
func (sm *SessionManager) GetClient(sessionKey string) (*ssh.Client, error) {
	clientKeyVal, ok := sm.sessions.Load(sessionKey)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionKey)
	}
	clientKey := clientKeyVal.(string)

	clientVal, ok := sm.mu.Load(clientKey)
	if !ok {
		return nil, fmt.Errorf("SSH connection not found for: %s", clientKey)
	}
	client, ok := clientVal.(*ssh.Client)
	if !ok {
		return nil, fmt.Errorf("invalid SSH client for: %s", clientKey)
	}
	return client, nil
}

// Disconnect closes an SSH session.
func (sm *SessionManager) Disconnect(sessionKey string) error {
	clientKeyVal, ok := sm.sessions.LoadAndDelete(sessionKey)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionKey)
	}
	clientKey := clientKeyVal.(string)

	if clientVal, loaded := sm.mu.LoadAndDelete(clientKey); loaded {
		if client, ok := clientVal.(*ssh.Client); ok {
			client.Close()
			sm.log.Info("SSH session disconnected", zap.String("key", clientKey))
		}
	}
	return nil
}

// CloseAll closes all SSH connections.
func (sm *SessionManager) CloseAll() {
	sm.mu.Range(func(key, value any) bool {
		if client, ok := value.(*ssh.Client); ok {
			client.Close()
		}
		sm.mu.Delete(key)
		return true
	})
	sm.sessions.Range(func(key, _ any) bool {
		sm.sessions.Delete(key)
		return true
	})
}

// GetHostFingerprint returns the host key fingerprint for a host.
func (sm *SessionManager) GetHostFingerprint(host, port string) (string, error) {
	if port == "" {
		port = "22"
	}
	addr := net.JoinHostPort(host, port)

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("cannot connect to %s: %w", addr, err)
	}
	defer conn.Close()

	var capturedKey ssh.PublicKey
	sshConn, _, _, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			capturedKey = key
			return nil
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return "", fmt.Errorf("SSH handshake failed: %w", err)
	}
	sshConn.Close()

	if capturedKey == nil {
		return "", fmt.Errorf("failed to retrieve host key")
	}
	return ssh.FingerprintSHA256(capturedKey), nil
}

// hostKeyCallback returns a callback that checks known_hosts or auto-stores new keys.
func (sm *SessionManager) hostKeyCallback(host, port string) (ssh.HostKeyCallback, error) {
	knownHostsPath := sm.knownHostsPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0700); err != nil {
		sm.log.Warn("Cannot create known_hosts dir, using insecure mode", zap.Error(err))
		return ssh.InsecureIgnoreHostKey(), nil
	}

	// If known_hosts exists, use it for verification
	if _, err := os.Stat(knownHostsPath); err == nil {
		if cb, err := knownhosts.New(knownHostsPath); err == nil {
			return cb, nil
		}
		// Fall through to auto-accept if parsing fails
	}

	// Create the file if it doesn't exist
	if err := os.WriteFile(knownHostsPath, []byte{}, 0600); err != nil {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	// Return a callback that auto-accepts and stores new keys
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// For now, auto-accept. In production, this should prompt the user.
		// We store the key for future verification.
		line := fmt.Sprintf("%s %s %s\n",
			hostname,
			key.Type(),
			string(ssh.MarshalAuthorizedKey(key)),
		)

		f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil // Accept anyway
		}
		defer f.Close()
		f.WriteString(line)

		sm.log.Info("SSH host key accepted and stored",
			zap.String("host", hostname),
			zap.String("fingerprint", ssh.FingerprintSHA256(key)),
		)
		return nil
	}, nil
}

func (sm *SessionManager) knownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/pux_ssh_known_hosts"
	}
	return filepath.Join(home, ".pux", "ssh", "known_hosts")
}

func generateSessionKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// tryAgentAuth attempts to use the SSH agent ($SSH_AUTH_SOCK).
func (sm *SessionManager) tryAgentAuth() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		sm.log.Debug("SSH agent not reachable", zap.String("socket", sock), zap.Error(err))
		return nil
	}
	ag := agent.NewClient(conn)
	signers, err := ag.Signers()
	if err != nil || len(signers) == 0 {
		conn.Close()
		return nil
	}
	sm.log.Debug("SSH agent auth available", zap.Int("signers", len(signers)))
	return ssh.PublicKeysCallback(ag.Signers)
}

// loadSSHKeys loads private keys from ~/.ssh/id_* (no extension or .pub only).
func (sm *SessionManager) loadSSHKeys() []ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	sshDir := filepath.Join(home, ".ssh")

	entries, err := os.ReadDir(sshDir)
	if err != nil {
		return nil
	}

	var signers []ssh.Signer
	for _, entry := range entries {
		name := entry.Name()
		// Only load id_* files that look like private keys
		if len(name) < 4 || name[:3] != "id_" {
			continue
		}
		// Skip .pub files
		if len(name) > 4 && name[len(name)-4:] == ".pub" {
			continue
		}
		// Skip known non-key files
		if name == "id_ed25519_sk" || name == "id_rsa_ssh2" {
			continue
		}

		keyPath := filepath.Join(sshDir, name)
		data, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}

		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
		sm.log.Debug("Auto-loaded SSH key", zap.String("key", name))
	}
	return signers
}
