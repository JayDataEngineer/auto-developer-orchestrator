package autoconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/profiles"
)

func TestProfileStoreList(t *testing.T) {
	// Create a project temp dir so profiles.NewStore picks up a project/ subpath
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	ctx := context.Background()

	// Write a profile to the project profiles dir
	profilesDir := filepath.Join(project, "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "app: testapp\ntype: game\nactions:\n  jump:\n    key: space\n"
	if err := os.WriteFile(filepath.Join(profilesDir, "testapp.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	m := result.(map[string]any)
	items := m["items"].([]string)
	// Should at least have testapp (may also have global profiles)
	found := false
	for _, name := range items {
		if name == "testapp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("testapp not found in list: %v", items)
	}
}

func TestProfileStoreGet(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	ctx := context.Background()

	// Write profile to project dir
	profilesDir := filepath.Join(project, "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "app: mygame\ntype: game\nactions:\n  jump:\n    key: space\n  shoot:\n    key: f\n"
	if err := os.WriteFile(filepath.Join(profilesDir, "mygame.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := s.Get(ctx, "mygame")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := result.(map[string]any)
	if m["name"] != "mygame" {
		t.Errorf("name = %q, want %q", m["name"], "mygame")
	}
	if m["actions"] != 2 {
		t.Errorf("actions = %v, want 2", m["actions"])
	}
}

func TestProfileStorePut(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	ctx := context.Background()

	result, err := s.Put(ctx, "newgame", map[string]any{
		"content": "app: newgame\ntype: game\nactions:\n  run:\n    key: w\n    hold: true\n",
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	m := result.(map[string]any)
	if m["message"] == nil {
		t.Error("expected message in Put result")
	}

	// Verify file exists in project/profiles/ dir
	if _, err := os.Stat(filepath.Join(project, "profiles", "newgame.yaml")); err != nil {
		t.Fatalf("profile file not created: %v", err)
	}
}

func TestProfileStoreDelete(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	ctx := context.Background()

	_, _ = s.Put(ctx, "temp-game", map[string]any{
		"content": "app: temp-game\ntype: game\nactions: {}\n",
	})

	if err := s.Delete(ctx, "temp-game"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get(ctx, "temp-game")
	if err == nil {
		t.Error("expected error getting deleted profile")
	}
}
