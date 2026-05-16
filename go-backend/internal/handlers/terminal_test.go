package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func TestTerminalWS_UpgradesAndExecutes(t *testing.T) {
	handler := NewSandboxHandler(nil, zap.NewNop(), nil)

	server := httptest.NewServer(http.HandlerFunc(handler.TerminalWS))
	defer server.Close()

	// Connect via WebSocket
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminal/ws?shell=bash"
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusBadRequest {
			// pty might not be available in CI
			if strings.Contains(err.Error(), "failed to start terminal") {
				t.Skipf("pty not available: %v", err)
			}
		}
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Send a simple command
	err = ws.WriteMessage(websocket.BinaryMessage, []byte("echo hello-pux-test\n"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Read output — bash should echo the typed command and then execute it
	var allOutput string
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	for i := 0; i < 10; i++ {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		allOutput += string(msg)
		if strings.Contains(allOutput, "hello-pux-test") {
			break
		}
	}

	if !strings.Contains(allOutput, "hello-pux-test") {
		t.Errorf("Expected output to contain 'hello-pux-test', got: %q", allOutput)
	}
	t.Logf("Terminal output received (%d bytes)", len(allOutput))
}

func TestTerminalWS_ClosesGracefully(t *testing.T) {
	handler := NewSandboxHandler(nil, zap.NewNop(), nil)

	server := httptest.NewServer(http.HandlerFunc(handler.TerminalWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminal/ws?shell=bash"
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil && strings.Contains(err.Error(), "failed to start terminal") {
			t.Skipf("pty not available: %v", err)
		}
		t.Fatalf("Failed to connect: %v", err)
	}

	// Close the client — server should not panic
	ws.Close()
	time.Sleep(100 * time.Millisecond)
	// If we get here, the server handled the close gracefully
}
