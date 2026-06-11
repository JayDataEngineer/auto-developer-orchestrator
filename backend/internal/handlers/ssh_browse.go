package handlers

import (
	"fmt"
	"net/http"
	"path"
	"strings"

	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"github.com/pkg/sftp"
	"go.uber.org/zap"
)

// SshBrowseHandler provides SSH remote filesystem browsing.
// Injected into PuxHandler via SetSSHManager.
type SshBrowseHandler struct {
	sessions *puxssh.SessionManager
	log      *zap.Logger
}

// NewSshBrowseHandler creates a new SSH browse handler.
func NewSshBrowseHandler(sessions *puxssh.SessionManager, log *zap.Logger) *SshBrowseHandler {
	return &SshBrowseHandler{sessions: sessions, log: log}
}

// SshConnect handles POST /api/pux/ssh/connect
func (h *SshBrowseHandler) SshConnect(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		User     string `json:"user"`
		Host     string `json:"host"`
		Port     string `json:"port"`
		Password string `json:"password"`
		KeyData  string `json:"keyData"`
	}](w, r)
	if !ok {
		return
	}

	if req.Host == "" {
		JSONError(w, "host is required", http.StatusBadRequest)
		return
	}
	if req.User == "" {
		JSONError(w, "user is required", http.StatusBadRequest)
		return
	}

	sessionKey, err := h.sessions.Connect(req.User, req.Host, req.Port, req.Password, req.KeyData)
	if err != nil {
		h.log.Warn("SSH connect failed", zap.String("host", req.Host), zap.Error(err))
		JSONError(w, fmt.Sprintf("Connection failed: %v", err), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"sessionKey": sessionKey,
		"host":       req.Host,
		"user":       req.User,
	})
}

// SshBrowse handles POST /api/pux/ssh/browse
func (h *SshBrowseHandler) SshBrowse(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		SessionKey string `json:"sessionKey"`
		Path       string `json:"path"`
	}](w, r)
	if !ok {
		return
	}

	if req.SessionKey == "" {
		JSONError(w, "sessionKey is required", http.StatusBadRequest)
		return
	}

	client, err := h.sessions.GetClient(req.SessionKey)
	if err != nil {
		JSONError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Default to home directory
	browsePath := req.Path
	if browsePath == "" {
		browsePath = "."
	}

	// Create SFTP client
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		JSONError(w, fmt.Sprintf("SFTP client failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer sftpClient.Close()

	// Clean and resolve path
	browsePath = path.Clean(browsePath)
	if browsePath == "." {
		if wd, err := sftpClient.Getwd(); err == nil {
			browsePath = wd
		} else {
			browsePath = "/"
		}
	}

	// Read directory
	entries, err := sftpClient.ReadDir(browsePath)
	if err != nil {
		JSONError(w, fmt.Sprintf("Cannot read directory: %v", err), http.StatusBadRequest)
		return
	}

	// Build file entries, filter and sort
	fe := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		fe = append(fe, fileEntry{Name: entry.Name(), IsDir: entry.IsDir(), Size: entry.Size()})
	}
	result := filterAndSortEntries(fe)

	// Compute parent
	parent := path.Dir(browsePath)
	if parent == browsePath {
		parent = ""
	}

	writeJSON(w, http.StatusOK, browseResponse{
		Path:    browsePath,
		Parent:  parent,
		Entries: result,
	})
}

// SshDisconnect handles POST /api/pux/ssh/disconnect
func (h *SshBrowseHandler) SshDisconnect(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		SessionKey string `json:"sessionKey"`
	}](w, r)
	if !ok {
		return
	}

	if req.SessionKey == "" {
		JSONError(w, "sessionKey is required", http.StatusBadRequest)
		return
	}

	if err := h.sessions.Disconnect(req.SessionKey); err != nil {
		JSONError(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// SshTrustHost handles POST /api/pux/ssh/trust-host
func (h *SshBrowseHandler) SshTrustHost(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Host string `json:"host"`
		Port string `json:"port"`
	}](w, r)
	if !ok {
		return
	}

	if req.Host == "" {
		JSONError(w, "host is required", http.StatusBadRequest)
		return
	}

	fingerprint, err := h.sessions.GetHostFingerprint(req.Host, req.Port)
	if err != nil {
		JSONError(w, fmt.Sprintf("Cannot get host key: %v", err), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"fingerprint": fingerprint,
	})
}

// SshMkdir handles POST /api/pux/ssh/mkdir — create directory on remote.
func (h *SshBrowseHandler) SshMkdir(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		SessionKey string `json:"sessionKey"`
		Path       string `json:"path"`
		Name       string `json:"name"`
	}](w, r)
	if !ok {
		return
	}

	if req.SessionKey == "" {
		JSONError(w, "sessionKey is required", http.StatusBadRequest)
		return
	}
	if req.Path == "" || req.Name == "" {
		JSONError(w, "path and name are required", http.StatusBadRequest)
		return
	}

	// Sanitize name
	cleanName := path.Clean(req.Name)
	if strings.Contains(cleanName, "/") || cleanName == "." || cleanName == ".." {
		JSONError(w, "invalid folder name", http.StatusBadRequest)
		return
	}

	client, err := h.sessions.GetClient(req.SessionKey)
	if err != nil {
		JSONError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		JSONError(w, fmt.Sprintf("SFTP client failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer sftpClient.Close()

	fullPath := path.Join(req.Path, cleanName)
	if err := sftpClient.Mkdir(fullPath); err != nil {
		JSONError(w, fmt.Sprintf("cannot create folder: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"path":    fullPath,
	})
}
