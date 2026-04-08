package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// DebugRpcTest starts a fresh pi subprocess, sends set_model + prompt, captures
// 30s of stdout, and returns raw events. Useful for testing pi RPC independently.
func (h *PiHandler) DebugRpcTest(w http.ResponseWriter, r *http.Request) {
	piPath, err := exec.LookPath("pi")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": "pi binary not found in PATH",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, piPath, "--mode", "rpc", "--no-session")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("stdin pipe: %v", err),
		})
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("stdout pipe: %v", err),
		})
		return
	}

	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("start: %v", err),
		})
		return
	}
	defer cmd.Process.Kill()

	// Send set_model
	setModel := `{"type":"set_model","provider":"litellm","modelId":"qwen-cloud","id":"1"}` + "\n"
	if _, err := stdin.Write([]byte(setModel)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("write set_model: %v", err),
		})
		return
	}

	time.Sleep(500 * time.Millisecond)

	// Send prompt
	prompt := `{"type":"prompt","message":"Say hi in one word","id":"2"}` + "\n"
	if _, err := stdin.Write([]byte(prompt)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("write prompt: %v", err),
		})
		return
	}

	// Read stdout for up to 30 seconds
	type rawEvent struct {
		Line  string      `json:"line"`
		Event interface{} `json:"event,omitempty"`
	}

	var events []rawEvent
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	timeout := time.After(30 * time.Second)
	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			evt := rawEvent{Line: line}
			var parsed interface{}
			if json.Unmarshal([]byte(line), &parsed) == nil {
				evt.Event = parsed
			}
			events = append(events, evt)

			// Stop after agent_end
			if strings.Contains(line, `"agent_end"`) {
				break
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-timeout:
	}

	stdin.Close()
	cmd.Process.Kill()
	cmd.Wait()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"events":  events,
		"count":   len(events),
	})
}
