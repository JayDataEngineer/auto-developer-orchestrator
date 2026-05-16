package autoconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/profiles"
)

func TestProfileToolList(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	// Write a profile
	profilesDir := filepath.Join(project, "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "app: testapp\ntype: game\nactions:\n  jump:\n    key: space\n"
	if err := os.WriteFile(filepath.Join(profilesDir, "testapp.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(ctx, map[string]any{"operation": "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m := result.(map[string]any)
	items := m["items"].([]string)
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

func TestProfileToolShow(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	profilesDir := filepath.Join(project, "profiles")
	os.MkdirAll(profilesDir, 0755)
	yaml := "app: mygame\ntype: game\nactions:\n  jump:\n    key: space\n  shoot:\n    key: f\n"
	os.WriteFile(filepath.Join(profilesDir, "mygame.yaml"), []byte(yaml), 0644)

	result, err := tool.Execute(ctx, map[string]any{"operation": "show", "name": "mygame"})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	m := result.(map[string]any)
	profile := m["profile"].(map[string]any)
	if profile["name"] != "mygame" {
		t.Errorf("name = %q, want mygame", profile["name"])
	}
}

func TestProfileToolCreate(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	yaml := "app: newapp\ntype: game\nactions:\n  jump:\n    key: space\n"
	result, err := tool.Execute(ctx, map[string]any{
		"operation": "create",
		"name":      "newapp",
		"content":   yaml,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = result

	// Verify it was saved
	prof, err := ps.Load("newapp")
	if err != nil {
		t.Fatalf("load after create: %v", err)
	}
	if prof.App != "newapp" {
		t.Errorf("app = %q, want newapp", prof.App)
	}
}

func TestProfileToolCreateMissingName(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"operation": "create",
		"content":   "app: foo\ntype: bar\n",
	})
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestProfileToolCreateMissingContent(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"operation": "create",
		"name":      "foo",
	})
	if err == nil {
		t.Error("expected error for missing content")
	}
}

func TestProfileToolDelete(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	profilesDir := filepath.Join(project, "profiles")
	os.MkdirAll(profilesDir, 0755)
	yaml := "app: deleteme\ntype: game\nactions: {}\n"
	os.WriteFile(filepath.Join(profilesDir, "deleteme.yaml"), []byte(yaml), 0644)

	result, err := tool.Execute(ctx, map[string]any{"operation": "delete", "name": "deleteme"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	_ = result

	if _, err := ps.Load("deleteme"); err == nil {
		t.Error("expected profile to be deleted")
	}
}

func TestProfileToolMissingOperation(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing operation")
	}
}

func TestProfileToolShowMissingName(t *testing.T) {
	project := t.TempDir()
	ps := profiles.NewStore(project)
	s := NewProfileStore(ps)
	tool := NewProfileTool(s)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{"operation": "show"})
	if err == nil {
		t.Error("expected error for show without name")
	}
}
