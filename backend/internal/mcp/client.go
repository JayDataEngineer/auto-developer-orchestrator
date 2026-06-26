// Package mcp provides an HTTP client for the MCP research server.
// It implements the Streamable HTTP transport (MCP spec 2025-03-26).
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Client is an HTTP client for the MCP research server.
//
// A client has a primary endpoint and an optional fallback endpoint. When
// fallback is configured (non-empty), the HealthMonitor or explicit
// SwitchEndpoint call can swap which URL is active in response to transport-
// level failures. The active endpoint is read on every request via the mu
// RWMutex — a switch is atomic from the caller's perspective.
//
// Tool discovery at boot snapshots both primaryTools and fallbackTools; the
// MultiClient advertises the INTERSECTION so the agent never calls a tool the
// active endpoint can't serve. The intersection is stable for the process
// lifetime — RefreshTools recomputes it only when explicitly asked.
type Client struct {
	prefix     string
	httpClient *http.Client
	logger     *zap.Logger
	sem        chan struct{}

	// Immutable after construction.
	primaryEndpoint  string
	fallbackEndpoint string

	// mu guards the active routing state. SwitchEndpoint takes write to flip
	// endpoint + clear sessionID together; doRequest takes read to snapshot
	// both at request-build time. Critical sections are tiny — the HTTP
	// round-trip happens outside the lock.
	mu           sync.RWMutex
	endpoint     string // current active endpoint (primary or fallback)
	sessionID    string // Mcp-Session-Id for the active endpoint
	instructions string // instructions text from initialize response

	// primaryTools / fallbackTools are populated at boot by InitializeAll.
	// Both stay nil when the endpoint wasn't probed. MultiClient intersects
	// them to decide which tools to advertise.
	primaryTools  []MCPTool
	fallbackTools []MCPTool

	// onSwitch fires on every endpoint change (primary→fallback or
	// fallback→primary). Wired by app.go to emit the mcp_endpoint_changed
	// SSE event. Nil when no SSE bus is wired (e.g. in unit tests).
	onSwitch func(from, to, reason string)
}

// NewClient creates a new MCP client without a fallback endpoint. Preserves
// the original signature for callers that don't need fallback. Equivalent to
// NewClientWithFallback(prefix, endpoint, "", logger).
func NewClient(prefix, endpoint string, logger *zap.Logger) *Client {
	return NewClientWithFallback(prefix, endpoint, "", logger)
}

// NewClientWithFallback creates a client with a primary endpoint and an
// optional fallback endpoint. Empty fallback means no fallback configured
// (behavior identical to NewClient). The prefix identifies this server for
// tool routing and instruction registration. If primary is empty, it falls
// back to the MCP_RESEARCH_ENDPOINT env var.
func NewClientWithFallback(prefix, primary, fallback string, logger *zap.Logger) *Client {
	if primary == "" {
		primary = os.Getenv("MCP_RESEARCH_ENDPOINT")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		prefix:           prefix,
		primaryEndpoint:  primary,
		fallbackEndpoint: fallback,
		endpoint:         primary,
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // MCP tools can be slow (AI vision models, search, scrape)
		},
		logger: logger,
		sem:    make(chan struct{}, 2), // max 2 concurrent MCP requests
	}
}

// SetSwitchCallback registers a callback fired on every endpoint change.
// app.go wires this to emit the mcp_endpoint_changed SSE event. The callback
// receives (fromURL, toURL, reason). Calling this with nil disables the
// callback (used in tests). Must be called before Initialize — the callback
// fires during the boot switch decision if both endpoints need probing.
func (c *Client) SetSwitchCallback(fn func(from, to, reason string)) {
	c.mu.Lock()
	c.onSwitch = fn
	c.mu.Unlock()
}

// SwitchEndpoint atomically swaps which endpoint is active. Clears sessionID
// because the new endpoint has its own session — the next CallTool auto-
// initializes via Initialize. Fires the onSwitch callback (if wired) AFTER
// the swap so recipients observe the new state. Returns an error if target
// matches neither primary nor fallback (defensive — caller bug otherwise).
func (c *Client) SwitchEndpoint(target, reason string) error {
	if target != c.primaryEndpoint && target != c.fallbackEndpoint {
		return fmt.Errorf("SwitchEndpoint: %q is neither primary (%q) nor fallback (%q)",
			target, c.primaryEndpoint, c.fallbackEndpoint)
	}
	c.mu.Lock()
	old := c.endpoint
	if old == target {
		c.mu.Unlock()
		return nil // already there — no-op, no callback
	}
	c.endpoint = target
	c.sessionID = ""
	cb := c.onSwitch
	c.mu.Unlock()
	if cb != nil {
		cb(old, target, reason)
	}
	c.logger.Info("MCP endpoint switched",
		zap.String("prefix", c.prefix),
		zap.String("from", old),
		zap.String("to", target),
		zap.String("reason", reason))
	return nil
}

// PrimaryEndpoint returns the declared primary URL. Immutable after construction.
func (c *Client) PrimaryEndpoint() string {
	return c.primaryEndpoint
}

// FallbackEndpoint returns the declared fallback URL, or empty when no
// fallback is configured.
func (c *Client) FallbackEndpoint() string {
	return c.fallbackEndpoint
}

// HasFallback reports whether a fallback endpoint is configured.
func (c *Client) HasFallback() bool {
	return c.fallbackEndpoint != ""
}

// ActiveEndpoint returns the URL currently in use (primary or fallback).
func (c *Client) ActiveEndpoint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.endpoint
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Initialize performs the MCP handshake.
func (c *Client) Initialize(ctx context.Context) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "auto-developer-orchestrator",
				"version": "0.1",
			},
		},
	}

	respBody, headers, err := c.doRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("MCP initialize failed: %w", err)
	}

	// Capture session ID from response headers for subsequent requests.
	// Held under the write lock because doRequest reads it under read lock
	// and SwitchEndpoint clears it — without coordination, a switch mid-init
	// could let a stale primary session slip onto a fallback request.
	if sid := headers.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	// Parse instructions from the initialize response
	var initResult struct {
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(respBody, &struct {
		Result json.RawMessage `json:"result"`
	}{}); err == nil {
		// respBody is the full JSON-RPC response, parse result.instructions
		var rpcResp struct {
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(respBody, &rpcResp) == nil && len(rpcResp.Result) > 0 {
			_ = json.Unmarshal(rpcResp.Result, &initResult)
		}
	}
	if initResult.Instructions != "" {
		c.mu.Lock()
		c.instructions = initResult.Instructions
		c.mu.Unlock()
		c.logger.Debug("MCP server instructions captured",
			zap.String("prefix", c.prefix),
			zap.Int("length", len(initResult.Instructions)),
		)
	}

	// Send initialized notification (fire-and-forget, no ID)
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	_, _, _ = c.doRequest(ctx, notif)

	c.logger.Debug("MCP initialize response",
		zap.String("sessionId", sidLocked(c)),
		zap.String("body", string(respBody)),
	)
	return nil
}

// sidLocked is a tiny helper that reads sessionID under the RLock for log
// formatting. Kept separate so the deferred Unlock can't be forgotten.
func sidLocked(c *Client) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// CallTool calls an MCP tool by name with the given arguments.
// Returns the text content from the tool result.
// Auto-initializes the session if not already initialized.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	// Auto-initialize if no session established yet
	if c.sessionID == "" {
		if err := c.Initialize(ctx); err != nil {
			return "", fmt.Errorf("MCP auto-initialize failed: %w", err)
		}
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      name,
			"arguments": args,
		},
	}

	respBody, _, err := c.doRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("MCP tools/call %s failed: %w", name, err)
	}

	// Parse the response
	var resp jsonRPCResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("MCP response parse error: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("MCP tool error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}

	// Extract text from result.content[0].text
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		// Return raw result as fallback
		return string(resp.Result), nil
	}

	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}

	return string(resp.Result), nil
}

// Research performs a research query (search + scrape in one call).
// Returns the raw text result from the MCP server.
func (c *Client) Research(ctx context.Context, query string, maxResults int) (string, error) {
	if maxResults <= 0 {
		maxResults = 3
	}
	return c.CallTool(ctx, "research", map[string]any{
		"query":       query,
		"max_results": maxResults,
		"depth":       "quick",
	})
}

// Search performs a search-only query (no scraping). Returns structured results
// with titles, URLs, and snippets. Use when you need URLs to scrape selectively.
func (c *Client) Search(ctx context.Context, query string, maxResults int) (string, error) {
	if maxResults <= 0 {
		maxResults = 5
	}
	return c.CallTool(ctx, "search", map[string]any{
		"query":  query,
		"top_k":  maxResults,
		"rerank": false,
	})
}

// Scrape fetches a URL and returns its content as clean markdown.
func (c *Client) Scrape(ctx context.Context, url string) (string, error) {
	return c.CallTool(ctx, "scrape", map[string]any{
		"url": url,
	})
}

// ProcessHTML takes raw HTML and returns clean LLM-ready markdown.
// Used by the browser scrape fallback: browser gets HTML (bypasses anti-bot),
// then this cleans it through Crawl4AI's content extraction pipeline.
func (c *Client) ProcessHTML(ctx context.Context, html string) (string, error) {
	return c.CallTool(ctx, "process_html", map[string]any{
		"html": html,
	})
}

// IsAvailable returns true if the MCP server is reachable.
// Fast-path: if a session is already established against the active endpoint,
// return true without re-initializing. This avoids competing for the semaphore
// with active worker tool calls and prevents overwriting a valid session ID.
//
// After SwitchEndpoint clears sessionID, this starts probing again — that's
// correct, not a bug: the new endpoint hasn't been initialized yet.
func (c *Client) IsAvailable() bool {
	c.mu.RLock()
	sid := c.sessionID
	c.mu.RUnlock()
	if sid != "" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Initialize(ctx) == nil
}

// MCPTool represents a tool definition from the MCP server's tools/list response.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListTools queries the MCP server for available tools and their schemas.
// Must be called after Initialize. Returns an empty list if the server has no tools.
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/list",
	}

	respBody, _, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP tools/list failed: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("MCP tools/list parse error: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP tools/list error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}

	var result struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("MCP tools/list result parse error: %w", err)
	}

	c.logger.Debug("MCP tools listed", zap.Int("count", len(result.Tools)))
	return result.Tools, nil
}

// PrimaryTools returns the tool list snapshot taken from the primary endpoint
// at boot. Nil if DiscoverBothEndpoints hasn't been called or primary wasn't
// reachable. Used by MultiClient to compute the intersection advertised to
// the agent.
func (c *Client) PrimaryTools() []MCPTool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.primaryTools
}

// FallbackTools returns the tool list snapshot taken from the fallback
// endpoint at boot. Nil if no fallback is configured or DiscoverBothEndpoints
// wasn't called. Empty (non-nil) slice means the fallback was reached and
// returned zero tools.
func (c *Client) FallbackTools() []MCPTool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fallbackTools
}

// SetPrimaryTools records the tool list for the primary endpoint. Called by
// MultiClient.InitializeAll after a successful ListTools against primary.
// Exposed so tests can seed state without spinning up a real server.
func (c *Client) SetPrimaryTools(tools []MCPTool) {
	c.mu.Lock()
	c.primaryTools = tools
	c.mu.Unlock()
}

// SetFallbackTools records the tool list for the fallback endpoint. Called by
// MultiClient.InitializeAll after probing fallback at boot.
func (c *Client) SetFallbackTools(tools []MCPTool) {
	c.mu.Lock()
	c.fallbackTools = tools
	c.mu.Unlock()
}

// IntersectionTools returns tools present on BOTH primary and fallback.
// If fallback is not configured, returns primaryTools (preserves today's
// behavior — no fallback means no intersection constraint). If primary is
// nil, returns nil. Used by MultiClient to decide what to advertise.
func (c *Client) IntersectionTools() []MCPTool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return intersectionLocked(c.primaryTools, c.fallbackTools)
}

// ProbeEndpoint lists tools at the given URL without touching the receiver's
// active state. Used by MultiClient at boot to discover the fallback tool
// list so the intersection can be computed. Returns the discovered tool list
// and an error if the URL was unreachable or the handshake failed.
func (c *Client) ProbeEndpoint(ctx context.Context, url string) ([]MCPTool, error) {
	if url == "" {
		return nil, fmt.Errorf("ProbeEndpoint: empty url")
	}
	probe := NewClient(c.prefix, url, c.logger)
	if err := probe.Initialize(ctx); err != nil {
		return nil, err
	}
	return probe.ListTools(ctx)
}

// intersectionLocked computes the intersection of two tool lists by name.
// Pure function so it can be reused by tests without a Client. Caller is
// responsible for any lock acquisition.
func intersectionLocked(primary, fallback []MCPTool) []MCPTool {
	if len(primary) == 0 {
		return nil
	}
	if len(fallback) == 0 {
		// Empty fallback slice with fallback configured → strict intersection
		// is empty. We can't tell that case from "no fallback configured" here
		// — the caller (IntersectionTools on Client) handles that distinction
		// by only routing here when needed. As a library function we return
		// primary to preserve the "no constraint" semantic when fallback is
		// empty/nil.
		return primary
	}
	seen := make(map[string]bool, len(fallback))
	for _, t := range fallback {
		seen[t.Name] = true
	}
	out := make([]MCPTool, 0, len(primary))
	for _, t := range primary {
		if seen[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// truncateString truncates a string to maxLen for debug logging.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// Endpoint returns the active MCP server endpoint URL. Alias for
// ActiveEndpoint(); preserved for backward compatibility with callers that
// predate fallback support.
func (c *Client) Endpoint() string {
	return c.ActiveEndpoint()
}

// Instructions returns the instructions text from the MCP server's initialize response.
func (c *Client) Instructions() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.instructions
}

// Prefix returns the server prefix used for tool routing.
func (c *Client) Prefix() string {
	return c.prefix
}

// doRequest sends a JSON-RPC request and returns the response body and headers.
// Handles both plain JSON and SSE (text/event-stream) responses from the MCP server.
// Uses a semaphore to limit concurrent requests (max 2) to avoid overwhelming the server.
//
// The active endpoint + session ID are snapshotted under the read lock at the
// top; the HTTP round-trip happens outside the lock so a SwitchEndpoint can
// fire mid-request without deadlock. The snapshot means an in-flight request
// always finishes against the endpoint it started with — the next call sees
// the new endpoint.
func (c *Client) doRequest(ctx context.Context, req jsonRPCRequest) ([]byte, http.Header, error) {
	// Acquire semaphore slot (or respect context cancellation)
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	c.mu.RLock()
	endpoint := c.endpoint
	sid := c.sessionID
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Attach session ID if we have one
	if sid != "" {
		httpReq.Header.Set("Mcp-Session-Id", sid)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// Wrap with the endpoint so callers can see which URL returned the
		// error — load-bearing for SwitchEndpoint decisions post-failure.
		return nil, resp.Header, fmt.Errorf("HTTP %d from %s: %s",
			resp.StatusCode, endpoint, string(respBody[:min(len(respBody), 200)]))
	}

	// If the response is SSE (text/event-stream), extract the data payload
	ct := resp.Header.Get("Content-Type")
	if len(ct) >= 10 && ct[:10] == "text/event" {
		return parseSSEData(respBody), resp.Header, nil
	}

	return respBody, resp.Header, nil
}

// parseSSEData extracts the JSON-RPC response from an SSE response body.
// The MCP server may send multiple SSE events (progress notifications, then the result).
// We need the event with matching "id" field, not the first data line.
func parseSSEData(body []byte) []byte {
	var lastData []byte
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if after, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			// Check if this is a JSON-RPC response (has "result" or "error" field)
			if bytes.Contains(after, []byte(`"result":`)) || bytes.Contains(after, []byte(`"error":`)) {
				return after
			}
			lastData = after
		}
	}
	// No JSON-RPC response found — return last data line as fallback
	if lastData != nil {
		return lastData
	}
	return body
}

// MultiClient routes MCP tool calls to the correct server.
// Each server has a prefix (e.g. "mcp_" for web-research, "media_" for media-analysis).
// Tool calls are routed by matching the tool name to the registered prefix.
type MultiClient struct {
	mu          sync.RWMutex
	clients     map[string]*Client // prefix → client
	toolMap     map[string]string  // toolName → prefix (built from ListTools)
	cachedTools []MCPTool          // cached after InitializeAll
	unavailable map[string]bool    // prefix → health-flag (set by HealthMonitor)
	logger      *zap.Logger
}

// NewMultiClient creates a multi-server MCP client.
func NewMultiClient(logger *zap.Logger) *MultiClient {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MultiClient{
		clients:     make(map[string]*Client),
		toolMap:     make(map[string]string),
		unavailable: make(map[string]bool),
		logger:      logger,
	}
}

// InstructionRegistrar is set by the application startup to register MCP instructions
// with the prompt builder. This avoids a circular import between mcp and agents/common.
var InstructionRegistrar func(prefix, instructions string)

// registerInstructions calls the registered InstructionRegistrar if set.
func registerInstructions(prefix, instructions string) {
	if InstructionRegistrar != nil {
		InstructionRegistrar(prefix, instructions)
	}
}

// AddClient registers an MCP server with a tool name prefix.
func (m *MultiClient) AddClient(prefix string, client *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[prefix] = client
}

// HasClient checks if a server with the given prefix is registered.
func (m *MultiClient) HasClient(prefix string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.clients[prefix]
	return ok
}

// ClientForPrefix returns the registered client for a server prefix, or nil
// if no server is registered under that prefix. Used by capability resolution
// to health-check a specific tier (e.g. "is the web server up?").
func (m *MultiClient) ClientForPrefix(prefix string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clients[prefix]
	if !ok {
		return nil
	}
	return c
}

// Prefixes returns the list of registered server prefixes. Used by HealthMonitor
// to iterate clients withoutSnapshotting the underlying map.
func (m *MultiClient) Prefixes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.clients))
	for p := range m.clients {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// MarkUnavailable flags a server prefix as failing. The MultiClient keeps the
// client registered (toolMap intact) so call paths still surface errors; the
// flag is advisory state for the management UI + HealthMonitor bookkeeping.
func (m *MultiClient) MarkUnavailable(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unavailable[prefix] = true
}

// MarkAvailable clears the unavailable flag. Called by HealthMonitor on recovery.
func (m *MultiClient) MarkAvailable(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.unavailable, prefix)
}

// IsUnavailable reports whether a prefix is currently flagged unavailable.
func (m *MultiClient) IsUnavailable(prefix string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.unavailable[prefix]
}

// RemoveClient removes a server by prefix and cleans up its tool entries.
func (m *MultiClient) RemoveClient(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, prefix)
	delete(m.unavailable, prefix)
	// Remove tool routing entries for this prefix
	for toolName, p := range m.toolMap {
		if p == prefix {
			delete(m.toolMap, toolName)
		}
	}
	// Rebuild cached tools without this server's tools
	var filtered []MCPTool
	for _, t := range m.cachedTools {
		if m.toolMap[t.Name] != "" {
			filtered = append(filtered, t)
		}
	}
	m.cachedTools = filtered
}

// InitializeAll initializes all registered MCP servers and discovers their tools.
// Populates both toolMap (tool → prefix routing) and cachedTools (full tool list).
// Also captures server instructions and registers them with the prompt builder.
//
// When a client has a fallback endpoint configured:
//  1. Primary is probed first. If primary succeeds, fallback is probed in a
//     one-shot ProbeEndpoint (no state pollution — uses a throwaway client).
//  2. If primary is unreachable but fallback is reachable, the client is
//     switched to fallback (activeEndpoint = fallback) so subsequent calls
//     route correctly.
//  3. Tools advertised are the INTERSECTION of primary and fallback tools
//     when both are reachable. When only one is reachable, that one's tools
//     are advertised as-is (degraded but functional).
//  4. When both are unreachable, the prefix is skipped (existing behavior).
func (m *MultiClient) InitializeAll(ctx context.Context) error {
	m.cachedTools = nil
	seen := make(map[string]bool)
	for prefix, client := range m.clients {
		advertised, err := m.bootOneClient(ctx, prefix, client)
		if err != nil {
			continue
		}
		for _, t := range advertised {
			if seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			m.toolMap[t.Name] = prefix
			m.cachedTools = append(m.cachedTools, t)
		}
	}
	return nil
}

// bootOneClient initializes a single client at boot, handling fallback
// discovery and intersection. Returns the tool list to advertise. Returns an
// error only when neither primary nor fallback is reachable.
func (m *MultiClient) bootOneClient(ctx context.Context, prefix string, client *Client) ([]MCPTool, error) {
	primaryErr := client.Initialize(ctx)
	if primaryErr != nil {
		m.logger.Warn("MCP server initialize failed",
			zap.String("prefix", prefix), zap.Error(primaryErr))
	}

	// Capture primary tools if initialize succeeded.
	var primaryTools []MCPTool
	if primaryErr == nil {
		// Register instructions with the prompt builder.
		if client.Instructions() != "" {
			registerInstructions(prefix, client.Instructions())
		}
		tools, err := client.ListTools(ctx)
		if err != nil {
			m.logger.Warn("MCP tools/list failed",
				zap.String("prefix", prefix), zap.Error(err))
			primaryErr = err // treat as primary-unreachable for fallback decision
		} else {
			primaryTools = tools
			client.SetPrimaryTools(tools)
		}
	}

	// Discover fallback tools (if configured) via one-shot probe. Doesn't
	// touch the active endpoint state.
	var fallbackTools []MCPTool
	if client.HasFallback() {
		ft, ferr := client.ProbeEndpoint(ctx, client.FallbackEndpoint())
		if ferr != nil {
			m.logger.Warn("MCP fallback probe failed",
				zap.String("prefix", prefix),
				zap.String("fallback", client.FallbackEndpoint()),
				zap.Error(ferr))
		} else {
			fallbackTools = ft
			client.SetFallbackTools(ft)
		}
	}

	// Decide what to advertise + whether to switch to fallback.
	switch {
	case primaryErr == nil && len(fallbackTools) > 0:
		// Both reachable — advertise intersection, primary stays active.
		intersected := intersectionLocked(primaryTools, fallbackTools)
		m.logger.Info("MCP server initialized with fallback",
			zap.String("prefix", prefix),
			zap.Int("primary_tools", len(primaryTools)),
			zap.Int("fallback_tools", len(fallbackTools)),
			zap.Int("advertised", len(intersected)))
		return intersected, nil

	case primaryErr == nil && !client.HasFallback():
		// No fallback configured — advertise primary tools. This is the
		// today's-behavior path; preserving it explicitly so the switch
		// statement is exhaustive.
		m.logger.Info("MCP server initialized (no fallback)",
			zap.String("prefix", prefix),
			zap.Int("tools", len(primaryTools)))
		return primaryTools, nil

	case primaryErr == nil && client.HasFallback() && len(fallbackTools) == 0:
		// Primary up, fallback configured but unreachable. Advertise primary
		// tools; runtime fallback path is dead but the primary still serves.
		m.logger.Warn("MCP server: primary up, fallback unreachable — advertising primary tools",
			zap.String("prefix", prefix))
		return primaryTools, nil

	case primaryErr != nil && len(fallbackTools) > 0:
		// Primary down, fallback up. Switch to fallback, advertise its tools.
		if swErr := client.SwitchEndpoint(client.FallbackEndpoint(), "primary unreachable at boot"); swErr != nil {
			m.logger.Error("MCP boot switch to fallback failed",
				zap.String("prefix", prefix), zap.Error(swErr))
			return nil, swErr
		}
		if initErr := client.Initialize(ctx); initErr != nil {
			m.logger.Error("MCP boot re-init on fallback failed",
				zap.String("prefix", prefix), zap.Error(initErr))
			return nil, initErr
		}
		if client.Instructions() != "" {
			registerInstructions(prefix, client.Instructions())
		}
		m.logger.Info("MCP server initialized on fallback (primary unreachable)",
			zap.String("prefix", prefix),
			zap.Int("fallback_tools", len(fallbackTools)))
		return fallbackTools, nil

	default:
		// Both unreachable OR primary down with no fallback configured.
		// Caller skips this prefix.
		if client.HasFallback() {
			m.logger.Error("MCP server unreachable on both primary and fallback at boot",
				zap.String("prefix", prefix),
				zap.String("primary", client.PrimaryEndpoint()),
				zap.String("fallback", client.FallbackEndpoint()))
		}
		return nil, fmt.Errorf("both endpoints unreachable")
	}
}

// AllTools returns all tools from all registered servers, with their original names.
// Returns cached tools from InitializeAll — no HTTP calls. Call ResetTools() then
// InitializeAll() to refresh.
func (m *MultiClient) AllTools() []MCPTool {
	return m.cachedTools
}

// ResetTools clears the cached tool list. The next call to AllTools will be empty
// until InitializeAll is called again.
func (m *MultiClient) ResetTools() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedTools = nil
}

// RefreshTools re-runs ListTools on every registered client and rebuilds the
// routing map. Used after org activation registers new servers mid-session
// (so their tools become visible without a process restart) and after the
// HealthMonitor recovers a previously-down server. Safe to call concurrently
// with CallTool — CallTool takes the read lock, RefreshTools takes write.
func (m *MultiClient) RefreshTools(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	seen := make(map[string]bool)
	var newCached []MCPTool
	for prefix, client := range m.clients {
		tools, err := client.ListTools(ctx)
		if err != nil {
			m.logger.Warn("MCP RefreshTools: ListTools failed",
				zap.String("prefix", prefix), zap.Error(err))
			continue
		}
		for _, t := range tools {
			if seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			m.toolMap[t.Name] = prefix
			newCached = append(newCached, t)
		}
		// Clear unavailable flag — a successful ListTools means the server is back.
		delete(m.unavailable, prefix)
	}
	m.cachedTools = newCached
	return nil
}

// CallTool routes a tool call to the correct server based on the tool name.
func (m *MultiClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	m.mu.RLock()
	prefix, ok := m.toolMap[name]
	if !ok {
		m.mu.RUnlock()
		return "", fmt.Errorf("MCP tool %q not found in any server", name)
	}
	client, ok := m.clients[prefix]
	if !ok {
		m.mu.RUnlock()
		return "", fmt.Errorf("MCP server %q not available", prefix)
	}
	m.mu.RUnlock()
	return client.CallTool(ctx, name, args)
}

// IsAvailable returns true if at least one server is reachable.
func (m *MultiClient) IsAvailable() bool {
	for _, client := range m.clients {
		if client.IsAvailable() {
			return true
		}
	}
	return false
}

// ClientCount returns the number of registered MCP servers.
func (m *MultiClient) ClientCount() int {
	return len(m.clients)
}

// ClientForTool returns the MCP client that handles the given tool name.
// Returns nil if the tool is not registered.
func (m *MultiClient) ClientForTool(toolName string) *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix, ok := m.toolMap[toolName]
	if !ok {
		return nil
	}
	return m.clients[prefix]
}

// HasTool checks if a tool name is registered in any server.
func (m *MultiClient) HasTool(toolName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.toolMap[toolName]
	return ok
}

// ServerToolNames returns all tool names registered under the given MCP server prefix.
func (m *MultiClient) ServerToolNames(prefix string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []string
	for name, p := range m.toolMap {
		if p == prefix {
			names = append(names, name)
		}
	}
	return names
}

// PrimaryClient returns the first registered client (for backward compatibility).
func (m *MultiClient) PrimaryClient() *Client {
	for _, c := range m.clients {
		return c
	}
	return nil
}

// MCPServerInfo describes a registered MCP server for the management UI.
type MCPServerInfo struct {
	Prefix           string   `json:"prefix"`
	Endpoint         string   `json:"endpoint"`          // active endpoint (legacy field)
	ActiveEndpoint   string   `json:"activeEndpoint"`    // currently in use
	PrimaryEndpoint  string   `json:"primaryEndpoint"`   // declared primary
	FallbackEndpoint string   `json:"fallbackEndpoint"`  // declared fallback (empty = none)
	Available        bool     `json:"available"`
	ToolCount        int      `json:"toolCount"`
	Tools            []string `json:"tools"`
}

// ServersInfo returns metadata about all registered MCP servers.
func (m *MultiClient) ServersInfo() []MCPServerInfo {
	var servers []MCPServerInfo
	for prefix, client := range m.clients {
		info := MCPServerInfo{
			Prefix:           prefix,
			Endpoint:         client.ActiveEndpoint(),
			ActiveEndpoint:   client.ActiveEndpoint(),
			PrimaryEndpoint:  client.PrimaryEndpoint(),
			FallbackEndpoint: client.FallbackEndpoint(),
			Available:        client.IsAvailable(),
			Tools:            m.ServerToolNames(prefix),
		}
		info.ToolCount = len(info.Tools)
		servers = append(servers, info)
	}
	// Sort by prefix for deterministic output
	sort.Slice(servers, func(i, j int) bool { return servers[i].Prefix < servers[j].Prefix })
	return servers
}
