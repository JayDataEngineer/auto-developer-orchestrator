package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// ToolsHandler provides direct tool execution for SDK consumers.
// Bypasses the agent loop — callers invoke tools by name with args.
type ToolsHandler struct {
	manager    *sandbox.Manager
	mcpMulti   *mcp.MultiClient
	mcpClient  *mcp.Client
	logger     *zap.Logger
}

// NewToolsHandler creates a new direct tool execution handler.
func NewToolsHandler(mgr *sandbox.Manager, mcpMulti *mcp.MultiClient, mcpClient *mcp.Client, logger *zap.Logger) *ToolsHandler {
	return &ToolsHandler{
		manager:   mgr,
		mcpMulti:  mcpMulti,
		mcpClient: mcpClient,
		logger:    logger,
	}
}

// SetMCP wires the MCP clients after the multi-client has finished initializing.
// Called from app.go's initMCP so /api/tools and /api/tools/exec see the same
// prefixed tool names the agent loop sees.
func (h *ToolsHandler) SetMCP(mcpMulti *mcp.MultiClient, mcpClient *mcp.Client) {
	h.mcpMulti = mcpMulti
	h.mcpClient = mcpClient
}

// toolsExecRequest is the request body for POST /api/tools/exec.
type toolsExecRequest struct {
	Tool      string                 `json:"tool"`
	Args      map[string]interface{} `json:"args,omitempty"`
	SandboxID string                 `json:"sandbox_id,omitempty"`
	Project   string                 `json:"project,omitempty"`
	Timeout   int                    `json:"timeout,omitempty"` // seconds, default 60
}

// ExecTool handles POST /api/tools/exec — direct tool execution.
func (h *ToolsHandler) ExecTool(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[toolsExecRequest](w, r)
	if !ok { return }

	if req.Tool == "" {
		JSONError(w, "missing 'tool' field", http.StatusBadRequest)
		return
	}

	timeout := 60
	if req.Timeout > 0 && req.Timeout <= 600 {
		timeout = req.Timeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
	defer cancel()

	var result interface{}
	var err error

	switch {
	case strings.HasPrefix(req.Tool, "mcp_") || h.isMCPTool(req.Tool):
		result, err = h.execMCP(ctx, req.Tool, req.Args)
	case req.Tool == "bash":
		result, err = h.execBash(ctx, req.SandboxID, req.Args)
	case req.Tool == "file_read":
		result, err = h.execFileRead(ctx, req.SandboxID, req.Args)
	case req.Tool == "file_write":
		result, err = h.execFileWrite(ctx, req.SandboxID, req.Args)
	default:
		JSONError(w, fmt.Sprintf("unsupported tool: %s (supported: mcp_*, bash, file_read, file_write)", req.Tool), http.StatusBadRequest)
		return
	}

	if err != nil {
		h.logger.Warn("Tool exec failed", zap.String("tool", req.Tool), zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"tool":    req.Tool,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tool":    req.Tool,
		"result":  result,
	})
}

// ToolsList handles GET /api/tools — lists available tools.
func (h *ToolsHandler) ToolsList(w http.ResponseWriter, r *http.Request) {
	tools := []map[string]string{}

	// Sandbox tools
	if h.manager != nil {
		tools = append(tools,
			map[string]string{"name": "bash", "category": "sandbox", "description": "Execute a bash command in a sandbox"},
			map[string]string{"name": "file_read", "category": "sandbox", "description": "Read a file from a sandbox"},
			map[string]string{"name": "file_write", "category": "sandbox", "description": "Write a file to a sandbox"},
		)
	}

	// MCP tools
	if h.mcpMulti != nil {
		allTools := h.mcpMulti.AllTools()
		for _, t := range allTools {
			prefix := ""
			if c := h.mcpMulti.ClientForTool(t.Name); c != nil {
				prefix = c.Prefix()
			}
			displayName := t.Name
			if prefix != "" {
				displayName = fmt.Sprintf("mcp__%s__%s", prefix, t.Name)
			}
			tools = append(tools, map[string]string{
				"name":        displayName,
				"category":    "mcp",
				"description": t.Description,
			})
		}
	} else if h.mcpClient != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if mcpTools, err := h.mcpClient.ListTools(ctx); err == nil {
			for _, t := range mcpTools {
				tools = append(tools, map[string]string{
					"name":        t.Name,
					"category":    "mcp",
					"description": t.Description,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tools": tools,
		"count": len(tools),
	})
}

// --- Tool implementations ---

// mcpToolNamePattern is mcp__{server}__{rawname} (double-underscore separators).
// Legacy single-underscore form (mcp_{rawname}) is also accepted for backwards compat.
var mcpToolNamePattern = regexp.MustCompile(`^mcp__(?P<server>[^_]+)__(?P<name>.+)$`)

// stripMCPPrefix returns the raw MCP tool name (as the MultiClient knows it)
// from either the new prefixed form (mcp__web__research → research) or the
// legacy form (mcp_research → research). Returns the input unchanged if it
// doesn't match either pattern.
func stripMCPPrefix(toolName string) string {
	if m := mcpToolNamePattern.FindStringSubmatch(toolName); m != nil {
		return m[2]
	}
	return strings.TrimPrefix(toolName, "mcp_")
}

func (h *ToolsHandler) isMCPTool(name string) bool {
	if h.mcpMulti != nil {
		raw := stripMCPPrefix(name)
		return h.mcpMulti.HasTool(raw) || h.mcpMulti.HasTool(name)
	}
	return false
}

func (h *ToolsHandler) execMCP(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	mcpName := stripMCPPrefix(toolName)

	// Try multi-client first
	if h.mcpMulti != nil {
		if h.mcpMulti.HasTool(mcpName) {
			result, err := h.mcpMulti.CallTool(ctx, mcpName, args)
			if err != nil {
				return nil, fmt.Errorf("MCP %s failed: %w", mcpName, err)
			}
			return result, nil
		}
	}

	// Fallback to single client
	if h.mcpClient == nil {
		return nil, fmt.Errorf("MCP tool %s not available — no MCP client configured", toolName)
	}
	result, err := h.mcpClient.CallTool(ctx, mcpName, args)
	if err != nil {
		return nil, fmt.Errorf("MCP %s failed: %w", mcpName, err)
	}
	return result, nil
}

func (h *ToolsHandler) execBash(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	if h.manager == nil {
		return nil, fmt.Errorf("sandbox manager not available")
	}
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox_id required for bash tool")
	}

	command, _ := args["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("missing 'command' arg")
	}

	output, err := h.manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", command})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"output": output,
	}, nil
}

func (h *ToolsHandler) execFileRead(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	if h.manager == nil {
		return nil, fmt.Errorf("sandbox manager not available")
	}
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox_id required for file_read tool")
	}

	path, _ := args["file_path"].(string)
	if path == "" {
		path, _ = args["path"].(string)
	}
	if path == "" {
		return nil, fmt.Errorf("missing 'file_path' arg")
	}

	offset, _ := args["offset"].(float64)
	limit, _ := args["limit"].(float64)

	// Use cat with line numbers
	cmd := fmt.Sprintf("cat -n '%s'", sandbox.ShellEscape(path))
	if offset > 0 || limit > 0 {
		start := int(offset)
		if start < 1 {
			start = 1
		}
		end := int(limit)
		if end == 0 {
			end = start + 2000
		} else {
			end = start + end
		}
		cmd = fmt.Sprintf("sed -n '%d,%dp' '%s'", start, end, sandbox.ShellEscape(path))
	}

	output, err := h.manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", cmd})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"content": output,
		"path":    path,
	}, nil
}

func (h *ToolsHandler) execFileWrite(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	if h.manager == nil {
		return nil, fmt.Errorf("sandbox manager not available")
	}
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox_id required for file_write tool")
	}

	path, _ := args["file_path"].(string)
	if path == "" {
		path, _ = args["path"].(string)
	}
	content, _ := args["content"].(string)
	if path == "" || content == "" {
		return nil, fmt.Errorf("missing 'file_path' or 'content' args")
	}

	// Create parent dir + write via base64 pipe (safe for all content)
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	dir := filepath.Dir(path)
	cmd := fmt.Sprintf("mkdir -p '%s' && echo '%s' | base64 -d > '%s'",
		sandbox.ShellEscape(dir),
		encoded,
		sandbox.ShellEscape(path),
	)

	_, err := h.manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", cmd})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true,
		"path":    path,
		"size":    len(content),
	}, nil
}
