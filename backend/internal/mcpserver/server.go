// Package mcpserver exposes Pux's sandbox + tools as a standalone MCP server.
//
// The server speaks MCP (Model Context Protocol) over Streamable HTTP:
// JSON-RPC 2.0 messages on a single endpoint, session via Mcp-Session-Id
// header. The wire format mirrors what our own client in internal/mcp/
// consumes — see backend/internal/mcp/client.go for the protocol surface.
//
// The server is single-tenant: it owns one sandbox, registered at startup,
// torn down on SIGTERM. Multi-tenant routing is out of scope — run multiple
// servers on different ports if you need per-project isolation.
//
// Transport is localhost-only. Operators wanting tailnet exposure run a
// reverse proxy (Caddy/Tailscale Funnel) in front.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/audit"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ProtocolVersion is the MCP protocol version this server speaks.
// Must match what clients expect — see backend/internal/mcp/client.go.
const ProtocolVersion = "2025-03-26"

// Server is the MCP server. It holds a tool registry and dispatches
// JSON-RPC calls. HTTP transport is handled by ServeHTTP; JSON-RPC logic
// is exported as Dispatch so unit tests can drive it without HTTP.
type Server struct {
	name    string
	version string

	mu     sync.RWMutex
	tools  map[string]core.Tool
	sessionID string
	initialized atomic.Bool

	audit *audit.Logger
}

// New constructs a Server with no tools registered. Call RegisterTool for each
// capability before serving. Pass nil for al if audit logging is disabled
// (the common case — audit is opt-in via PUX_AUDIT_LOG).
func New(name, version string, al *audit.Logger) *Server {
	return &Server{
		name:    name,
		version: version,
		tools:   make(map[string]core.Tool),
		audit:   al,
	}
}

// RegisterTool adds a tool to the registry. Names must be unique — a duplicate
// registration panics early (caller bug, not runtime).
func (s *Server) RegisterTool(t core.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := t.Name()
	if _, exists := s.tools[name]; exists {
		panic(fmt.Sprintf("mcpserver: duplicate tool registration %q", name))
	}
	s.tools[name] = t
}

// Tools returns the current tool catalog (snapshot). The registry is mutable
// at runtime — callers can hot-swap tools via ResetTools (used by tests).
func (s *Server) Tools() []core.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]core.Tool, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, t)
	}
	return out
}

// ResetTools clears the registry. Test helper — not on the stable surface.
func (s *Server) ResetTools() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = make(map[string]core.Tool)
}

// ── JSON-RPC primitives ────────────────────────────────────────────

// rpcRequest is the JSON-RPC 2.0 request envelope.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is the JSON-RPC 2.0 response envelope.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes (per spec).
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Dispatch routes a JSON-RPC request to the appropriate handler. Returns the
// response object (never nil for requests with IDs; nil for notifications).
// Errors are returned in the response envelope, not as Go errors — Go error
// is reserved for transport-level failures (caller writes HTTP status).
func (s *Server) Dispatch(ctx context.Context, raw []byte, sessionID string) *rpcResponse {
	// Session validation: after initialize, all subsequent requests must carry
	// the established session ID. We don't reject mismatched IDs in MVP —
	// single-tenant server has one client — but we track the session for
	// future multi-tenant hooks.
	if sessionID != "" && s.sessionID != "" && sessionID != s.sessionID {
		return &rpcResponse{
			JSONRPC: "2.0",
			Error: &rpcError{
				Code:    codeInvalidRequest,
				Message: fmt.Sprintf("session mismatch (got %q, expected %q)", sessionID, s.sessionID),
			},
		}
	}

	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return &rpcResponse{
			JSONRPC: "2.0",
			Error: &rpcError{
				Code:    codeParseError,
				Message: "parse error: " + err.Error(),
			},
		}
	}

	// Notification (no ID) — execute side effects, return nil.
	isNotification := len(req.ID) == 0

	var resp *rpcResponse
	switch req.Method {
	case "initialize":
		resp = s.handleInitialize(req)
	case "notifications/initialized":
		// Client signals init complete. No response required (it's a notification).
		s.initialized.Store(true)
		return nil
	case "tools/list":
		resp = s.handleToolsList(req)
	case "tools/call":
		resp = s.handleToolsCall(ctx, req, sessionID)
	case "ping":
		resp = &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	default:
		if isNotification {
			return nil
		}
		resp = &rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    codeMethodNotFound,
				Message: "method not found: " + req.Method,
			},
		}
	}
	return resp
}

// ── Method handlers ───────────────────────────────────────────────

// initializeParams models the params object the client sends.
type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	ClientInfo      clientInfoRoot `json:"clientInfo"`
	Capabilities    json.RawMessage `json:"capabilities"`
}

type clientInfoRoot struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (s *Server) handleInitialize(req rpcRequest) *rpcResponse {
	// Parse params (ignore errors — client may send minimal init).
	var p initializeParams
	_ = json.Unmarshal(req.Params, &p)

	// Generate a session ID for this connection. Single-tenant MVP uses a
	// fixed ID; multi-tenant would derive from client identity here.
	if s.sessionID == "" {
		s.sessionID = newSessionID()
	}

	result := map[string]any{
		"protocolVersion": ProtocolVersion,
		"serverInfo": map[string]string{
			"name":    s.name,
			"version": s.version,
		},
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": true,
			},
		},
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) handleToolsList(req rpcRequest) *rpcResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		var schema any
		_ = json.Unmarshal(t.Schema(), &schema)
		tools = append(tools, map[string]any{
			"name":        t.Name(),
			"description": t.Description(),
			"inputSchema": schema,
		})
	}
	return &rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	}
}

// toolsCallParams models the params object for tools/call.
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// mcpContent is the MCP content shape for tool results.
type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Data       string `json:"data,omitempty"`       // base64 for image/audio
	// MimeType   string `json:"mimeType,omitempty"`
}

// mcpToolResult is the result envelope for tools/call.
type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

func (s *Server) handleToolsCall(ctx context.Context, req rpcRequest, sessionID string) *rpcResponse {
	var p toolsCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return &rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()},
		}
	}

	s.mu.RLock()
	t, ok := s.tools[p.Name]
	s.mu.RUnlock()
	if !ok {
		return &rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: codeMethodNotFound, Message: "unknown tool: " + p.Name},
		}
	}

	// Execute the tool. We translate core.Tool's (any, error) return into
	// MCP's content-array shape. Map returns are JSON-marshaled to text;
	// strings pass through; errors become isError=true content entries.
	start := time.Now()
	result, err := t.Execute(ctx, p.Arguments)

	// Audit log captures every successful tool dispatch (parse + lookup
	// passed). Protocol errors (bad params, unknown tool) are NOT audited
	// — they aren't model actions, they're wire-level failures.
	if s.audit != nil {
		entry := audit.Entry{
			Timestamp:  start,
			SessionID:  sessionID,
			Tool:       p.Name,
			Args:       p.Arguments,
			Result:     result,
			DurationMs: time.Since(start).Milliseconds(),
		}
		if err != nil {
			entry.Error = err.Error()
		}
		s.audit.Log(entry)
	}

	if err != nil {
		// Tool errors are returned as MCP tool results with isError=true
		// (per MCP spec). Transport/panic errors become JSON-RPC errors.
		return &rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: mcpToolResult{
				IsError: true,
				Content: []mcpContent{{Type: "text", Text: err.Error()}},
			},
		}
	}

	return &rpcResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: mcpToolResult{
			Content: []mcpContent{{Type: "text", Text: stringifyResult(result)}},
		},
	}
}

// stringifyResult converts the tool's any return into MCP text content.
// Strings pass through; everything else is JSON-marshaled for readability.
func stringifyResult(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
