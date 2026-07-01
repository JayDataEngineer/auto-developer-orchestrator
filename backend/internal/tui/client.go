// client.go is a slim MCP HTTP client. It owns one session per TUI process
// lifetime: an initialize handshake captures the Mcp-Session-Id header,
// then all subsequent tools/call dispatches reuse that session.
//
// The client has ZERO dependencies on internal/mcpserver, internal/agent,
// internal/sandbox, etc. It speaks pure JSON-RPC over HTTP — same wire
// contract any external MCP client (Claude Desktop, Hermes, etc.) would.
//
// The seam is intentional: this package is fully deletable. rm this file
// (and the rest of internal/tui/) and the MCP server builds + runs identically.

package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// mcpProtocolVersion is the wire-protocol version we declare at initialize.
// Matches what scripts/smoke_history.py uses; the server's actual version
// lives in mcpserver/server.go::ProtocolVersion.
const mcpProtocolVersion = "2025-03-26"

// MCPClient owns one MCP session against a running pux-mcpserver.
// Construct via NewMCPClient; call Init once before any Dispatch/Status call.
type MCPClient struct {
	baseURL string
	http    *http.Client

	mu        sync.Mutex
	sessionID string // captured from initialize response header
	initialized bool
}

// NewMCPClient constructs a client pointed at addr (e.g. "http://127.0.0.1:9987").
// The HTTP timeout is 180s to match the agent-loop budget — long-running
// dispatches can sit at the server for a while before returning the task_id.
func NewMCPClient(addr string) *MCPClient {
	return &MCPClient{
		baseURL: addr,
		http:    &http.Client{Timeout: 180 * time.Second},
	}
}

// Init runs the initialize handshake + the notifications/initialized
// fire-and-forget. Captures the Mcp-Session-Id header for subsequent calls.
// Safe to call multiple times — subsequent calls are no-ops.
func (c *MCPClient) Init(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialized {
		return nil
	}

	// 1. initialize — capture session ID from response header (the body
	//    doesn't carry it).
	initResp, err := c.doLocked(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      "init",
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "pux-tui",
				"version": "0.1.0",
			},
		},
	}, "")
	if err != nil {
		return fmt.Errorf("init handshake: %w", err)
	}
	if initResp.sessionID == "" {
		return fmt.Errorf("init: server returned no Mcp-Session-Id header")
	}
	c.sessionID = initResp.sessionID

	// 2. notifications/initialized — fire-and-forget, no response expected.
	_, err = c.doLocked(ctx, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	}, c.sessionID)
	if err != nil {
		return fmt.Errorf("notifications/initialized: %w", err)
	}

	c.initialized = true
	return nil
}

// DispatchResponse is the parsed shape returned by dispatch_task.
type DispatchResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// Dispatch kicks off a task against the named org. The taskDescription is
// the verbatim string the CTO sees — the caller (TUI) is responsible for
// accumulating multi-turn context into this single blob. Returns the new
// task_id immediately; the work happens in a server goroutine.
func (c *MCPClient) Dispatch(ctx context.Context, org, taskDescription string) (DispatchResponse, error) {
	if err := c.Init(ctx); err != nil {
		return DispatchResponse{}, err
	}
	resp, err := c.callTool(ctx, "dispatch_task", map[string]any{
		"org_name":         org,
		"task_description": taskDescription,
	})
	if err != nil {
		return DispatchResponse{}, err
	}
	var out DispatchResponse
	if err := json.Unmarshal(resp, &out); err != nil {
		return DispatchResponse{}, fmt.Errorf("dispatch_task: parse response: %w", err)
	}
	if out.TaskID == "" {
		return DispatchResponse{}, fmt.Errorf("dispatch_task: empty task_id in response: %s", string(resp))
	}
	return out, nil
}

// StatusResponse is the parsed shape returned by get_task_status. Fields
// use pointers / zero values for optionality so a running task (no result
// yet) deserializes cleanly.
type StatusResponse struct {
	TaskID          string `json:"task_id"`
	Org             string `json:"org"`
	Status          string `json:"status"`              // pending | running | complete | failed
	Round           int    `json:"round"`
	Result          string `json:"result,omitempty"`
	Error           string `json:"error,omitempty"`
	TranscriptTail  string `json:"transcript_tail,omitempty"`
}

// Status polls the current state of a dispatched task. Cheap to call —
// the server returns a snapshot from an in-memory map.
func (c *MCPClient) Status(ctx context.Context, taskID string) (StatusResponse, error) {
	if err := c.Init(ctx); err != nil {
		return StatusResponse{}, err
	}
	resp, err := c.callTool(ctx, "get_task_status", map[string]any{
		"task_id": taskID,
	})
	if err != nil {
		return StatusResponse{}, err
	}
	var out StatusResponse
	if err := json.Unmarshal(resp, &out); err != nil {
		return StatusResponse{}, fmt.Errorf("get_task_status: parse response: %w", err)
	}
	return out, nil
}

// OrgSummary is one entry from list_orgs — name + description + role names.
type OrgSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
}

// ListOrgs enumerates the orgs available under <project>/orgs/. Used to
// populate the org picker when --org isn't passed on the command line.
func (c *MCPClient) ListOrgs(ctx context.Context) ([]OrgSummary, error) {
	if err := c.Init(ctx); err != nil {
		return nil, err
	}
	resp, err := c.callTool(ctx, "list_orgs", map[string]any{})
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Orgs  []OrgSummary `json:"orgs"`
		Count int          `json:"count"`
	}
	if err := json.Unmarshal(resp, &wrap); err != nil {
		return nil, fmt.Errorf("list_orgs: parse response: %w", err)
	}
	return wrap.Orgs, nil
}

// ── internals ─────────────────────────────────────────────────────────

// rpcResponse is the JSON-RPC envelope. The Result is held as RawMessage
// so the caller can decode just the inner shape it needs.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rawResp carries the parsed envelope plus the captured session header.
type rawResp struct {
	body      *rpcResponse
	sessionID string
}

// doLocked executes one JSON-RPC call while holding the client mutex. The
// mutex isn't for thread-safety of the HTTP call — it's to serialize the
// session-ID capture during Init (which can race with a concurrent call
// if the caller forgot to Init first).
func (c *MCPClient) doLocked(ctx context.Context, payload map[string]any, sessionID string) (rawResp, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return rawResp{}, fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return rawResp{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	httpResp, err := c.http.Do(req)
	if err != nil {
		return rawResp{}, fmt.Errorf("http: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode/100 != 2 {
		return rawResp{}, fmt.Errorf("http status %d", httpResp.StatusCode)
	}

	// A notifications/initialized call returns 202 Accepted with no body.
	// Treat empty body as a successful notification.
	var env rpcResponse
	if httpResp.ContentLength == 0 {
		return rawResp{body: nil, sessionID: httpResp.Header.Get("Mcp-Session-Id")}, nil
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&env); err != nil {
		return rawResp{}, fmt.Errorf("decode response: %w", err)
	}
	if env.Error != nil {
		return rawResp{}, fmt.Errorf("rpc error %d: %s", env.Error.Code, env.Error.Message)
	}
	return rawResp{body: &env, sessionID: httpResp.Header.Get("Mcp-Session-Id")}, nil
}

// callTool is the post-init tools/call helper. It returns the decoded
// "text" field of the first content block — the dispatch surface packs
// JSON-encoded bodies into that slot per the MCP content-block convention.
func (c *MCPClient) callTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	c.mu.Lock()
	defer c.mu.Unlock()

	raw, err := c.doLocked(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}, c.sessionID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if raw.body == nil {
		return nil, fmt.Errorf("%s: empty response", name)
	}

	// The MCP tools/call result envelope is:
	//   {content: [{type:"text", text:"<json-stringified body>"}], isError: bool}
	// Extract the text field and surface the inner body.
	var wrap struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw.body.Result, &wrap); err != nil {
		return nil, fmt.Errorf("%s: parse content envelope: %w", name, err)
	}
	if wrap.IsError {
		// Concatenate all text blocks — some tools return multi-block errors.
		var msg string
		for _, b := range wrap.Content {
			if b.Text != "" {
				if msg != "" {
					msg += "\n"
				}
				msg += b.Text
			}
		}
		return nil, fmt.Errorf("%s: tool error: %s", name, msg)
	}
	if len(wrap.Content) == 0 || wrap.Content[0].Text == "" {
		return nil, fmt.Errorf("%s: empty content array", name)
	}
	return json.RawMessage(wrap.Content[0].Text), nil
}
