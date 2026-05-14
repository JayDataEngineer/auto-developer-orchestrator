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

// TestTerminalWS_Reconnect verifies that after a WebSocket disconnect,
// a new connection can be established on the same endpoint.
func TestTerminalWS_Reconnect(t *testing.T) {
	handler := NewSandboxHandler(nil, zap.NewNop())

	server := httptest.NewServer(http.HandlerFunc(handler.TerminalWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminal/ws?shell=bash"

	// First connection
	ws1, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil && strings.Contains(err.Error(), "failed to start terminal") {
			t.Skipf("pty not available: %v", err)
		}
		t.Fatalf("First connection failed: %v", err)
	}

	// Send something to verify it works
	ws1.WriteMessage(websocket.BinaryMessage, []byte("echo first\n"))
	ws1.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := ws1.ReadMessage()
	if err != nil {
		t.Fatalf("First read failed: %v", err)
	}
	firstOutput := string(msg)
	if !strings.Contains(firstOutput, "first") {
		t.Errorf("First connection output missing 'first': %q", firstOutput)
	}

	// Close first connection
	ws1.Close()
	time.Sleep(100 * time.Millisecond)

	// Reconnect — should work
	ws2, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Reconnect failed: %v (resp=%v)", err, resp)
	}
	defer ws2.Close()

	// Verify second connection works
	ws2.WriteMessage(websocket.BinaryMessage, []byte("echo second\n"))
	ws2.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg2, err := ws2.ReadMessage()
	if err != nil {
		t.Fatalf("Second read failed: %v", err)
	}
	secondOutput := string(msg2)
	if !strings.Contains(secondOutput, "second") {
		t.Errorf("Second connection output missing 'second': %q", secondOutput)
	}

	t.Log("Reconnect test passed — two sequential connections both worked")
}

// TestTerminalWS_ConcurrentConnections verifies multiple terminals
// can be open simultaneously.
func TestTerminalWS_ConcurrentConnections(t *testing.T) {
	handler := NewSandboxHandler(nil, zap.NewNop())

	server := httptest.NewServer(http.HandlerFunc(handler.TerminalWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminal/ws?shell=bash"

	conns := make([]*websocket.Conn, 3)
	for i := range conns {
		ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			if resp != nil && strings.Contains(err.Error(), "failed to start terminal") {
				t.Skipf("pty not available: %v", err)
			}
			t.Fatalf("Connection %d failed: %v", i, err)
		}
		conns[i] = ws
	}

	// Send unique markers on each
	for i, ws := range conns {
		ws.WriteMessage(websocket.BinaryMessage, []byte("echo conn"+string(rune('0'+i))+"\n"))
	}

	// Read from each
	for i, ws := range conns {
		ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, msg, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("Connection %d read failed: %v", i, err)
			continue
		}
		marker := "conn" + string(rune('0'+i))
		if !strings.Contains(string(msg), marker) {
			t.Errorf("Connection %d: expected %q in output, got: %q", i, marker, string(msg))
		}
	}

	// Clean up
	for _, ws := range conns {
		ws.Close()
	}

	t.Log("Concurrent connections test passed")
}
