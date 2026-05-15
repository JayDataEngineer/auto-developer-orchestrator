package handlers

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var termUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TerminalWS handles WebSocket upgrade for an interactive terminal.
// GET /api/terminal/ws?shell=bash
// Falls back to host PTY when no sandbox is available.
func (h *SandboxHandler) TerminalWS(w http.ResponseWriter, r *http.Request) {
	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("terminal ws upgrade failed", zap.Error(err))
		return
	}
	defer conn.Close()

	// Start a host PTY
	shell := r.URL.Query().Get("shell")
	if shell == "" {
		shell = "bash"
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Try to set working directory to project path
	// cwd may be a project name (e.g. "auto-developer-orchestrator") or a full path.
	// Resolve project names via sandbox manager first.
	if cwd := r.URL.Query().Get("cwd"); cwd != "" {
		dir := cwd
		if sb := h.manager.FindSandboxByProject(cwd); sb != nil {
			dir = sb.ProjectPath
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			cmd.Dir = dir
		}
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		h.logger.Warn("terminal pty start failed", zap.Error(err))
		conn.WriteMessage(websocket.TextMessage, []byte("failed to start terminal: "+err.Error()))
		return
	}
	defer ptmx.Close()

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cmd.Process.Kill()
			cmd.Wait()
		})
	}
	defer cleanup()

	// PTY → WebSocket
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := ptmx.Read(buf)
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

	// WebSocket → PTY
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
		if _, err := ptmx.Write(msg); err != nil {
			cancel()
			return
		}
	}
}
