package profiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLoad(t *testing.T) {
	// Create temp dirs
	global := t.TempDir()
	project := t.TempDir()
	s := &Store{
		global:  global,
		project: project,
		cache:   make(map[string]*cachedProfile),
	}

	// Write a profile to global dir
	profileYAML := `
app: test-app
type: game
actions:
  jump:
    key: space
  move:
    key: w
    hold: true
`
	if err := os.WriteFile(filepath.Join(global, "test-app.yaml"), []byte(profileYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Load it
	prof, err := s.Load("test-app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if prof.App != "test-app" {
		t.Errorf("App = %q, want %q", prof.App, "test-app")
	}
	if prof.Type != "game" {
		t.Errorf("Type = %q, want %q", prof.Type, "game")
	}
	if len(prof.Actions) != 2 {
		t.Fatalf("Actions = %d, want 2", len(prof.Actions))
	}
	if prof.Actions["jump"].Key != "space" {
		t.Errorf("jump key = %q, want %q", prof.Actions["jump"].Key, "space")
	}
	if prof.Actions["move"].Hold == nil || !*prof.Actions["move"].Hold {
		t.Error("move hold = false, want true")
	}
}

func TestStoreProjectOverridesGlobal(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()
	s := &Store{
		global:  global,
		project: project,
		cache:   make(map[string]*cachedProfile),
	}

	// Global version
	globalYAML := `
app: myapp
type: game
actions:
  jump:
    key: space
`
	if err := os.WriteFile(filepath.Join(global, "myapp.yaml"), []byte(globalYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Project version (overrides)
	projectYAML := `
app: myapp
type: game
actions:
  jump:
    key: Enter
  attack:
    mouse: left
`
	if err := os.WriteFile(filepath.Join(project, "myapp.yaml"), []byte(projectYAML), 0644); err != nil {
		t.Fatal(err)
	}

	prof, err := s.Load("myapp")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Should get project version
	if prof.Actions["jump"].Key != "Enter" {
		t.Errorf("jump key = %q, want %q (project override)", prof.Actions["jump"].Key, "Enter")
	}
	if _, ok := prof.Actions["attack"]; !ok {
		t.Error("missing 'attack' action from project profile")
	}
}

func TestStoreSaveAndReload(t *testing.T) {
	project := t.TempDir()
	s := &Store{
		global:  t.TempDir(),
		project: project,
		cache:   make(map[string]*cachedProfile),
	}

	hold := true
	prof := &Profile{
		App:   "newgame",
		Type:  "game",
		Actions: map[string]Action{
			"jump":  {Key: "space"},
			"run":   {Key: "Shift_L", Hold: &hold},
		},
	}

	if err := s.Save("newgame", prof); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(filepath.Join(project, "newgame.yaml")); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Load it back
	loaded, err := s.Load("newgame")
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if loaded.Actions["jump"].Key != "space" {
		t.Errorf("jump key = %q, want %q", loaded.Actions["jump"].Key, "space")
	}
	if loaded.Actions["run"].Hold == nil || !*loaded.Actions["run"].Hold {
		t.Error("run hold = false, want true")
	}
}

func TestStoreList(t *testing.T) {
	global := t.TempDir()
	s := &Store{
		global: global,
		cache:  make(map[string]*cachedProfile),
	}

	// Create a few profiles
	for _, name := range []string{"minecraft", "godot", "telegram"} {
		yaml := "app: " + name + "\ntype: game\nactions: {}\n"
		if err := os.WriteFile(filepath.Join(global, name+".yaml"), []byte(yaml), 0644); err != nil {
			t.Fatal(err)
		}
	}

	names, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 3 {
		t.Errorf("List returned %d profiles, want 3", len(names))
	}
}

func TestResolveAction(t *testing.T) {
	global := t.TempDir()
	s := &Store{
		global: global,
		cache:  make(map[string]*cachedProfile),
	}

	profileYAML := `
app: testgame
type: game
actions:
  jump:
    key: space
  select_slot:
    key: "{slot}"
    params:
      slot: {type: int, range: [1, 9]}
  send_msg:
    steps:
      - key: T
      - type: "{text}"
      - key: Return
    params:
      text: {type: string}
`
	if err := os.WriteFile(filepath.Join(global, "testgame.yaml"), []byte(profileYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Simple action
	action, err := s.ResolveAction("testgame", "jump", nil)
	if err != nil {
		t.Fatalf("ResolveAction jump: %v", err)
	}
	if action.Key != "space" {
		t.Errorf("jump key = %q, want %q", action.Key, "space")
	}

	// Parameterized action with params
	action, err = s.ResolveAction("testgame", "select_slot", map[string]any{"slot": 3})
	if err != nil {
		t.Fatalf("ResolveAction select_slot: %v", err)
	}
	if action.Key != "{slot}" {
		t.Errorf("select_slot key = %q, want %q", action.Key, "{slot}")
	}

	// Parameterized action missing params
	_, err = s.ResolveAction("testgame", "select_slot", nil)
	if err == nil {
		t.Error("expected error for missing param")
	}

	// Compound action with params
	action, err = s.ResolveAction("testgame", "send_msg", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("ResolveAction send_msg: %v", err)
	}
	if len(action.Steps) != 3 {
		t.Errorf("send_msg steps = %d, want 3", len(action.Steps))
	}

	// Unknown action
	_, err = s.ResolveAction("testgame", "fly", nil)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestPathTraversal(t *testing.T) {
	s := &Store{
		global: t.TempDir(),
		cache:  make(map[string]*cachedProfile),
	}

	_, err := s.Load("../../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestCacheInvalidation(t *testing.T) {
	global := t.TempDir()
	s := &Store{
		global: global,
		cache:  make(map[string]*cachedProfile),
	}

	// Write v1
	yamlV1 := "app: cachetest\ntype: game\nactions:\n  jump:\n    key: space\n"
	if err := os.WriteFile(filepath.Join(global, "cachetest.yaml"), []byte(yamlV1), 0644); err != nil {
		t.Fatal(err)
	}

	prof, err := s.Load("cachetest")
	if err != nil {
		t.Fatal(err)
	}
	if prof.Actions["jump"].Key != "space" {
		t.Errorf("v1 jump = %q, want space", prof.Actions["jump"].Key)
	}

	// Manually clear the cache entry to simulate modTime change
	// (filesystem modTime has 1s resolution on many systems)
	s.mu.Lock()
	delete(s.cache, filepath.Join(global, "cachetest.yaml"))
	s.mu.Unlock()

	// Update file (v2)
	yamlV2 := "app: cachetest\ntype: game\nactions:\n  jump:\n    key: Enter\n"
	if err := os.WriteFile(filepath.Join(global, "cachetest.yaml"), []byte(yamlV2), 0644); err != nil {
		t.Fatal(err)
	}

	// Should get updated version
	prof2, err := s.Load("cachetest")
	if err != nil {
		t.Fatal(err)
	}
	if prof2.Actions["jump"].Key != "Enter" {
		t.Errorf("v2 jump = %q, want Enter", prof2.Actions["jump"].Key)
	}
}
