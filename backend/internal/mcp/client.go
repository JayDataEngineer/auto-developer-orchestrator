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
	"time"

	"go.uber.org/zap"
)

// Client is an HTTP client for the MCP research server.
type Client struct {
	endpoint     string
	prefix       string // server prefix for tool routing and instructions
	httpClient   *http.Client
	logger       *zap.Logger
	sessionID    string        // Mcp-Session-Id from initialize response
	instructions string        // instructions text from initialize response
	sem          chan struct{} // concurrency limiter — 2 concurrent requests max
}

// NewClient creates a new MCP client. The endpoint should be the MCP server URL.
// The prefix identifies this server for tool routing and instruction registration.
// If endpoint is empty, it falls back to the MCP_RESEARCH_ENDPOINT env var.
func NewClient(prefix, endpoint string, logger *zap.Logger) *Client {
	if endpoint == "" {
		endpoint = os.Getenv("MCP_RESEARCH_ENDPOINT")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		prefix:   prefix,
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // MCP tools can be slow (AI vision models, search, scrape)
		},
		logger: logger,
		sem:    make(chan struct{}, 2), // max 2 concurrent MCP requests
	}
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

	// Capture session ID from response headers for subsequent requests
	if sid := headers.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
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
		c.instructions = initResult.Instructions
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
		zap.String("sessionId", c.sessionID),
		zap.String("body", string(respBody)),
	)
	return nil
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
// Fast-path: if a session is already established, return true without
// re-initializing. This avoids competing for the semaphore with active
// worker tool calls and prevents overwriting a valid session ID.
func (c *Client) IsAvailable() bool {
	if c.sessionID != "" {
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

// truncateString truncates a string to maxLen for debug logging.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// Endpoint returns the MCP server endpoint URL.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// Instructions returns the instructions text from the MCP server's initialize response.
func (c *Client) Instructions() string {
	return c.instructions
}

// Prefix returns the server prefix used for tool routing.
func (c *Client) Prefix() string {
	return c.prefix
}

// doRequest sends a JSON-RPC request and returns the response body and headers.
// Handles both plain JSON and SSE (text/event-stream) responses from the MCP server.
// Uses a semaphore to limit concurrent requests (max 2) to avoid overwhelming the server.
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Attach session ID if we have one
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
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
		return nil, resp.Header, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 200)]))
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
	clients     map[string]*Client // prefix → client
	toolMap     map[string]string  // toolName → prefix (built from ListTools)
	cachedTools []MCPTool          // cached after InitializeAll
	logger      *zap.Logger
}

// NewMultiClient creates a multi-server MCP client.
func NewMultiClient(logger *zap.Logger) *MultiClient {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MultiClient{
		clients: make(map[string]*Client),
		toolMap: make(map[string]string),
		logger:  logger,
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
	m.clients[prefix] = client
}

// HasClient checks if a server with the given prefix is registered.
func (m *MultiClient) HasClient(prefix string) bool {
	_, ok := m.clients[prefix]
	return ok
}

// ClientForPrefix returns the registered client for a server prefix, or nil
// if no server is registered under that prefix. Used by capability resolution
// to health-check a specific tier (e.g. "is the web server up?").
func (m *MultiClient) ClientForPrefix(prefix string) *Client {
	c, ok := m.clients[prefix]
	if !ok {
		return nil
	}
	return c
}

// RemoveClient removes a server by prefix and cleans up its tool entries.
func (m *MultiClient) RemoveClient(prefix string) {
	delete(m.clients, prefix)
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
func (m *MultiClient) InitializeAll(ctx context.Context) error {
	m.cachedTools = nil
	seen := make(map[string]bool)
	for prefix, client := range m.clients {
		if err := client.Initialize(ctx); err != nil {
			m.logger.Warn("MCP server initialize failed", zap.String("prefix", prefix), zap.Error(err))
			continue
		}

		// Register instructions with the prompt builder
		if client.Instructions() != "" {
			registerInstructions(prefix, client.Instructions())
		}

		tools, err := client.ListTools(ctx)
		if err != nil {
			m.logger.Warn("MCP tools/list failed", zap.String("prefix", prefix), zap.Error(err))
			continue
		}
		for _, t := range tools {
			if seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			m.toolMap[t.Name] = prefix
			m.cachedTools = append(m.cachedTools, t)
		}
		m.logger.Info("MCP server initialized", zap.String("prefix", prefix), zap.Int("tools", len(tools)))
	}
	return nil
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
	m.cachedTools = nil
}

// CallTool routes a tool call to the correct server based on the tool name.
func (m *MultiClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	prefix, ok := m.toolMap[name]
	if !ok {
		return "", fmt.Errorf("MCP tool %q not found in any server", name)
	}
	client, ok := m.clients[prefix]
	if !ok {
		return "", fmt.Errorf("MCP server %q not available", prefix)
	}
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
	prefix, ok := m.toolMap[toolName]
	if !ok {
		return nil
	}
	return m.clients[prefix]
}

// HasTool checks if a tool name is registered in any server.
func (m *MultiClient) HasTool(toolName string) bool {
	_, ok := m.toolMap[toolName]
	return ok
}

// ServerToolNames returns all tool names registered under the given MCP server prefix.
func (m *MultiClient) ServerToolNames(prefix string) []string {
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
	Prefix    string   `json:"prefix"`
	Endpoint  string   `json:"endpoint"`
	Available bool     `json:"available"`
	ToolCount int      `json:"toolCount"`
	Tools     []string `json:"tools"`
}

// ServersInfo returns metadata about all registered MCP servers.
func (m *MultiClient) ServersInfo() []MCPServerInfo {
	var servers []MCPServerInfo
	for prefix, client := range m.clients {
		info := MCPServerInfo{
			Prefix:    prefix,
			Endpoint:  client.Endpoint(),
			Available: client.IsAvailable(),
			Tools:     m.ServerToolNames(prefix),
		}
		info.ToolCount = len(info.Tools)
		servers = append(servers, info)
	}
	// Sort by prefix for deterministic output
	sort.Slice(servers, func(i, j int) bool { return servers[i].Prefix < servers[j].Prefix })
	return servers
}
