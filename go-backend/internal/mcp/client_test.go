package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestClient_SessionPersistence verifies that session ID is reused across calls.
func TestClient_SessionPersistence(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "persistent-session-abc")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		// Every subsequent call should include the session ID
		sid := r.Header.Get("Mcp-Session-Id")
		if sid != "persistent-session-abc" {
			t.Errorf("call %d: expected session ID, got %q", atomic.LoadInt32(&callCount), sid)
		}
		atomic.AddInt32(&callCount, 1)

		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`),
		})
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)
	ctx := context.Background()

	// Initialize once
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	// Make multiple calls — all should reuse the session
	for i := 0; i < 5; i++ {
		_, err := client.CallTool(ctx, "research", map[string]any{"query": "test"})
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}

	if atomic.LoadInt32(&callCount) != 5 {
		t.Errorf("expected 5 tool calls, got %d", atomic.LoadInt32(&callCount))
	}
}

// TestClient_AutoInitialize verifies CallTool auto-initializes when no session exists.
func TestClient_AutoInitialize(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		methods = append(methods, req.Method)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "auto-init-session")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"auto-init result"}]}`),
		})
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)

	// Call Research WITHOUT calling Initialize first
	result, err := client.Research(context.Background(), "test query", 3)
	if err != nil {
		t.Fatalf("Research without prior Initialize failed: %v", err)
	}
	if result != "auto-init result" {
		t.Errorf("unexpected result: %q", result)
	}

	// Should have auto-initialized: initialize → notification → tools/call
	hasInit := false
	for _, m := range methods {
		if m == "initialize" {
			hasInit = true
		}
	}
	if !hasInit {
		t.Errorf("expected auto-initialize, methods: %v", methods)
	}
}

// TestClient_ResearchMaxResults verifies max_results is passed correctly.
func TestClient_ResearchMaxResults(t *testing.T) {
	tests := []struct {
		name       string
		maxResults int
		expected   int
	}{
		{"custom max", 5, 5},
		{"zero defaults to 3", 0, 3},
		{"negative defaults to 3", -1, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedArgs map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req jsonRPCRequest
				json.NewDecoder(r.Body).Decode(&req)

				if req.Method == "initialize" {
					w.Header().Set("Mcp-Session-Id", "s")
					json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
					return
				}
				if req.Method == "notifications/initialized" {
					return
				}

				params, _ := req.Params.(map[string]any)
				capturedArgs, _ = params["arguments"].(map[string]any)

				json.NewEncoder(w).Encode(jsonRPCResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result:  json.RawMessage(`{"content":[{"type":"text","text":"result"}]}`),
				})
			}))
			defer server.Close()

			client := NewClient("test", server.URL, nil)
			_, err := client.Research(context.Background(), "test", tt.maxResults)
			if err != nil {
				t.Fatal(err)
			}
			maxRes, _ := capturedArgs["max_results"].(float64)
			if int(maxRes) != tt.expected {
				t.Errorf("expected max_results=%d, got %v", tt.expected, capturedArgs["max_results"])
			}
		})
	}
}

// TestClient_ScrapeParams verifies scrape passes the URL correctly.
func TestClient_ScrapeParams(t *testing.T) {
	var capturedArgs map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		params, _ := req.Params.(map[string]any)
		capturedArgs, _ = params["arguments"].(map[string]any)

		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"page content"}]}`),
		})
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)
	_, err := client.Scrape(context.Background(), "https://docs.example.com/api")
	if err != nil {
		t.Fatal(err)
	}
	if capturedArgs["url"] != "https://docs.example.com/api" {
		t.Errorf("expected url, got %v", capturedArgs["url"])
	}
}

// TestClient_MultipleContentItems verifies handling of multi-item content arrays.
func TestClient_MultipleContentItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		// Return multiple content items — client should return first one
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]}`),
		})
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)
	result, err := client.CallTool(context.Background(), "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "first" {
		t.Errorf("expected first content item, got %q", result)
	}
}

// TestClient_RawResultFallback verifies fallback when content array is empty.
func TestClient_RawResultFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		// Return empty content array — should fall back to raw result
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"raw":"data"}`),
		})
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)
	result, err := client.CallTool(context.Background(), "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "raw") {
		t.Errorf("expected raw result fallback, got %q", result)
	}
}

// TestClient_HTTPError verifies HTTP error handling.
func TestClient_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)
	_, err := client.CallTool(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

// TestClient_ContextCancellation verifies context cancel is respected.
func TestClient_ContextCancellation(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // block until test is done
	}))
	defer server.Close()
	defer close(block) // unblock the handler on cleanup

	client := NewClient("test", server.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Research(ctx, "test", 1)
	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
}

// TestClient_ProtocolVersion verifies the initialize request has the correct protocol version.
func TestClient_ProtocolVersion(t *testing.T) {
	var capturedInit map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Method == "initialize" {
			capturedInit, _ = req.Params.(map[string]any)
			w.Header().Set("Mcp-Session-Id", "s")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`),
		})
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)
	client.Initialize(context.Background())

	if capturedInit == nil {
		t.Fatal("no initialize params captured")
	}
	if capturedInit["protocolVersion"] != "2025-03-26" {
		t.Errorf("expected protocol 2025-03-26, got %v", capturedInit["protocolVersion"])
	}
	clientInfo, _ := capturedInit["clientInfo"].(map[string]any)
	if clientInfo["name"] != "auto-developer-orchestrator" {
		t.Errorf("expected client name, got %v", clientInfo)
	}
}

// TestClient_JSONRPCVersion verifies all requests use JSON-RPC 2.0.
func TestClient_JSONRPCVersion(t *testing.T) {
	var versions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		versions = append(versions, req.JSONRPC)

		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s")
			json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: 1, Result: json.RawMessage(`{}`)})
			return
		}
		if req.Method == "notifications/initialized" {
			return
		}
		json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`),
		})
	}))
	defer server.Close()

	client := NewClient("test", server.URL, nil)
	client.Initialize(context.Background())
	client.CallTool(context.Background(), "test", nil)

	for i, v := range versions {
		if v != "2.0" {
			t.Errorf("request %d: expected JSONRPC 2.0, got %q", i, v)
		}
	}
}
