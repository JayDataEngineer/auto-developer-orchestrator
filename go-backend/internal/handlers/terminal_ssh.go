package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
	"go.uber.org/zap"
)

// SshTerminalHandler handles WebSocket upgrade for SSH remote terminals.
type SshTerminalHandler struct {
	sessions *puxssh.SessionManager
	log      *zap.Logger
}

// NewSshTerminalHandler creates a new SSH terminal handler.
func NewSshTerminalHandler(sessions *puxssh.SessionManager, log *zap.Logger) *SshTerminalHandler {
	return &SshTerminalHandler{sessions: sessions, log: log}
}

// SshTerminalWS handles WebSocket upgrade for an interactive SSH terminal.
// GET /api/terminal/ssh/ws?sessionKey=xxx&rows=24&cols=80&cwd=/path
//
// The frontend first establishes an SSH session via /api/pux/ssh/connect,
// then passes the sessionKey here to get a terminal.
func (h *SshTerminalHandler) SshTerminalWS(w http.ResponseWriter, r *http.Request) {
	sessionKey := r.URL.Query().Get("sessionKey")
	if sessionKey == "" {
		JSONError(w, "sessionKey is required", http.StatusBadRequest)
		return
	}

	client, err := h.sessions.GetClient(sessionKey)
	if err != nil {
		JSONError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Terminal dimensions
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}

	// Working directory
	cwd := r.URL.Query().Get("cwd")

	// Create SSH session
	sshSession, err := client.NewSession()
	if err != nil {
		JSONError(w, fmt.Sprintf("SSH session failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer sshSession.Close()

	// Request PTY
	if err := sshSession.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		JSONError(w, fmt.Sprintf("PTY request failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Set up pipes
	stdinPipe, err := sshSession.StdinPipe()
	if err != nil {
		JSONError(w, fmt.Sprintf("stdin pipe failed: %v", err), http.StatusInternalServerError)
		return
	}

	stdoutPipe, err := sshSession.StdoutPipe()
	if err != nil {
		JSONError(w, fmt.Sprintf("stdout pipe failed: %v", err), http.StatusInternalServerError)
		return
	}

	stderrPipe, err := sshSession.StderrPipe()
	if err != nil {
		JSONError(w, fmt.Sprintf("stderr pipe failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Start shell (with cd to cwd if specified)
	if cwd != "" {
		// Use a login shell that cds to the target directory
		err = sshSession.Start(fmt.Sprintf("cd '%s' 2>/dev/null; exec bash --login", cwd))
	} else {
		err = sshSession.Start("bash --login")
	}
	if err != nil {
		JSONError(w, fmt.Sprintf("shell start failed: %v", err), http.StatusInternalServerError)
		return
	}

	// WebSocket upgrade
	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("SSH terminal ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			sshSession.Close()
		})
	}
	defer cleanup()

	// Handle resize via SSH protocol messages
	// We watch for a special resize message: "\x01RESIZE:rows:cols"
	resizeCh := make(chan [2]int, 4)

	// stdout+stderr → WebSocket
	go func() {
		buf := make([]byte, 8192)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := stdoutPipe.Read(buf)
			if err != nil {
				cancel()
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				cancel()
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 8192)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := stderrPipe.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				cancel()
				return
			}
		}
	}()

	// Resize handler
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case dims := <-resizeCh:
				sshSession.WindowChange(dims[0], dims[1])
			}
		}
	}()

	// WebSocket → stdin
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(24 * time.Hour))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			cancel()
			return
		}

		// Check for resize control message: "\x01RESIZE:rows:cols"
		msgStr := string(msg)
		if len(msgStr) > 8 && msgStr[:8] == "\x01RESIZE:" {
			var r, c int
			fmt.Sscanf(msgStr[8:], "%d:%d", &r, &c)
			if r > 0 && c > 0 {
				select {
				case resizeCh <- [2]int{r, c}:
				default:
				}
			}
			continue
		}

		if _, err := stdinPipe.Write(msg); err != nil {
			cancel()
			return
		}
	}
}
