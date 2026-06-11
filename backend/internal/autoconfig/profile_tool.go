package autoconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ProfileTool lets the AI manage application profiles through the ArtifactStore contract.
// Implements core.Tool — registered as a CTO tool.
type ProfileTool struct {
	store *ProfileStore
}

// NewProfileTool creates the profile management tool.
func NewProfileTool(store *ProfileStore) *ProfileTool {
	return &ProfileTool{store: store}
}

func (t *ProfileTool) Name() string { return "manage_profile" }

func (t *ProfileTool) Description() string {
	return `Manage application interaction profiles. Profiles map semantic actions (like "jump", "open_inventory") to keyboard/mouse inputs.

Operations:
- list: Show all available profiles
- show: Display a profile's actions
- create: Create a new profile from YAML content
- update: Update an existing profile
- delete: Remove a profile

Use list_capabilities on manage_worker to see available tool packages.`
}

func (t *ProfileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["list", "show", "create", "update", "delete"],
				"description": "Operation to perform"
			},
			"name": {
				"type": "string",
				"description": "Profile name (required for show, create, update, delete)"
			},
			"content": {
				"type": "string",
				"description": "YAML content for create/update operations"
			}
		},
		"required": ["operation"]
	}`)
}

func (t *ProfileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	if op == "" {
		return nil, core.NewToolError("manage_profile", "missing required parameter 'operation'")
	}

	switch op {
	case "list":
		return t.list(ctx)
	case "show":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_profile", "show requires 'name'")
		}
		return t.show(ctx, name)
	case "create":
		return t.createOrUpdate(ctx, args, "create")
	case "update":
		return t.createOrUpdate(ctx, args, "update")
	case "delete":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_profile", "delete requires 'name'")
		}
		return t.delete(ctx, name)
	default:
		return nil, core.NewToolError("manage_profile", fmt.Sprintf("unknown operation: %s", op))
	}
}

func (t *ProfileTool) list(ctx context.Context) (any, error) {
	result, err := t.store.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]string, 0)
	if m, ok := result.(map[string]any); ok {
		if list, ok := m["items"].([]string); ok {
			items = list
		}
	}
	rows := make([]map[string]any, 0, len(items))
	for _, name := range items {
		rows = append(rows, map[string]any{"name": name})
	}
	return map[string]any{
		"operation": "list",
		"items":     items,
		"count":     len(items),
		"widget": core.WidgetResult{
			Type:    "list",
			Title:   fmt.Sprintf("%d profile%s", len(items), pluralS(len(items))),
			Icon:    "IdCard",
			Columns: []core.WidgetColumn{{Key: "name", Label: "Name", Type: core.WidgetColText}},
			Rows:    rows,
			Empty:   "No profiles configured",
			Actions: []core.WidgetAction{
				{Label: "Delete", Icon: "Trash2", Method: "DELETE", URL: "/api/profiles/{name}",
					Confirm: "Delete this profile?", Variant: "destructive"},
			},
		},
	}, nil
}

func (t *ProfileTool) show(ctx context.Context, name string) (any, error) {
	result, err := t.store.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	item, _ := result.(map[string]any)
	if item == nil {
		item = map[string]any{"name": name}
	}
	return map[string]any{
		"operation": "show",
		"profile":   item,
		"widget": core.WidgetResult{
			Type:  "detail",
			Title: name,
			Icon:  "IdCard",
			Columns: []core.WidgetColumn{
				{Key: "name", Label: "Name", Type: core.WidgetColText},
				{Key: "type", Label: "Type", Type: core.WidgetColBadge},
				{Key: "actions", Label: "Actions", Type: core.WidgetColMono},
			},
			Item: item,
			Actions: []core.WidgetAction{
				{Label: "Delete", Icon: "Trash2", Method: "DELETE", URL: fmt.Sprintf("/api/profiles/%s", name),
					Confirm: fmt.Sprintf("Delete profile %q?", name), Variant: "destructive"},
			},
		},
	}, nil
}

func (t *ProfileTool) createOrUpdate(ctx context.Context, args map[string]any, op string) (any, error) {
	name, _ := args["name"].(string)
	content, _ := args["content"].(string)
	if name == "" {
		return nil, core.NewToolError("manage_profile", fmt.Sprintf("%s requires 'name'", op))
	}
	if content == "" {
		return nil, core.NewToolError("manage_profile", fmt.Sprintf("%s requires 'content' (YAML)", op))
	}
	spec := map[string]any{"content": content}
	_, err := t.store.Put(ctx, name, spec)
	if err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("Profile %q %sd.", name, op)
	title := "Profile Created"
	if op == "update" {
		title = "Profile Updated"
	}
	return map[string]any{
		"operation": op,
		"message":   msg,
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   title,
			Icon:    "CheckCircle",
			Message: msg,
		},
	}, nil
}

func (t *ProfileTool) delete(ctx context.Context, name string) (any, error) {
	if err := t.store.Delete(ctx, name); err != nil {
		return nil, err
	}
	msg := fmt.Sprintf("Profile %q deleted.", name)
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

var _ core.Tool = (*ProfileTool)(nil)
