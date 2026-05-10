package perms

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewToolPermissionConfig_Defaults(t *testing.T) {
	c := NewToolPermissionConfig(zap.NewNop())
	perms := c.AllPermissions()

	expected := map[string]PermissionLevel{
		"bash":         PermAutoApprove,
		"write":        PermAutoApprove,
		"edit":         PermAutoApprove,
		"delete":       PermRequireApproval,
		"git_push":     PermRequireApproval,
		"git_reset":    PermRequireApproval,
		"web_fetch":    PermAutoApprove,
		"computer_use": PermAutoApprove,
	}
	for tool, level := range expected {
		p, ok := perms[tool]
		if !ok {
			t.Errorf("missing default for tool %q", tool)
			continue
		}
		if p.Level != level {
			t.Errorf("tool %q: expected level %q, got %q", tool, level, p.Level)
		}
	}
}

func TestSetPermission_UpdateExisting(t *testing.T) {
	c := NewToolPermissionConfig(zap.NewNop())
	c.SetPermission("bash", PermDeny, "security policy")

	p := c.AllPermissions()["bash"]
	if p.Level != PermDeny {
		t.Errorf("expected PermDeny, got %q", p.Level)
	}
	if p.Reason != "security policy" {
		t.Errorf("expected reason 'security policy', got %q", p.Reason)
	}
}

func TestSetPermission_NewTool(t *testing.T) {
	c := NewToolPermissionConfig(zap.NewNop())
	c.SetPermission("custom_tool", PermRequireApproval, "requires confirmation")

	p := c.AllPermissions()["custom_tool"]
	if p.Level != PermRequireApproval {
		t.Errorf("expected PermRequireApproval, got %q", p.Level)
	}
}

func TestSetPermission_InvalidLevel(t *testing.T) {
	c := NewToolPermissionConfig(zap.NewNop())
	c.SetPermission("bash", PermissionLevel("invalid"), "")

	p := c.AllPermissions()["bash"]
	if p.Level != PermAutoApprove {
		t.Errorf("expected unchanged PermAutoApprove, got %q", p.Level)
	}
}

func TestAllPermissions_Snapshot(t *testing.T) {
	c := NewToolPermissionConfig(zap.NewNop())
	first := c.AllPermissions()
	c.SetPermission("bash", PermDeny, "")
	second := c.AllPermissions()

	// First snapshot should be unchanged
	if first["bash"].Level != PermAutoApprove {
		t.Error("first snapshot should have original value")
	}
	if second["bash"].Level != PermDeny {
		t.Error("second snapshot should have updated value")
	}
}

func TestModelConfigProvider_Default(t *testing.T) {
	provider := ModelConfigProvider("test-model")
	if provider != "llamacpp" {
		t.Errorf("expected default provider 'llamacpp', got %q", provider)
	}
}
