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

	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, map[string]any{
			"name": e.Name, "jit": e.JIT, "source": e.Source,
		})
	}

	return map[string]any{
		"operation": "list",
		"workers":   entries,
		"count":     len(entries),
		"widget": core.WidgetResult{
			Type:  "list",
			Title: fmt.Sprintf("%d worker%s", len(entries), pluralS(len(entries))),
			Icon:  "Users",
			Columns: []core.WidgetColumn{
				{Key: "name", Label: "Name", Type: core.WidgetColText},
				{Key: "jit", Label: "JIT", Type: core.WidgetColBoolean},
				{Key: "source", Label: "Source", Type: core.WidgetColBadge},
			},
			Rows:  rows,
			Empty: "No workers configured",
			Actions: []core.WidgetAction{
				{Label: "Delete", Icon: "Trash2", Method: "DELETE", URL: "/api/workers/{name}",
					Confirm: "Delete this worker?", Variant: "destructive"},
			},
		},
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

	item, _ := result.(map[string]any)
	if item == nil {
		item = map[string]any{"name": name}
	}

	return map[string]any{
		"operation": "show",
		"worker":    item,
		"widget": core.WidgetResult{
			Type:  "detail",
			Title: name,
			Icon:  "Users",
			Columns: []core.WidgetColumn{
				{Key: "persona", Label: "Persona", Type: core.WidgetColText},
				{Key: "capabilities", Label: "Capabilities", Type: core.WidgetColBadge},
				{Key: "model", Label: "Model", Type: core.WidgetColMono},
				{Key: "sandbox", Label: "Sandbox", Type: core.WidgetColBadge},
				{Key: "max_rounds", Label: "Max Rounds", Type: core.WidgetColMono},
				{Key: "jit", Label: "JIT", Type: core.WidgetColBoolean},
			},
			Item: item,
			Actions: []core.WidgetAction{
				{Label: "Delete", Icon: "Trash2", Method: "DELETE", URL: fmt.Sprintf("/api/workers/%s", name),
					Confirm: fmt.Sprintf("Delete worker %q?", name), Variant: "destructive"},
			},
		},
	}, nil
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
	msg := fmt.Sprintf("Worker %q created%s. Delegate to it by name.", name, jitLabel)
	return map[string]any{
		"operation": "create",
		"message":   msg,
		"name":      name,
		"jit":       store.IsJIT(),
		"details":   result,
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   "Worker Created",
			Icon:    "CheckCircle",
			Message: msg,
		},
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

	msg := fmt.Sprintf("Worker %q updated.", name)
	return map[string]any{
		"operation": "update",
		"message":   msg,
		"details":   result,
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   "Worker Updated",
			Icon:    "CheckCircle",
			Message: msg,
		},
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
	msg := fmt.Sprintf("Worker %q deleted.", name)
	return map[string]any{
		"operation": "delete",
		"message":   msg,
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   "Deleted",
			Icon:    "Trash2",
			Message: msg,
		},
	}, nil
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

	rows := make([]map[string]any, 0, len(details))
	for _, d := range details {
		row := make(map[string]any, len(d))
		for k, v := range d {
			row[k] = v
		}
		rows = append(rows, row)
	}

	return map[string]any{
		"operation":    "list_capabilities",
		"capabilities": details,
		"count":        len(details),
		"usage":        "Use capability names in the 'capabilities' array when creating workers.",
		"widget": core.WidgetResult{
			Type:  "list",
			Title: fmt.Sprintf("%d capabilit%s", len(details), pluralIES(len(details))),
			Icon:  "Package",
			Columns: []core.WidgetColumn{
				{Key: "name", Label: "Name", Type: core.WidgetColText},
				{Key: "description", Label: "Description", Type: core.WidgetColText},
				{Key: "tools", Label: "Tools", Type: core.WidgetColMono},
			},
			Rows:  rows,
			Empty: "No capabilities found",
		},
	}, nil
}
