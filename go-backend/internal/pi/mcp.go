package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// MCPTransportType defines the transport for an MCP server
type MCPTransportType string

const (
	MCPTransportStdio MCPTransportType = "stdio"
	MCPTransportSSE   MCPTransportType = "sse"
)

// MCPServerConfig is the configuration for a single MCP server
type MCPServerConfig struct {
	// Transport is "stdio" or "sse"
	Transport MCPTransportType `json:"transport"`
	// Command is the executable (stdio only)
	Command string `json:"command,omitempty"`
	// Args are the command arguments (stdio only)
	Args []string `json:"args,omitempty"`
	// Env are environment variables for the command (stdio only)
	Env map[string]string `json:"env,omitempty"`
	// URL is the SSE endpoint URL (SSE only)
	URL string `json:"url,omitempty"`
}

// MCPClient manages connections to MCP servers
type MCPClient struct {
	mu       sync.Mutex
	servers  map[string]*MCPServerInstance
	logger   *zap.Logger
	nextID   int
}

// MCPServerInstance holds a running MCP server connection
type MCPServerInstance struct {
	Name     string
	Config   MCPServerConfig
	Stdin    io.WriteCloser
	Stdout   io.ReadCloser
	Stderr   io.ReadCloser
	Cmd      *exec.Cmd
	HTTPClient *http.Client
	BaseURL  string
	Tools    []MCPTool
	initialized bool
	responses map[int]chan json.RawMessage
	responsesMu sync.Mutex
	idGen    int
	logger   *zap.Logger
}

// MCPTool represents a tool exposed by an MCP server
type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
	ServerName  string      `json:"server_name"`
}

// NewMCPClient creates a new MCP client
func NewMCPClient(logger *zap.Logger) *MCPClient {
	return &MCPClient{
		servers: make(map[string]*MCPServerInstance),
		logger:  logger,
	}
}

// StartServer starts an MCP server connection
func (mc *MCPClient) StartServer(ctx context.Context, name string, cfg MCPServerConfig) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if _, exists := mc.servers[name]; exists {
		return fmt.Errorf("MCP server %s already running", name)
	}

	inst := &MCPServerInstance{
		Name:      name,
		Config:    cfg,
		responses: make(map[int]chan json.RawMessage),
		logger:    mc.logger.With(zap.String("mcp_server", name)),
	}

	switch cfg.Transport {
	case MCPTransportStdio:
		if err := inst.startStdIO(ctx); err != nil {
			return err
		}
	case MCPTransportSSE:
		if err := inst.startSSE(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown MCP transport: %s", cfg.Transport)
	}

	// Initialize the MCP session
	if err := inst.initialize(ctx); err != nil {
		inst.Close()
		return fmt.Errorf("MCP initialize failed: %w", err)
	}

	// Fetch available tools
	if err := inst.listTools(ctx); err != nil {
		mc.logger.Warn("MCP listTools failed (non-fatal)", zap.String("server", name), zap.Error(err))
	}

	mc.servers[name] = inst
	mc.logger.Info("MCP server started",
		zap.String("name", name),
		zap.String("transport", string(cfg.Transport)),
		zap.Int("tools", len(inst.Tools)),
	)

	return nil
}

// CallTool calls a tool on an MCP server
func (mc *MCPClient) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (string, error) {
	mc.mu.Lock()
	inst, ok := mc.servers[serverName]
	mc.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("MCP server %s not running", serverName)
	}

	request := map[string]interface{}{
		"method": "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	var result json.RawMessage
	var err error

	switch inst.Config.Transport {
	case MCPTransportStdio:
		result, err = inst.sendRequest(ctx, request)
	case MCPTransportSSE:
		result, err = inst.sendSSERequest(ctx, request)
	}

	if err != nil {
		return "", err
	}

	// Extract the result content
	var response struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("MCP tool response parse error: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("MCP tool error: %s", response.Error.Message)
	}

	var parts []string
	for _, c := range response.Result.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}

	if response.Result.IsError && len(parts) == 0 {
		return "", fmt.Errorf("MCP tool returned error")
	}

	return strings.Join(parts, "\n"), nil
}

// ListServers returns running MCP server names
func (mc *MCPClient) ListServers() []string {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	names := make([]string, 0, len(mc.servers))
	for name := range mc.servers {
		names = append(names, name)
	}
	return names
}

// ListTools returns all tools from all MCP servers
func (mc *MCPClient) ListTools() []MCPTool {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	var allTools []MCPTool
	for _, inst := range mc.servers {
		allTools = append(allTools, inst.Tools...)
	}
	return allTools
}

// CloseAll shuts down all MCP servers
func (mc *MCPClient) CloseAll() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for name, inst := range mc.servers {
		inst.Close()
		delete(mc.servers, name)
	}
}

// ----- StdIO Transport -----

func (inst *MCPServerInstance) startStdIO(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, inst.Config.Command, inst.Config.Args...)
	env := make([]string, 0)
	for k, v := range inst.Config.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = append(env, cmd.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdio stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdio stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stdio stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("stdio command start: %w", err)
	}

	inst.Cmd = cmd
	inst.Stdin = stdin
	inst.Stdout = stdout
	inst.Stderr = stderr

	// Read stderr in background
	go func() {
		scanner := bufio.NewScanner(inst.Stderr)
		for scanner.Scan() {
			inst.logger.Debug("MCP stderr", zap.String("line", scanner.Text()))
		}
	}()

	// Read stdout in background and dispatch responses
	go inst.readStdIOResponses()

	return nil
}

func (inst *MCPServerInstance) readStdIOResponses() {
	scanner := bufio.NewScanner(inst.Stdout)
	// Increase scanner buffer for large JSON responses
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *int            `json:"id,omitempty"`
			Method  string          `json:"method,omitempty"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   json.RawMessage `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			inst.logger.Debug("MCP parse error", zap.String("line", line[:min(len(line), 200)]))
			continue
		}

		if msg.ID != nil {
			inst.responsesMu.Lock()
			ch, ok := inst.responses[*msg.ID]
			if ok {
				ch <- json.RawMessage(line)
				delete(inst.responses, *msg.ID)
			}
			inst.responsesMu.Unlock()
		}
	}
}

func (inst *MCPServerInstance) sendRequest(ctx context.Context, request map[string]interface{}) (json.RawMessage, error) {
	inst.responsesMu.Lock()
	inst.idGen++
	id := inst.idGen
	ch := make(chan json.RawMessage, 1)
	inst.responses[id] = ch
	inst.responsesMu.Unlock()

	request["id"] = id
	request["jsonrpc"] = "2.0"

	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	if _, err := inst.Stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("stdio write: %w", err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("MCP request timeout")
	}
}

// ----- SSE Transport -----

func (inst *MCPServerInstance) startSSE(ctx context.Context) error {
	inst.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	inst.BaseURL = strings.TrimSuffix(inst.Config.URL, "/")
	return nil
}

func (inst *MCPServerInstance) sendSSERequest(ctx context.Context, request map[string]interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", inst.BaseURL+"/message", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := inst.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("SSE read: %w", err)
	}

	return json.RawMessage(body), nil
}

// ----- MCP Protocol -----

func (inst *MCPServerInstance) initialize(ctx context.Context) error {
	request := map[string]interface{}{
		"method": "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "auto-developer-orchestrator",
				"version": "1.0.0",
			},
		},
	}

	var result json.RawMessage
	var err error

	switch inst.Config.Transport {
	case MCPTransportStdio:
		result, err = inst.sendRequest(ctx, request)
	case MCPTransportSSE:
		result, err = inst.sendSSERequest(ctx, request)
	}

	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	inst.initialized = true
	inst.logger.Debug("MCP server initialized", zap.String("response", string(result)[:min(len(result), 200)]))

	// Send initialized notification
	notif := map[string]interface{}{
		"method": "notifications/initialized",
		"params": map[string]interface{}{},
	}
	switch inst.Config.Transport {
	case MCPTransportStdio:
		data, _ := json.Marshal(notif)
		inst.Stdin.Write(append(data, '\n'))
	}

	return nil
}

func (inst *MCPServerInstance) listTools(ctx context.Context) error {
	request := map[string]interface{}{
		"method": "tools/list",
		"params": map[string]interface{}{},
	}

	var result json.RawMessage
	var err error

	switch inst.Config.Transport {
	case MCPTransportStdio:
		result, err = inst.sendRequest(ctx, request)
	case MCPTransportSSE:
		result, err = inst.sendSSERequest(ctx, request)
	}

	if err != nil {
		return err
	}

	var resp struct {
		Result struct {
			Tools []struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				InputSchema interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("listTools parse: %w", err)
	}

	for _, t := range resp.Result.Tools {
		inst.Tools = append(inst.Tools, MCPTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			ServerName:  inst.Name,
		})
	}

	return nil
}

// Close shuts down an MCP server instance
func (inst *MCPServerInstance) Close() {
	if inst.Stdin != nil {
		inst.Stdin.Close()
	}
	if inst.Cmd != nil && inst.Cmd.Process != nil {
		inst.Cmd.Process.Kill()
		inst.Cmd.Wait()
	}
}

// StartMCPServers loads and starts all MCP servers from the project's .pi/mcp-servers.json
func (mc *MCPClient) StartMCPServers(ctx context.Context, projectDir string) error {
	configPath := filepath.Join(projectDir, ".pi", "mcp-servers.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no config file
		}
		return err
	}

	var config struct {
		MCPServers map[string]MCPServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse mcp-servers.json: %w", err)
	}

	for name, cfg := range config.MCPServers {
		if cfg.Transport == "" {
			cfg.Transport = MCPTransportStdio
		}
		if err := mc.StartServer(ctx, name, cfg); err != nil {
			mc.logger.Warn("MCP server start failed", zap.String("name", name), zap.Error(err))
		}
	}

	return nil
}

// GetMCPClient returns the MCP client for direct access
func (c *PiClient) GetMCPClient() *MCPClient {
	return c.mcpClient
}

// GetHookManager returns the hook manager for direct access
func (c *PiClient) GetHookManager() *HookManager {
	return c.hookManager
}
