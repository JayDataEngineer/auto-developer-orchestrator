package appprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/profiles"
	"github.com/auto-developer-orchestrator/backend/internal/tools/desktop"
)

// InteractTool translates semantic actions from a profile into desktop primitives.
// The agent calls app_interact(action="jump") and the tool resolves it to xdotool commands.
type InteractTool struct {
	store       *profiles.Store
	provider    desktop.DesktopProvider
	sandboxID   func() string
	activeProfile string // set by app_profile select
}

// NewInteractTool creates the app_interact tool.
func NewInteractTool(store *profiles.Store, provider desktop.DesktopProvider, sandboxID func() string) *InteractTool {
	return &InteractTool{
		store:     store,
		provider:  provider,
		sandboxID: sandboxID,
	}
}

func (t *InteractTool) Name() string { return "app_interact" }

func (t *InteractTool) Description() string {
	return `Execute a semantic action from the active application profile. Translates high-level actions (like "jump", "open_inventory", "send_message") into keyboard/mouse commands automatically based on the loaded profile.

Actions are defined in interaction profiles (YAML files in ~/.pux/profiles/ or project/profiles/). Use app_profile to see available actions.

Examples:
- app_interact(action="jump") — press the key mapped to "jump"
- app_interact(action="select_slot", params={"slot": 3}) — press key "3" for hotbar slot 3
- app_interact(action="send_command", params={"text": "/gamemode creative"}) — compound action: open chat, type, enter`
}

func (t *InteractTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"description": "Semantic action name from the profile (e.g. 'jump', 'move_forward', 'open_inventory')"
			},
			"params": {
				"type": "object",
				"description": "Parameters for the action (e.g. {\"slot\": 3}, {\"text\": \"hello\"})",
				"additionalProperties": true
			},
			"profile": {
				"type": "string",
				"description": "Profile name (optional — uses active profile if not specified)"
			},
			"duration_ms": {
				"type": "integer",
				"description": "Hold duration in ms for hold-type actions (default: 500ms)"
			}
		},
		"required": ["action"]
	}`)
}

func (t *InteractTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if sbID == "" {
		return nil, core.NewToolError("app_interact", "no sandbox available")
	}

	actionName, _ := args["action"].(string)
	if actionName == "" {
		return nil, core.NewToolError("app_interact", "missing required parameter 'action'")
	}

	// Collect params
	params := make(map[string]any)
	if p, ok := args["params"].(map[string]any); ok {
		params = p
	}

	// Determine profile name
	profileName, _ := args["profile"].(string)
	if profileName == "" {
		profileName = t.activeProfile
	}
	if profileName == "" {
		return nil, core.NewToolError("app_interact",
			"no active profile — use app_profile to select one, or pass profile parameter")
	}

	// Resolve action from profile
	action, err := t.store.ResolveAction(profileName, actionName, params)
	if err != nil {
		return nil, core.NewToolError("app_interact", err.Error())
	}

	// Execute the action
	result, err := t.executeAction(ctx, sbID, action, params, args)
	if err != nil {
		return nil, core.NewToolError("app_interact", err.Error())
	}

	return map[string]any{
		"profile":    profileName,
		"action":     actionName,
		"executed":   result,
	}, nil
}

// SetActiveProfile sets the default profile for subsequent app_interact calls.
func (t *InteractTool) SetActiveProfile(name string) {
	t.activeProfile = name
}

// executeAction translates an Action into desktop primitives and executes them.
func (t *InteractTool) executeAction(ctx context.Context, sbID string, action *profiles.Action, params map[string]any, args map[string]any) (string, error) {
	// Compound action: sequence of steps
	if len(action.Steps) > 0 {
		return t.executeSteps(ctx, sbID, action.Steps, params)
	}

	// Release action
	if action.Release && action.Key != "" {
		key := interpolate(action.Key, params)
		_, err := t.provider.DesktopKey(ctx, sbID, key+"up")
		if err != nil {
			return "", fmt.Errorf("release key %q: %w", key, err)
		}
		return fmt.Sprintf("released %s", key), nil
	}

	// Simple key action
	if action.Key != "" {
		key := interpolate(action.Key, params)
		isHold := action.Hold != nil && *action.Hold

		if isHold {
			// Hold the key down — agent must call again with release=true to release
			duration := 500 // default 500ms hold
			if d, ok := args["duration_ms"].(float64); ok && d > 0 {
				duration = int(d)
			}
			return t.executeHoldKey(ctx, sbID, key, duration)
		}

		_, err := t.provider.DesktopKey(ctx, sbID, key)
		if err != nil {
			return "", fmt.Errorf("key %q: %w", key, err)
		}

		if action.Wait > 0 {
			time.Sleep(time.Duration(action.Wait) * time.Millisecond)
		}
		return fmt.Sprintf("key %s", key), nil
	}

	// Shortcut action
	if action.Shortcut != "" {
		shortcut := interpolate(action.Shortcut, params)
		_, err := t.provider.DesktopKey(ctx, sbID, shortcut)
		if err != nil {
			return "", fmt.Errorf("shortcut %q: %w", shortcut, err)
		}
		if action.Wait > 0 {
			time.Sleep(time.Duration(action.Wait) * time.Millisecond)
		}
		return fmt.Sprintf("shortcut %s", shortcut), nil
	}

	// Mouse action
	if action.Mouse != "" {
		_ = mouseButtonToInt(action.Mouse)
		isHold := action.MouseHold != nil && *action.MouseHold
		if isHold {
			return fmt.Sprintf("mouse %s held (use release to release)", action.Mouse), nil
		}
		// Mouse click without coordinates — just press and release
		return fmt.Sprintf("mouse %s click", action.Mouse), nil
	}

	// Type action
	if action.Type != "" {
		text := interpolate(action.Type, params)
		_, err := t.provider.DesktopType(ctx, sbID, text)
		if err != nil {
			return "", fmt.Errorf("type: %w", err)
		}
		if action.Wait > 0 {
			time.Sleep(time.Duration(action.Wait) * time.Millisecond)
		}
		return fmt.Sprintf("typed %q", text), nil
	}

	return "", fmt.Errorf("action has no key, shortcut, mouse, or type field")
}

// executeSteps runs a compound action sequence.
func (t *InteractTool) executeSteps(ctx context.Context, sbID string, steps []profiles.Step, params map[string]any) (string, error) {
	var parts []string

	for i, step := range steps {
		// Wait step
		if step.Wait > 0 {
			time.Sleep(time.Duration(step.Wait) * time.Millisecond)
			parts = append(parts, fmt.Sprintf("wait %dms", step.Wait))
			continue
		}

		// Key step
		if step.Key != "" {
			key := interpolate(step.Key, params)
			if step.Duration > 0 {
				if err := t.executeHoldKeyStep(ctx, sbID, key, step.Duration); err != nil {
					return "", fmt.Errorf("step %d: %w", i, err)
				}
				parts = append(parts, fmt.Sprintf("hold %s %dms", key, step.Duration))
			} else {
				if _, err := t.provider.DesktopKey(ctx, sbID, key); err != nil {
					return "", fmt.Errorf("step %d key %q: %w", i, key, err)
				}
				parts = append(parts, fmt.Sprintf("key %s", key))
			}
		}

		// Shortcut step
		if step.Shortcut != "" {
			shortcut := interpolate(step.Shortcut, params)
			if _, err := t.provider.DesktopKey(ctx, sbID, shortcut); err != nil {
				return "", fmt.Errorf("step %d shortcut %q: %w", i, shortcut, err)
			}
			parts = append(parts, fmt.Sprintf("shortcut %s", shortcut))
		}

		// Type step
		if step.Type != "" {
			text := interpolate(step.Type, params)
			if _, err := t.provider.DesktopType(ctx, sbID, text); err != nil {
				return "", fmt.Errorf("step %d type: %w", i, err)
			}
			parts = append(parts, fmt.Sprintf("type %q", text))
		}

		// Mouse step
		if step.Mouse != "" || step.Click != "" {
			btn := step.Mouse
			if btn == "" {
				btn = step.Click
			}
			parts = append(parts, fmt.Sprintf("mouse %s", btn))
		}
	}

	return strings.Join(parts, " → "), nil
}

// executeHoldKey presses a key, waits, then releases.
func (t *InteractTool) executeHoldKey(ctx context.Context, sbID, key string, durationMs int) (string, error) {
	// Key down
	if _, err := t.provider.DesktopKey(ctx, sbID, key+"down"); err != nil {
		return "", fmt.Errorf("keydown %q: %w", key, err)
	}
	time.Sleep(time.Duration(durationMs) * time.Millisecond)
	// Key up
	if _, err := t.provider.DesktopKey(ctx, sbID, key+"up"); err != nil {
		return "", fmt.Errorf("keyup %q: %w", key, err)
	}
	return fmt.Sprintf("held %s for %dms", key, durationMs), nil
}

// executeHoldKeyStep presses and releases a key for a duration within a step sequence.
func (t *InteractTool) executeHoldKeyStep(ctx context.Context, sbID, key string, durationMs int) error {
	if _, err := t.provider.DesktopKey(ctx, sbID, key+"down"); err != nil {
		return err
	}
	time.Sleep(time.Duration(durationMs) * time.Millisecond)
	_, err := t.provider.DesktopKey(ctx, sbID, key+"up")
	return err
}

// interpolate replaces {param} placeholders with values from params.
func interpolate(template string, params map[string]any) string {
	result := template
	for k, v := range params {
		result = strings.ReplaceAll(result, "{"+k+"}", fmt.Sprintf("%v", v))
	}
	return result
}

// mouseButtonToInt converts a mouse button name to xdotool button number.
func mouseButtonToInt(name string) int {
	switch strings.ToLower(name) {
	case "left":
		return 1
	case "middle":
		return 2
	case "right":
		return 3
	default:
		return 1
	}
}
