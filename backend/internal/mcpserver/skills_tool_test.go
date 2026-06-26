package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkillFile creates <root>/skills/<name>/SKILL.md. Test helper.
func writeSkillFile(t *testing.T, root, name, desc, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestListSkillsReturnsAllSkills proves a project with multiple skills
// surfaces every SKILL.md via list_skills. Metadata only — Content stays
// empty in the list response.
func TestListSkillsReturnsAllSkills(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, root, "alpha", "Alpha summary", "alpha body")
	writeSkillFile(t, root, "beta", "Beta summary", "beta body")

	srv := New("skills-test", "0.0.1", nil)
	srv.RegisterTool(NewListSkillsTool(root))

	resp := srv.Dispatch(context.Background(), mustRPCCall(t, "tools/call", map[string]any{
		"name":      "list_skills",
		"arguments": map[string]any{},
	}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch: %+v", resp)
	}

	// Result is mcpToolResult — pull the text out, parse, verify.
	text := resp.Result.(mcpToolResult).Content[0].Text
	var parsed struct {
		Skills []struct {
			Name, Description, Path string
		} `json:"skills"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, text)
	}
	if parsed.Count != 2 {
		t.Errorf("count: got %d want 2", parsed.Count)
	}
	names := []string{parsed.Skills[0].Name, parsed.Skills[1].Name}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("names: got %v want [alpha beta]", names)
	}
}

// TestListSkillsEmptyWhenNoSkillsDir proves the common case (project has no
// skills/) returns an empty list, not an error.
func TestListSkillsEmptyWhenNoSkillsDir(t *testing.T) {
	root := t.TempDir()
	srv := New("skills-test", "0.0.1", nil)
	srv.RegisterTool(NewListSkillsTool(root))

	resp := srv.Dispatch(context.Background(), mustRPCCall(t, "tools/call", map[string]any{
		"name":      "list_skills",
		"arguments": map[string]any{},
	}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch: %+v", resp)
	}
	text := resp.Result.(mcpToolResult).Content[0].Text
	var parsed struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, text)
	}
	if parsed.Count != 0 {
		t.Errorf("count: got %d want 0", parsed.Count)
	}
}

// TestLoadSkillReturnsBody proves load_skill pulls the full markdown body
// — distinct from list_skills which omits it.
func TestLoadSkillReturnsBody(t *testing.T) {
	root := t.TempDir()
	writeSkillFile(t, root, "deep", "Deep skill", "# Deep\n\nDetailed body.")

	srv := New("skills-test", "0.0.1", nil)
	srv.RegisterTool(NewLoadSkillTool(root))

	resp := srv.Dispatch(context.Background(), mustRPCCall(t, "tools/call", map[string]any{
		"name":      "load_skill",
		"arguments": map[string]any{"name": "deep"},
	}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch: %+v", resp)
	}
	text := resp.Result.(mcpToolResult).Content[0].Text
	var parsed struct {
		Name, Description, Path, Content string
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, text)
	}
	if parsed.Name != "deep" {
		t.Errorf("name: got %q want deep", parsed.Name)
	}
	if parsed.Description != "Deep skill" {
		t.Errorf("description: got %q", parsed.Description)
	}
	if parsed.Content != "# Deep\n\nDetailed body." {
		t.Errorf("content: got %q", parsed.Content)
	}
}

// TestLoadSkillMissingReturnsToolError proves a missing skill produces
// isError=true (MCP tool error), NOT a JSON-RPC error. This is the
// load-bearing correctness split — see server.go::handleToolsCall.
func TestLoadSkillMissingReturnsToolError(t *testing.T) {
	root := t.TempDir()
	srv := New("skills-test", "0.0.1", nil)
	srv.RegisterTool(NewLoadSkillTool(root))

	resp := srv.Dispatch(context.Background(), mustRPCCall(t, "tools/call", map[string]any{
		"name":      "load_skill",
		"arguments": map[string]any{"name": "ghost"},
	}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch: %+v", resp)
	}
	res := resp.Result.(mcpToolResult)
	if !res.IsError {
		t.Errorf("expected isError=true for missing skill, got: %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "not found") {
		t.Errorf("error text should mention 'not found', got: %s", res.Content[0].Text)
	}
}

// TestLoadSkillMissingName proves the args validation path: missing 'name'
// produces a tool error too.
func TestLoadSkillMissingName(t *testing.T) {
	root := t.TempDir()
	srv := New("skills-test", "0.0.1", nil)
	srv.RegisterTool(NewLoadSkillTool(root))

	resp := srv.Dispatch(context.Background(), mustRPCCall(t, "tools/call", map[string]any{
		"name":      "load_skill",
		"arguments": map[string]any{},
	}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch: %+v", resp)
	}
	res := resp.Result.(mcpToolResult)
	if !res.IsError {
		t.Errorf("expected isError=true for missing name arg")
	}
}

// mustRPCCall wraps method+params into the JSON-RPC envelope Dispatch expects.
// Local helper so this test file doesn't depend on audit_hook_test.go's
// mustRPC (test files in the same package can see each other's helpers,
// but the names stay clear when each file is self-documenting).
func mustRPCCall(t *testing.T, method string, params map[string]any) []byte {
	t.Helper()
	envelope := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
	b, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal rpc: %v", err)
	}
	return b
}
