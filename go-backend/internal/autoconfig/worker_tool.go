package autoconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// WorkerTool lets the AI compose and manage workers at runtime.
// Implements core.Tool — registered as a CTO tool.
type WorkerTool struct {
	store    *WorkerStore // persistent workers
	jitStore *WorkerStore // session-scoped JIT workers (may be nil)
}

// NewWorkerTool creates the worker management tool.
// jitStore may be nil if JIT workers are not available.
func NewWorkerTool(store, jitStore *WorkerStore) *WorkerTool {
	return &WorkerTool{store: store, jitStore: jitStore}
}

func (t *WorkerTool) Name() string { return "manage_worker" }

func (t *WorkerTool) Description() string {
	return `Create and manage workers by composing capabilities. Workers are agents with specific tool sets.

Operations:
- list: Show all workers (persistent + JIT)
- show: Display a worker's configuration
- create: Create a new persistent worker
- create_jit: Create a session-scoped worker (auto-deleted when session ends)
- update: Update an existing worker
- delete: Remove a worker
- list_capabilities: Show available capabilities to compose with

Use list_capabilities first to see what you can compose, then create a worker and delegate to it immediately.`
}

func (t *WorkerTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["list", "show", "create", "create_jit", "update", "delete", "list_capabilities"],
				"description": "Operation to perform"
			},
			"name": {
				"type": "string",
				"description": "Worker name (lowercase, dash-separated, e.g. 'data-collector')"
			},
			"persona": {
				"type": "string",
				"description": "Worker identity and purpose (required for create/update)"
			},
			"capabilities": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Capabilities to compose (e.g. ['browser', 'research', 'shell']). Use list_capabilities to see available options."
			},
			"model": {
				"type": "string",
				"description": "Model override for this worker (optional)"
			},
			"max_rounds": {
				"type": "integer",
				"description": "Max tool rounds (default: 15)"
			},
			"temperature": {
				"type": "number",
				"description": "Temperature (default: 0.4)"
			},
			"sandbox": {
				"type": "string",
				"description": "Sandbox tier: isolated (default), bridged, or native"
			}
		},
		"required": ["operation"]
	}`)
}

func (t *WorkerTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	if op == "" {
		return nil, core.NewToolError("manage_worker", "missing required parameter 'operation'")
	}

	switch op {
	case "list":
		return t.list(ctx)
	case "show":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_worker", "show requires 'name'")
		}
		return t.show(ctx, name)
	case "create":
		return t.create(ctx, args, t.store)
	case "create_jit":
		if t.jitStore == nil {
			return nil, core.NewToolError("manage_worker", "JIT workers not available (no session)")
		}
		return t.create(ctx, args, t.jitStore)
	case "update":
		return t.update(ctx, args)
	case "delete":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_worker", "delete requires 'name'")
		}
		return t.delete(ctx, name)
	case "list_capabilities":
		return t.listCapabilities()
	default:
		return nil, core.NewToolError("manage_worker", fmt.Sprintf("unknown operation: %s", op))
	}
}

func (t *WorkerTool) list(ctx context.Context) (any, error) {
	type workerEntry struct {
		Name   string `json:"name"`
		JIT    bool   `json:"jit"`
		Source string `json:"source"`
	}

	var entries []workerEntry

	// Persistent workers
	persistent, err := t.store.List(ctx)
	if err != nil {
		return nil, err
	}
	if result, ok := persistent.(map[string]any); ok {
		if items, ok := result["items"].([]string); ok {
			for _, name := range items {
				entries = append(entries, workerEntry{Name: name, JIT: false, Source: "persistent"})
			}
		}
	}

	// JIT workers
	if t.jitStore != nil {
		jit, err := t.jitStore.List(ctx)
		if err != nil {
			return nil, err
		}
		if result, ok := jit.(map[string]any); ok {
			if items, ok := result["items"].([]string); ok {
				for _, name := range items {
					entries = append(entries, workerEntry{Name: name, JIT: true, Source: "session"})
				}
			}
		}
	}

	return map[string]any{
		"workers": entries,
		"count":   len(entries),
	}, nil
}

func (t *WorkerTool) show(ctx context.Context, name string) (any, error) {
	// Try persistent first, then JIT
	result, err := t.store.Get(ctx, name)
	if err != nil {
		if t.jitStore != nil {
			result, err = t.jitStore.Get(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("worker %q not found in persistent or JIT stores", name)
			}
		} else {
			return nil, err
		}
	}
	return result, nil
}

func (t *WorkerTool) create(ctx context.Context, args map[string]any, store *WorkerStore) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("manage_worker", "create requires 'name'")
	}

	// Build spec from args
	spec := map[string]any{
		"persona":      args["persona"],
		"capabilities": args["capabilities"],
		"tools":        args["tools"],
		"mcp_servers":  args["mcp_servers"],
		"max_rounds":   args["max_rounds"],
		"temperature":  args["temperature"],
		"model":        args["model"],
		"sandbox":      args["sandbox"],
	}

	result, err := store.Put(ctx, name, spec)
	if err != nil {
		return nil, err
	}

	jitLabel := ""
	if store.IsJIT() {
		jitLabel = " (session-scoped)"
	}
	return map[string]any{
		"message":   fmt.Sprintf("Worker %q created%s. Delegate to it by name.", name, jitLabel),
		"name":      name,
		"jit":       store.IsJIT(),
		"details":   result,
	}, nil
}

func (t *WorkerTool) update(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("manage_worker", "update requires 'name'")
	}

	// Find which store has the worker
	store := t.store
	if _, err := t.store.Get(ctx, name); err != nil {
		if t.jitStore != nil {
			if _, err2 := t.jitStore.Get(ctx, name); err2 == nil {
				store = t.jitStore
			} else {
				return nil, fmt.Errorf("worker %q not found", name)
			}
		} else {
			return nil, fmt.Errorf("worker %q not found", name)
		}
	}

	spec := map[string]any{
		"persona":      args["persona"],
		"capabilities": args["capabilities"],
		"tools":        args["tools"],
		"mcp_servers":  args["mcp_servers"],
		"max_rounds":   args["max_rounds"],
		"temperature":  args["temperature"],
		"model":        args["model"],
		"sandbox":      args["sandbox"],
	}

	result, err := store.Put(ctx, name, spec)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"message": fmt.Sprintf("Worker %q updated.", name),
		"details": result,
	}, nil
}

func (t *WorkerTool) delete(ctx context.Context, name string) (any, error) {
	// Try persistent first, then JIT
	err := t.store.Delete(ctx, name)
	if err != nil {
		if t.jitStore != nil {
			err = t.jitStore.Delete(ctx, name)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return TextResult(fmt.Sprintf("Worker %q deleted.", name)), nil
}

func (t *WorkerTool) listCapabilities() (any, error) {
	pkgs := common.LoadToolPackages()
	if len(pkgs) == 0 {
		return TextResult("No capabilities found. Check config/capabilities/ directory."), nil
	}

	names := make([]string, 0, len(pkgs))
	details := make([]map[string]string, 0, len(pkgs))
	for name, pkg := range pkgs {
		names = append(names, name)
		tools := strings.Join(pkg.Tools, ", ")
		if len(pkg.MCPServers) > 0 {
			tools += ", mcp:" + strings.Join(pkg.MCPServers, ", mcp:")
		}
		details = append(details, map[string]string{
			"name":        name,
			"description": pkg.Description,
			"tools":       tools,
		})
	}
	sort.Slice(details, func(i, j int) bool {
		return details[i]["name"] < details[j]["name"]
	})

	return map[string]any{
		"capabilities": details,
		"count":        len(details),
		"usage":        "Use capability names in the 'capabilities' array when creating workers.",
	}, nil
}
