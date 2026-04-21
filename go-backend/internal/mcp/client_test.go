package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClient_Initialize(t *testing.T) {
	var initialized int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req.Method == "initialize" {
			atomic.StoreInt32(&initialized, 1)
			w.Header().Set("Mcp-Session-Id", "test-session-123")
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      1,
				Result:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{}}`),
			})
			return
		}

		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Tool calls — verify session ID is present
		if req.Method == "tools/call" {
			if sid := r.Header.Get("Mcp-Session-Id"); sid != "test-session-123" {
				t.Errorf("expected session ID 'test-session-123', got '%s'", sid)
			}
			json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"test result"}]}`),
			})
			return
		}

		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)
	ctx := context.Background()

	// Test Initialize
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if client.sessionID != "test-session-123" {
		t.Errorf("session ID not captured: got %q", client.sessionID)
	}

	// Test CallTool with session
	result, err := client.CallTool(ctx, "research", map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result != "test result" {
		t.Errorf("unexpected result: got %q", result)
	}
}

func TestClient_Research(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		// Verify research params
		params, _ := req.Params.(map[string]any)
		if params["name"] != "research" {
			t.Errorf("expected tool name 'research', got %v", params["name"])
		}
		args, _ := params["arguments"].(map[string]any)
		if args["query"] != "golang testing" {
			t.Errorf("unexpected query: %v", args["query"])
		}

		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"Go testing best practices..."}]}`),
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)
	result, err := client.Research(context.Background(), "golang testing", 3)
	if err != nil {
		t.Fatalf("Research failed: %v", err)
	}
	if result != "Go testing best practices..." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestClient_Scrape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"# Example\n\nPage content."}]}`),
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)
	result, err := client.Scrape(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("Scrape failed: %v", err)
	}
	if result != "# Example\n\nPage content." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestClient_ServerUnreachable(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", nil) // unreachable port
	_, err := client.Research(context.Background(), "test", 1)
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestClient_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32600, Message: "invalid params"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)
	_, err := client.CallTool(context.Background(), "research", map[string]any{})
	if err == nil {
		t.Fatal("expected error from tool error response")
	}
}
