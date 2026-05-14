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
		return t.store.List(ctx)
	case "show":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_profile", "show requires 'name'")
		}
		return t.store.Get(ctx, name)
	case "create":
		name, _ := args["name"].(string)
		content, _ := args["content"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_profile", "create requires 'name'")
		}
		if content == "" {
			return nil, core.NewToolError("manage_profile", "create requires 'content' (YAML)")
		}
		spec := map[string]any{"content": content}
		return t.store.Put(ctx, name, spec)
	case "update":
		name, _ := args["name"].(string)
		content, _ := args["content"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_profile", "update requires 'name'")
		}
		if content == "" {
			return nil, core.NewToolError("manage_profile", "update requires 'content' (YAML)")
		}
		spec := map[string]any{"content": content}
		return t.store.Put(ctx, name, spec)
	case "delete":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_profile", "delete requires 'name'")
		}
		if err := t.store.Delete(ctx, name); err != nil {
			return nil, err
		}
		return TextResult(fmt.Sprintf("Profile %q deleted.", name)), nil
	default:
		return nil, core.NewToolError("manage_profile", fmt.Sprintf("unknown operation: %s", op))
	}
}

var _ core.Tool = (*ProfileTool)(nil)
