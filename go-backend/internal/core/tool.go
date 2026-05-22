package core

import (
	"context"
	"encoding/json"
	"strings"
)

// Tool is the minimal interface every tool must implement.
// The agent loop knows nothing about specific tools — it just calls Execute().
// Tools are injected into the agent at composition time.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args map[string]any) (any, error)
}

// ToolExecutor is the interface used by the agent loop to dispatch tool calls.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName string, args map[string]any) (any, error)
}

// ToolExecutorStreaming is an optional interface for tools that stream partial results.
// If a tool implements this, the agent loop will use it for progressive updates.
type ToolExecutorStreaming interface {
	ToolExecutor
	ExecuteStreaming(ctx context.Context, toolName string, args map[string]any, onUpdate func(string)) (any, error)
}

// ToolRegistry maps tool names to Tool implementations.
type ToolRegistry struct {
	tools    map[string]Tool
	aliases  map[string]string // alias → canonical name
}

// NewToolRegistry creates a registry from a slice of tools.
func NewToolRegistry(tools []Tool) *ToolRegistry {
	reg := &ToolRegistry{
		tools:   make(map[string]Tool, len(tools)),
		aliases: make(map[string]string),
	}
	for _, t := range tools {
		reg.tools[t.Name()] = t
	}
	return reg
}

// RegisterAlias adds an alternate name for a canonical tool.
// The agent loop automatically normalizes tool names through aliases.
// Skips registration if the alias collides with an actual registered tool
// (e.g., MCP tools like "search", "grep", "glob" must not be overridden).
func (r *ToolRegistry) RegisterAlias(alias, canonical string) {
	// Prevent self-referencing loops
	if alias == canonical {
		return
	}
	// Don't shadow real tools — an MCP tool named "search" must not be
	// redirected to a non-existent "search_web" via alias.
	if _, exists := r.tools[alias]; exists {
		return
	}
	r.aliases[alias] = canonical
}

// RegisterCommonAliases adds standard aliases so models can call tools by common alternate names.
// Canonical tool names are: bash, file_read, file_write, file_edit, file_grep, file_glob, etc.
// Aliases let models use natural names like write_file, read_file, etc.
// Each alias is guarded: if the alias OR canonical name is not in the registry, it's a no-op.
func (r *ToolRegistry) RegisterCommonAliases() {
	// Bash variants
	r.RegisterAlias("bash_execute", "bash")
	r.RegisterAlias("execute_bash", "bash")
	r.RegisterAlias("execute_command", "bash")
	r.RegisterAlias("run_command", "bash")
	// Model-friendly: models often say write_file, read_file → resolve to file_write, file_read
	r.RegisterAlias("read_file", "file_read")
	r.RegisterAlias("write_file", "file_write")
	r.RegisterAlias("edit_file", "file_edit")
	r.RegisterAlias("grep", "file_grep")
	r.RegisterAlias("glob", "file_glob")
	// Browser navigation shortcut
	r.RegisterAlias("navigate", "browse_to")
}

// Get returns a tool by name, resolving aliases.
// Returns nil if the tool is not found.
func (r *ToolRegistry) Get(name string) Tool {
	canonical := r.normalize(name)
	return r.tools[canonical]
}

// All returns all registered tools.
func (r *ToolRegistry) All() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// Names returns the names of all registered tools.
func (r *ToolRegistry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// normalize resolves a tool name through aliases.
func (r *ToolRegistry) normalize(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	// Apply alias chain (up to 5 levels to prevent infinite loops)
	for i := 0; i < 5; i++ {
		canon, ok := r.aliases[name]
		if !ok {
			return name
		}
		name = canon
	}
	return name
}

// NormalizeToolName is a convenience function for external use.
func (r *ToolRegistry) NormalizeToolName(name string) string {
	return r.normalize(name)
}

// Execute implements ToolExecutor by routing to registered tools.
func (r *ToolRegistry) Execute(ctx context.Context, toolName string, args map[string]any) (any, error) {
	canonical := r.normalize(toolName)
	t := r.tools[canonical]
	if t == nil {
		return nil, &ToolNotFoundError{ToolName: canonical}
	}
	return t.Execute(ctx, args)
}

// ToolNotFoundError is returned when a tool is not in the registry.
type ToolNotFoundError struct {
	ToolName string
}

func (e *ToolNotFoundError) Error() string {
	return "tool not found: " + e.ToolName
}

// ToolError is a general tool execution error.
type ToolError struct {
	ToolName string
	Message  string
}

func (e *ToolError) Error() string {
	return "[" + e.ToolName + "] " + e.Message
}

// NewToolError creates a new ToolError.
func NewToolError(toolName, message string) *ToolError {
	return &ToolError{ToolName: toolName, Message: message}
}

// ── Error classification for retry logic ────────────────────────────

// ErrorClass categorizes tool execution errors for retry decisions.
type ErrorClass int

const (
	ErrorTransient ErrorClass = iota // temporary — retry after backoff
	ErrorPermanent                    // won't succeed — don't retry, change approach
	ErrorUnknown                      // uncertain — default behavior
)

// ClassifyError categorizes an error for retry decisions.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrorUnknown
	}
	msg := strings.ToLower(err.Error())
	// Transient: network issues, timeouts, connection drops, provider errors
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "connection") ||
		strings.Contains(msg, "reset") || strings.Contains(msg, "temporarily") ||
		strings.Contains(msg, "refused") || strings.Contains(msg, "eof") ||
		strings.Contains(msg, "context canceled") || strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "internal_error") || strings.Contains(msg, "stream error") ||
		strings.Contains(msg, "received from peer") || strings.Contains(msg, "500") ||
		strings.Contains(msg, "502") || strings.Contains(msg, "503") ||
		strings.Contains(msg, "504") || strings.Contains(msg, "overloaded") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "internal server error") {
		return ErrorTransient
	}
	// Permanent: resource doesn't exist, permission denied, invalid params
	if strings.Contains(msg, "not found") || strings.Contains(msg, "denied") ||
		strings.Contains(msg, "invalid") || strings.Contains(msg, "unknown") ||
		strings.Contains(msg, "missing") || strings.Contains(msg, "not available") {
		return ErrorPermanent
	}
	return ErrorUnknown
}
