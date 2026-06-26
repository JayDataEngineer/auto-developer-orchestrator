package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// SessionIDHeader is the MCP-spec session header name.
const SessionIDHeader = "Mcp-Session-Id"

// ServeHTTP implements http.Handler for the MCP server.
//
// The single endpoint accepts POST requests with JSON-RPC envelopes and
// returns a single JSON-RPC response per request. SSE streaming, batched
// requests, and server-initiated notifications are all OUT of scope — the
// slim MVP is strictly request/response. GET and DELETE are reserved
// (return 405 and 204 respectively); OPTIONS handles CORS preflight.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS for localhost dev (Claude Desktop, Hermes connect from local
	// renderers that may set Origin). No auth on localhost.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, " + SessionIDHeader)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodGet:
		// Reserved for SSE notifications channel. MVP returns 405 to make
		// the contract explicit — clients should not rely on GET yet.
		http.Error(w, "GET not supported in MVP; use POST", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		// Client-initiated session teardown. MVP doesn't track per-client
		// state beyond the shared sessionID, so just 204.
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePost dispatches a single JSON-RPC request.
func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		writeJSONHTTPError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	// Detect batch requests (JSON array). MVP supports single requests only;
	// batches get a clear error rather than silent wrong behavior.
	if strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		writeJSONRPCError(w, "", codeInvalidRequest, "batch requests not supported in MVP")
		return
	}

	sessionID := r.Header.Get(SessionIDHeader)
	resp := s.Dispatch(r.Context(), body, sessionID)
	if resp == nil {
		// Notification — no response. Per MCP spec.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Echo the session header on initialize so the client can capture it.
	if resp.Result != nil {
		if obj, ok := resp.Result.(map[string]any); ok {
			if _, hasSession := obj["protocolVersion"]; hasSession && s.sessionID != "" {
				w.Header().Set(SessionIDHeader, s.sessionID)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// writeJSONRPCError writes a JSON-RPC error envelope as the HTTP response body.
// Used for transport-level failures that can't be expressed as a normal
// response (parse failures before ID is known, etc.).
func writeJSONRPCError(w http.ResponseWriter, idAny any, code int, msg string) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      nil,
		Error:   &rpcError{Code: code, Message: msg},
	}
	if idAny != nil {
		raw, _ := json.Marshal(idAny)
		resp.ID = raw
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// writeJSONHTTPError writes a plain HTTP error (not JSON-RPC). Reserved for
// transport errors where JSON-RPC framing would be misleading.
func writeJSONHTTPError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

// maxRequestBytes caps incoming request bodies. MCP tool args can be large
// (file writes, base64 images) but we still cap to bound server memory.
const maxRequestBytes = 32 * 1024 * 1024 // 32 MB

// ── Public dispatch helper for tests ──────────────────────────────

// DispatchRaw is a test helper that takes raw JSON bytes and returns the
// encoded response. Mirrors the HTTP path without HTTP plumbing.
func (s *Server) DispatchRaw(ctx context.Context, raw []byte) ([]byte, error) {
	resp := s.Dispatch(ctx, raw, "")
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}
