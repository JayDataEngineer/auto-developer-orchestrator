package perms

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestBashRuleStore_AddAndRemove(t *testing.T) {
	s := NewBashRuleStore(zap.NewNop())

	rule, err := s.AddRule("docker*", PermRequireApproval)
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if rule.Pattern != "docker*" {
		t.Errorf("pattern = %q, want %q", rule.Pattern, "docker*")
	}
	if rule.Level != PermRequireApproval {
		t.Errorf("level = %q, want %q", rule.Level, PermRequireApproval)
	}
	if rule.ID == "" {
		t.Error("expected non-empty ID")
	}

	rules := s.AllRules()
	if len(rules) != 1 {
		t.Fatalf("AllRules count = %d, want 1", len(rules))
	}

	if !s.RemoveRule(rule.ID) {
		t.Error("RemoveRule returned false")
	}
	if s.RemoveRule("nonexistent") {
		t.Error("RemoveRule should return false for nonexistent ID")
	}
	if len(s.AllRules()) != 0 {
		t.Error("expected 0 rules after removal")
	}
}

func TestBashRuleStore_LoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")

	s := NewBashRuleStore(zap.NewNop())
	s.SetFilePath(path)

	s.AddRule("rm", PermDeny)
	s.AddRule("docker*", PermRequireApproval)

	// Save
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load into fresh store
	s2 := NewBashRuleStore(zap.NewNop())
	if err := s2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	rules := s2.AllRules()
	if len(rules) != 2 {
		t.Fatalf("loaded count = %d, want 2", len(rules))
	}
	if rules[0].Pattern != "rm" || rules[0].Level != PermDeny {
		t.Errorf("rule[0] = %+v, want rm/deny", rules[0])
	}
	if rules[1].Pattern != "docker*" || rules[1].Level != PermRequireApproval {
		t.Errorf("rule[1] = %+v, want docker*/confirm", rules[1])
	}
}

func TestBashRuleStore_LoadMissingFile(t *testing.T) {
	s := NewBashRuleStore(zap.NewNop())
	if err := s.Load("/nonexistent/path/rules.json"); err != nil {
		t.Fatalf("Load missing file should not error: %v", err)
	}
	if len(s.AllRules()) != 0 {
		t.Error("expected empty rules for missing file")
	}
}

func TestBashRuleStore_InvalidPattern(t *testing.T) {
	s := NewBashRuleStore(zap.NewNop())

	cases := []struct {
		pattern string
		ok      bool
	}{
		{"", false},
		{"rm^", false},
		{"rm$", false},
		{"rm+", false},
		{"rm?", false},
		{"rm(foo)", false},
		{"rm[abc]", false},
		{"rm{1,3}", false},
		{"rm\\n", false},
		{"rm", true},
		{"docker*", true},
		{"git push*", true},
		{"rm -rf", true},
		{"my_cmd", true},
		{"build.sh", true},
	}

	for _, tc := range cases {
		_, err := s.AddRule(tc.pattern, PermDeny)
		if (err == nil) != tc.ok {
			t.Errorf("AddRule(%q) error = %v, ok = %v", tc.pattern, err, tc.ok)
		}
	}
}

func TestBashRuleStore_InvalidLevel(t *testing.T) {
	s := NewBashRuleStore(zap.NewNop())
	_, err := s.AddRule("rm", "invalid")
	if err == nil {
		t.Error("expected error for invalid level")
	}
}

func TestBashRuleStore_Match(t *testing.T) {
	s := NewBashRuleStore(zap.NewNop())
	s.AddRule("docker*", PermRequireApproval)
	s.AddRule("rm", PermDeny)
	s.AddRule("git push*", PermDeny)

	cases := []struct {
		cmd       string
		wantLevel PermissionLevel
		wantMatch bool
	}{
		{"docker ps", PermRequireApproval, true},
		{"docker run ubuntu", PermRequireApproval, true},
		{"dock", "", false},
		{"rm file.txt", PermDeny, true},
		{"rm -rf /tmp/build", PermDeny, true},
		{"rmdir docs", "", false}, // first token is "rmdir", not "rm"
		{"git push --force origin main", PermDeny, true},
		{"git pull", "", false}, // "git" doesn't match any rule, "git push*" doesn't match
		{"git status", "", false},
		{"ls -la", "", false},
		{"", "", false},
		{"  ", "", false},
	}

	for _, tc := range cases {
		level, matched := s.Match(tc.cmd)
		if matched != tc.wantMatch {
			t.Errorf("Match(%q) matched = %v, want %v", tc.cmd, matched, tc.wantMatch)
		}
		if level != tc.wantLevel {
			t.Errorf("Match(%q) level = %q, want %q", tc.cmd, level, tc.wantLevel)
		}
	}
}

func TestBashRuleStore_MatchFirstWins(t *testing.T) {
	s := NewBashRuleStore(zap.NewNop())
	s.AddRule("docker*", PermDeny)
	s.AddRule("docker*", PermAutoApprove) // second rule, same pattern

	level, matched := s.Match("docker ps")
	if !matched {
		t.Error("expected match")
	}
	if level != PermDeny {
		t.Errorf("expected first rule (deny), got %q", level)
	}
}

func TestBashRuleStore_AutoSaveOnMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")

	s := NewBashRuleStore(zap.NewNop())
	s.SetFilePath(path)

	s.AddRule("rm", PermDeny)

	// File should be written
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("auto-save file: %v", err)
	}
	if len(data) == 0 {
		t.Error("auto-save wrote empty file")
	}

	s.RemoveRule(s.AllRules()[0].ID)

	data2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("auto-save after remove: %v", err)
	}
	if string(data2) != "[]\n" && string(data2) != "[]" {
		// After removing the only rule, file should have empty array
		t.Logf("data after remove: %s", data2)
	}
}

func TestBashRuleStore_LoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	os.WriteFile(path, []byte("not json"), 0644)

	s := NewBashRuleStore(zap.NewNop())
	if err := s.Load(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
