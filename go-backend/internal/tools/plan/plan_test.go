package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizePlanName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "refactor-auth", want: "refactor-auth"},
		{input: "Refactor Auth Module", want: "refactor-auth-module"},
		{input: "hello   world", want: "hello-world"},
		{input: "UPPERCASE", want: "uppercase"},
		{input: "refactor auth!!!", want: "refactor-auth"},
		{input: "---leading-dashes---", want: "leading-dashes"},
		{input: "a", want: "a"},
		{input: "", want: ""},
		{input: "123-test", want: "123-test"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizePlanName(tc.input)
			if got != tc.want {
				t.Errorf("sanitizePlanName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizePlanName_MaxLength(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := sanitizePlanName(long)
	if len(got) > 64 {
		t.Errorf("expected max 64 chars, got %d", len(got))
	}
}

func TestInjectActivePlan_NoDir(t *testing.T) {
	result := InjectActivePlan(t.TempDir())
	if result != "" {
		t.Errorf("expected empty for no plans dir, got %q", result)
	}
}

func TestInjectActivePlan_NoPlans(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".pux", "plans"), 0755)
	result := InjectActivePlan(dir)
	if result != "" {
		t.Errorf("expected empty for empty plans dir, got %q", result)
	}
}

func TestInjectActivePlan_WithPlan(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".pux", "plans")
	os.MkdirAll(plansDir, 0755)
	os.WriteFile(filepath.Join(plansDir, "test-plan.md"), []byte("# Plan: Test"), 0644)

	result := InjectActivePlan(dir)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "Test") {
		t.Errorf("expected plan content, got %q", result)
	}
	if !strings.HasPrefix(result, "<active_plan>") {
		t.Errorf("expected <active_plan> prefix, got %q", result[:20])
	}
	if !strings.HasSuffix(result, "\n\n") {
		t.Errorf("expected trailing newlines, got %q", result[len(result)-10:])
	}
}

func TestInjectActivePlan_OnlyMD(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".pux", "plans")
	os.MkdirAll(plansDir, 0755)
	os.WriteFile(filepath.Join(plansDir, "plan.md"), []byte("plan 1"), 0644)
	os.WriteFile(filepath.Join(plansDir, "notes.txt"), []byte("not a plan"), 0644)

	result := InjectActivePlan(dir)
	if !strings.Contains(result, "plan 1") {
		t.Errorf("expected plan 1 content, got %q", result)
	}
}

func TestInjectActivePlan_MostRecent(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, ".pux", "plans")
	os.MkdirAll(plansDir, 0755)

	// Write older plan
	oldPath := filepath.Join(plansDir, "old.md")
	os.WriteFile(oldPath, []byte("old plan"), 0644)

	// Write newer plan
	newPath := filepath.Join(plansDir, "new.md")
	os.WriteFile(newPath, []byte("new plan"), 0644)

	result := InjectActivePlan(dir)
	if !strings.Contains(result, "new plan") {
		t.Errorf("expected most recent plan (new plan), got %q", result)
	}
}
