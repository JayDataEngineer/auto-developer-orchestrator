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
	"sync"
	"time"

	"go.uber.org/zap"
)

// Client is an HTTP client for the MCP research server.
type Client struct {
	endpoint   string
	httpClient *http.Client
	sessionID  string
	mu         sync.Mutex
	logger     *zap.Logger
}

// NewClient creates a new MCP client. The endpoint should be the MCP server URL.
// If endpoint is empty, it falls back to the MCP_RESEARCH_ENDPOINT env var,
// then to "http://100.121.245.20:8327/mcp".
func NewClient(endpoint string, logger *zap.Logger) *Client {
	if endpoint == "" {
		endpoint = os.Getenv("MCP_RESEARCH_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = "http://100.121.245.20:8327/mcp"
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
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

// Initialize performs the MCP handshake. The session ID from the response
// header is stored and used for all subsequent requests.
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

	// Capture session ID from response header
	if sid := headers.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
		c.logger.Info("MCP session established", zap.String("session_id", sid))
	} else {
		c.logger.Warn("MCP initialize succeeded but no session ID in response")
	}

	// Send initialized notification (fire-and-forget, no ID)
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	_, _, _ = c.doRequest(ctx, notif)

	c.logger.Debug("MCP initialize response", zap.String("body", string(respBody)))
	return nil
}

// CallTool calls an MCP tool by name with the given arguments.
// Returns the text content from the tool result.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	c.mu.Lock()
	sessionID := c.sessionID
	c.mu.Unlock()

	// If no session, try to initialize first
	if sessionID == "" {
		if err := c.Initialize(ctx); err != nil {
			return "", fmt.Errorf("MCP session not initialized: %w", err)
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
	})
}

// Scrape fetches a URL and returns its content as clean markdown.
func (c *Client) Scrape(ctx context.Context, url string) (string, error) {
	return c.CallTool(ctx, "scrape", map[string]any{
		"url": url,
	})
}

// IsAvailable returns true if the MCP server is reachable.
func (c *Client) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Initialize(ctx) == nil
}

// Endpoint returns the MCP server endpoint URL.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// doRequest sends a JSON-RPC request and returns the response body and headers.
// Handles both plain JSON and SSE (text/event-stream) responses from the MCP server.
func (c *Client) doRequest(ctx context.Context, req jsonRPCRequest) ([]byte, http.Header, error) {
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
	c.mu.Lock()
	sid := c.sessionID
	c.mu.Unlock()
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
		return nil, resp.Header, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 200)]))
	}

	// If the response is SSE (text/event-stream), extract the data payload
	ct := resp.Header.Get("Content-Type")
	if len(ct) >= 10 && ct[:10] == "text/event" {
		return parseSSEData(respBody), resp.Header, nil
	}

	return respBody, resp.Header, nil
}

// parseSSEData extracts JSON from an SSE response body.
// Looks for "data: " lines and returns the first one as bytes.
func parseSSEData(body []byte) []byte {
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if after, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			return after
		}
	}
	// No SSE data lines found — return raw body as fallback
	return body
}
