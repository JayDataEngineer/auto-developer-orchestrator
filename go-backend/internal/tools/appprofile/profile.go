package appprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/profiles"
	"gopkg.in/yaml.v3"
)

// ProfileTool manages interaction profiles — CRUD + select active profile.
type ProfileTool struct {
	store     *profiles.Store
	interact  *InteractTool // to set active profile
}

// NewProfileTool creates the app_profile tool.
func NewProfileTool(store *profiles.Store, interact *InteractTool) *ProfileTool {
	return &ProfileTool{store: store, interact: interact}
}

func (t *ProfileTool) Name() string { return "app_profile" }

func (t *ProfileTool) Description() string {
	return `Manage application interaction profiles. Profiles map semantic actions (like "jump", "open_inventory") to keyboard/mouse inputs.

Operations:
- list: Show all available profiles
- show: Display a profile's actions and key mappings
- select: Set a profile as active for app_interact
- create: Create a new profile from YAML content
- update: Update an existing profile (merge or replace)
- delete: Remove a project-level profile

Profiles are stored in ~/.pux/profiles/ (global) or project/profiles/ (project-specific).
Edit the YAML files directly or use this tool to manage them.`
}

func (t *ProfileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["list", "show", "select", "create", "update", "delete"],
				"description": "Operation to perform"
			},
			"profile": {
				"type": "string",
				"description": "Profile name (required for show, select, update, delete)"
			},
			"content": {
				"type": "string",
				"description": "YAML content for create/update operations"
			},
			"merge": {
				"type": "boolean",
				"description": "For update: merge actions instead of replacing (default: false = replace)"
			}
		},
		"required": ["operation"]
	}`)
}

func (t *ProfileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	if op == "" {
		return nil, core.NewToolError("app_profile", "missing required parameter 'operation'")
	}

	switch op {
	case "list":
		return t.listProfiles()
	case "show":
		name, _ := args["profile"].(string)
		if name == "" {
			return nil, core.NewToolError("app_profile", "show requires 'profile' parameter")
		}
		return t.showProfile(name)
	case "select":
		name, _ := args["profile"].(string)
		if name == "" {
			return nil, core.NewToolError("app_profile", "select requires 'profile' parameter")
		}
		return t.selectProfile(name)
	case "create":
		return t.createProfile(args)
	case "update":
		return t.updateProfile(args)
	case "delete":
		name, _ := args["profile"].(string)
		if name == "" {
			return nil, core.NewToolError("app_profile", "delete requires 'profile' parameter")
		}
		return t.deleteProfile(name)
	default:
		return nil, core.NewToolError("app_profile", fmt.Sprintf("unknown operation: %s", op))
	}
}

func (t *ProfileTool) listProfiles() (any, error) {
	names, err := t.store.List()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return map[string]any{
			"profiles": []string{},
			"message":   "No profiles found. Use create to add one, or place YAML files in ~/.pux/profiles/",
		}, nil
	}
	return map[string]any{
		"profiles": names,
		"count":    len(names),
	}, nil
}

func (t *ProfileTool) showProfile(name string) (any, error) {
	prof, err := t.store.Load(name)
	if err != nil {
		return nil, err
	}

	// Format a human-readable summary
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Profile: %s (type: %s)\n", prof.App, prof.Type))
	if prof.Detect.WindowTitle != "" || prof.Detect.WindowClass != "" || prof.Detect.Process != "" {
		sb.WriteString(fmt.Sprintf("Detect: title=%q class=%q process=%q\n",
			prof.Detect.WindowTitle, prof.Detect.WindowClass, prof.Detect.Process))
	}
	sb.WriteString(fmt.Sprintf("\nActions (%d):\n", len(prof.Actions)))
	for name, action := range prof.Actions {
		sb.WriteString(fmt.Sprintf("  %s: ", name))
		if action.Key != "" {
			sb.WriteString(fmt.Sprintf("key=%s", action.Key))
			if action.Hold != nil && *action.Hold {
				sb.WriteString(" (hold)")
			}
		} else if action.Shortcut != "" {
			sb.WriteString(fmt.Sprintf("shortcut=%s", action.Shortcut))
		} else if action.Mouse != "" {
			sb.WriteString(fmt.Sprintf("mouse=%s", action.Mouse))
		} else if action.Type != "" {
			sb.WriteString(fmt.Sprintf("type=%q", action.Type))
		} else if len(action.Steps) > 0 {
			sb.WriteString(fmt.Sprintf("compound (%d steps)", len(action.Steps)))
		}
		if len(action.Params) > 0 {
			paramNames := make([]string, 0, len(action.Params))
			for p := range action.Params {
				paramNames = append(paramNames, p)
			}
			sb.WriteString(fmt.Sprintf(" params: %s", strings.Join(paramNames, ", ")))
		}
		sb.WriteString("\n")
	}

	return map[string]any{
		"profile": name,
		"summary": sb.String(),
		"actions": prof.Actions,
	}, nil
}

func (t *ProfileTool) selectProfile(name string) (any, error) {
	// Verify profile exists
	prof, err := t.store.Load(name)
	if err != nil {
		return nil, err
	}

	// Set as active on the interact tool
	if t.interact != nil {
		t.interact.SetActiveProfile(name)
	}

	return map[string]any{
		"active_profile": name,
		"type":           prof.Type,
		"actions":        len(prof.Actions),
		"message":        fmt.Sprintf("Profile %q selected. Use app_interact to execute actions.", name),
	}, nil
}

func (t *ProfileTool) createProfile(args map[string]any) (any, error) {
	name, _ := args["profile"].(string)
	content, _ := args["content"].(string)

	if name == "" {
		return nil, core.NewToolError("app_profile", "create requires 'profile' name")
	}
	if content == "" {
		return nil, core.NewToolError("app_profile", "create requires 'content' (YAML)")
	}

	var prof profiles.Profile
	if err := yaml.Unmarshal([]byte(content), &prof); err != nil {
		return nil, core.NewToolError("app_profile", fmt.Sprintf("invalid YAML: %v", err))
	}

	if prof.App == "" {
		prof.App = name
	}

	if err := t.store.Save(name, &prof); err != nil {
		return nil, err
	}

	return map[string]any{
		"profile":  name,
		"type":     prof.Type,
		"actions":  len(prof.Actions),
		"message":  fmt.Sprintf("Profile %q created with %d actions.", name, len(prof.Actions)),
	}, nil
}

func (t *ProfileTool) updateProfile(args map[string]any) (any, error) {
	name, _ := args["profile"].(string)
	content, _ := args["content"].(string)
	merge, _ := args["merge"].(bool)

	if name == "" {
		return nil, core.NewToolError("app_profile", "update requires 'profile' name")
	}
	if content == "" {
		return nil, core.NewToolError("app_profile", "update requires 'content' (YAML)")
	}

	var update profiles.Profile
	if err := yaml.Unmarshal([]byte(content), &update); err != nil {
		return nil, core.NewToolError("app_profile", fmt.Sprintf("invalid YAML: %v", err))
	}

	if merge {
		// Load existing and merge actions
		existing, err := t.store.Load(name)
		if err != nil {
			return nil, core.NewToolError("app_profile", fmt.Sprintf("cannot merge: %v", err))
		}
		if existing.Actions == nil {
			existing.Actions = make(map[string]profiles.Action)
		}
		for k, v := range update.Actions {
			existing.Actions[k] = v
		}
		if update.Type != "" {
			existing.Type = update.Type
		}
		if update.Detect.WindowTitle != "" {
			existing.Detect.WindowTitle = update.Detect.WindowTitle
		}
		if update.Detect.WindowClass != "" {
			existing.Detect.WindowClass = update.Detect.WindowClass
		}
		if update.Detect.Process != "" {
			existing.Detect.Process = update.Detect.Process
		}
		if update.Layout != nil {
			existing.Layout = update.Layout
		}
		update = *existing
	}

	if err := t.store.Save(name, &update); err != nil {
		return nil, err
	}

	return map[string]any{
		"profile": name,
		"actions": len(update.Actions),
		"merged":  merge,
		"message": fmt.Sprintf("Profile %q updated (%d actions).", name, len(update.Actions)),
	}, nil
}

func (t *ProfileTool) deleteProfile(name string) (any, error) {
	if err := t.store.Delete(name); err != nil {
		return nil, err
	}
	return map[string]any{
		"profile": name,
		"message": fmt.Sprintf("Profile %q deleted.", name),
	}, nil
}
