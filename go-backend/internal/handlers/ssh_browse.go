package handlers

import (
	"fmt"
	"net/http"
	"path"
	"sort"
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

	// Filter and build response (same shape as local fs_browse)
	var result []browseEntry
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files and known noise
		if strings.HasPrefix(name, ".") || skipNames[name] {
			continue
		}

		isDir := entry.IsDir()
		var size int64
		if !isDir {
			size = entry.Size()
		}

		result = append(result, browseEntry{Name: name, IsDir: isDir, Size: size})

		// Cap at 200 entries
		if len(result) >= 200 {
			break
		}
	}

	// Sort: directories first, then alphabetical
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})

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
