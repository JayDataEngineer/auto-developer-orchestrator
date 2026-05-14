package appprofile

import (
	"context"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/profiles"
)

// mockProvider records desktop commands for test assertions.
type mockProvider struct {
	keyPresses  []string
	typedTexts  []string
	clicks      []clickRecord
	keyHolds    []holdRecord
}

type clickRecord struct {
	x, y  float64
	button int
}

type holdRecord struct {
	key      string
	duration int
}

func (m *mockProvider) DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "ok"}, nil
}

func (m *mockProvider) DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error) {
	m.clicks = append(m.clicks, clickRecord{x, y, button})
	return map[string]interface{}{"status": "ok"}, nil
}

func (m *mockProvider) DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error) {
	m.typedTexts = append(m.typedTexts, text)
	return map[string]interface{}{"status": "ok"}, nil
}

func (m *mockProvider) DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error) {
	m.keyPresses = append(m.keyPresses, key)
	return map[string]interface{}{"status": "ok"}, nil
}

func (m *mockProvider) Resolution(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return map[string]interface{}{"width": "1920", "height": "1080"}, nil
}

func (m *mockProvider) DesktopObserve(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "ok"}, nil
}

func setupTest(t *testing.T) (*profiles.Store, *mockProvider, *InteractTool) {
	t.Helper()
	store := profiles.NewStore(t.TempDir())
	provider := &mockProvider{}
	sandboxID := func() string { return "test-sandbox" }

	// Create a test profile
	hold := true
	prof := &profiles.Profile{
		App:  "testgame",
		Type: "game",
		Actions: map[string]profiles.Action{
			"jump":          {Key: "space"},
			"move_forward":  {Key: "w", Hold: &hold},
			"open_inv":      {Key: "e", Wait: 100},
			"select_slot":   {Key: "{slot}", Params: map[string]profiles.ParamDef{"slot": {Type: "int"}}},
			"send_command": {
				Steps: []profiles.Step{
					{Key: "T"},
					{Type: "{text}"},
					{Key: "Return"},
				},
				Params: map[string]profiles.ParamDef{"text": {Type: "string"}},
			},
			"save": {Shortcut: "Ctrl+S"},
		},
	}
	if err := store.Save("testgame", prof); err != nil {
		t.Fatal(err)
	}

	interact := NewInteractTool(store, provider, sandboxID)
	interact.SetActiveProfile("testgame")

	return store, provider, interact
}

func TestSimpleKey(t *testing.T) {
	_, provider, interact := setupTest(t)

	result, err := interact.Execute(context.Background(), map[string]any{
		"action": "jump",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	r := result.(map[string]any)
	if r["action"] != "jump" {
		t.Errorf("action = %q, want %q", r["action"], "jump")
	}
	if len(provider.keyPresses) != 1 || provider.keyPresses[0] != "space" {
		t.Errorf("key presses = %v, want [space]", provider.keyPresses)
	}
}

func TestHoldKey(t *testing.T) {
	_, provider, interact := setupTest(t)

	result, err := interact.Execute(context.Background(), map[string]any{
		"action":      "move_forward",
		"duration_ms": float64(200),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	r := result.(map[string]any)
	if r["action"] != "move_forward" {
		t.Errorf("action = %q", r["action"])
	}

	// Should be keydown + keyup = 2 presses
	if len(provider.keyPresses) != 2 {
		t.Fatalf("expected 2 key presses (down+up), got %d: %v", len(provider.keyPresses), provider.keyPresses)
	}
	if provider.keyPresses[0] != "wdown" {
		t.Errorf("first press = %q, want %q", provider.keyPresses[0], "wdown")
	}
	if provider.keyPresses[1] != "wup" {
		t.Errorf("second press = %q, want %q", provider.keyPresses[1], "wup")
	}
}

func TestParameterizedAction(t *testing.T) {
	_, provider, interact := setupTest(t)

	result, err := interact.Execute(context.Background(), map[string]any{
		"action": "select_slot",
		"params": map[string]any{"slot": 5},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	r := result.(map[string]any)
	if r["action"] != "select_slot" {
		t.Errorf("action = %q", r["action"])
	}

	// The {slot} should be interpolated to "5"
	if len(provider.keyPresses) != 1 || provider.keyPresses[0] != "5" {
		t.Errorf("key presses = %v, want [5]", provider.keyPresses)
	}
}

func TestCompoundAction(t *testing.T) {
	_, provider, interact := setupTest(t)

	result, err := interact.Execute(context.Background(), map[string]any{
		"action": "send_command",
		"params": map[string]any{"text": "/gamemode creative"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	r := result.(map[string]any)
	executed, _ := r["executed"].(string)
	if executed == "" {
		t.Error("expected non-empty executed summary")
	}

	// Should be: key T, type text, key Return
	if len(provider.keyPresses) != 2 {
		t.Errorf("key presses = %v, want 2 (T + Return)", provider.keyPresses)
	}
	if len(provider.typedTexts) != 1 || provider.typedTexts[0] != "/gamemode creative" {
		t.Errorf("typed texts = %v, want [/gamemode creative]", provider.typedTexts)
	}
}

func TestShortcutAction(t *testing.T) {
	_, provider, interact := setupTest(t)

	_, err := interact.Execute(context.Background(), map[string]any{
		"action": "save",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(provider.keyPresses) != 1 || provider.keyPresses[0] != "Ctrl+S" {
		t.Errorf("key presses = %v, want [Ctrl+S]", provider.keyPresses)
	}
}

func TestMissingAction(t *testing.T) {
	_, _, interact := setupTest(t)

	_, err := interact.Execute(context.Background(), map[string]any{
		"action": "fly",
	})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestNoSandbox(t *testing.T) {
	store := profiles.NewStore("")
	provider := &mockProvider{}
	sandboxID := func() string { return "" }
	interact := NewInteractTool(store, provider, sandboxID)

	_, err := interact.Execute(context.Background(), map[string]any{
		"action": "jump",
	})
	if err == nil {
		t.Error("expected error when no sandbox")
	}
}

func TestNoActiveProfile(t *testing.T) {
	store := profiles.NewStore("")
	provider := &mockProvider{}
	sandboxID := func() string { return "test" }
	interact := NewInteractTool(store, provider, sandboxID)

	_, err := interact.Execute(context.Background(), map[string]any{
		"action": "jump",
	})
	if err == nil {
		t.Error("expected error when no active profile")
	}
}

func TestMissingRequiredParam(t *testing.T) {
	_, _, interact := setupTest(t)

	_, err := interact.Execute(context.Background(), map[string]any{
		"action": "select_slot",
		// no params
	})
	if err == nil {
		t.Error("expected error for missing param")
	}
}
