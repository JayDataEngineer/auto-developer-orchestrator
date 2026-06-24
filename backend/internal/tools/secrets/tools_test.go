package secrets

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
)

// TestListTool_SchemaIsValid enforces the contract that every tool.Schema()
// returns JSON-parseable bytes. Part of the PR3 tool audit gate.
func TestListTool_SchemaIsValid(t *testing.T) {
	testutil.AssertValidSchema(t, NewListTool(nil))
}

func TestInjectTool_SchemaIsValid(t *testing.T) {
	testutil.AssertValidSchema(t, NewInjectTool(nil))
}

// TestListTool_NilStore proves list_secrets degrades gracefully when no
// store is configured — returns empty domains, not an error.
func TestListTool_NilStore(t *testing.T) {
	tool := NewListTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{})
	testutil.AssertNoError(t, err)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, hasDomains := m["domains"]; !hasDomains {
		t.Errorf("expected 'domains' key in result, got %v", m)
	}
}

// TestListTool_WithStore proves list_secrets returns domain keys without
// leaking values. The contract: model never sees secret values via list.
func TestListTool_WithStore(t *testing.T) {
	store := sensitive.NewStore()
	store.Set("openrouter", "api_key", "sk-real-secret-value")
	store.Set("openrouter", "user_id", "u-12345")
	store.Set("anthropic", "api_key", "sk-ant-other")

	tool := NewListTool(store)
	result, err := tool.Execute(context.Background(), map[string]any{})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	domains, _ := m["domains"].(map[string][]string)

	if _, ok := domains["openrouter"]; !ok {
		t.Errorf("expected 'openrouter' domain in result; got %v", domains)
	}
	if len(domains["openrouter"]) != 2 {
		t.Errorf("expected 2 openrouter keys, got %d (%v)", len(domains["openrouter"]), domains["openrouter"])
	}

	// Critical contract: no secret VALUE ever appears in list output.
	body := strings.ToLower(stringifyMap(m))
	if strings.Contains(body, "sk-real-secret-value") {
		t.Errorf("list_secrets LEAKED secret value: %s", body)
	}
	if strings.Contains(body, "sk-ant-other") {
		t.Errorf("list_secrets LEAKED secret value: %s", body)
	}
}

// TestInjectTool_MissingArgs proves inject_secret requires both domain + key.
func TestInjectTool_MissingArgs(t *testing.T) {
	store := sensitive.NewStore()
	store.Set("openrouter", "api_key", "sk-real")
	tool := NewInjectTool(store)

	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing domain", map[string]any{"key": "api_key"}},
		{"missing key", map[string]any{"domain": "openrouter"}},
		{"empty", map[string]any{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), c.args)
			if err == nil {
				t.Errorf("expected error for %s", c.name)
			}
		})
	}
}

// TestInjectTool_NotFound proves inject_secret returns a structured error
// when the secret doesn't exist — without leaking whether other secrets exist.
func TestInjectTool_NotFound(t *testing.T) {
	store := sensitive.NewStore()
	store.Set("openrouter", "api_key", "sk-real")
	tool := NewInjectTool(store)

	result, err := tool.Execute(context.Background(), map[string]any{
		"domain": "openrouter",
		"key":    "nonexistent",
	})
	testutil.AssertNoError(t, err)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map (structured error), got %T", result)
	}
	if _, hasErr := m["error"]; !hasErr {
		t.Errorf("expected 'error' key in not-found result, got %v", m)
	}
}

// TestInjectTool_ReturnsPlaceholder proves a valid inject_secret call
// returns a <secret>domain.key</secret> placeholder, NEVER the real value.
func TestInjectTool_ReturnsPlaceholder(t *testing.T) {
	store := sensitive.NewStore()
	store.Set("openrouter", "api_key", "sk-real-secret-value-DO-NOT-LEAK")
	tool := NewInjectTool(store)

	result, err := tool.Execute(context.Background(), map[string]any{
		"domain": "openrouter",
		"key":    "api_key",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	placeholder, _ := m["placeholder"].(string)

	if !strings.Contains(placeholder, "<secret>openrouter.api_key</secret>") {
		t.Errorf("expected <secret>openrouter.api_key</secret> placeholder, got %q", placeholder)
	}
	// Critical contract: the real value MUST NOT appear in the result.
	body := stringifyMap(m)
	if strings.Contains(body, "sk-real-secret-value-DO-NOT-LEAK") {
		t.Errorf("inject_secret LEAKED real value: %s", body)
	}
}

// TestAllTools_NilStoreReturnsNil proves AllTools refuses to register
// secrets tools when no store is configured — prevents the model from
// seeing a tool that would only error on every call.
func TestAllTools_NilStoreReturnsNil(t *testing.T) {
	if tools := AllTools(nil); tools != nil {
		t.Errorf("expected nil from AllTools(nil), got %d tools", len(tools))
	}
}

// TestAllTools_RegistersBothTools confirms AllTools registers the full
// secrets toolset — single source of truth for orchestrator wiring.
func TestAllTools_RegistersBothTools(t *testing.T) {
	store := sensitive.NewStore()
	store.Set("test", "key", "v")
	tools := AllTools(store)

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	if !names["list_secrets"] {
		t.Errorf("AllTools missing list_secrets; got %v", names)
	}
	if !names["inject_secret"] {
		t.Errorf("AllTools missing inject_secret; got %v", names)
	}
}

// stringifyMap flattens a map[string]any to a string for substring assertions.
// Used to prove no secret value leaks into the tool result. Uses Sprintf for
// unknown types so we always get a deterministic string for substring search.
func stringifyMap(m map[string]any) string {
	var b strings.Builder
	for k, v := range m {
		b.WriteString(k)
		b.WriteString("=")
		switch x := v.(type) {
		case string:
			b.WriteString(x)
		case []string:
			for _, s := range x {
				b.WriteString(s)
			}
		case map[string][]string:
			for _, ss := range x {
				for _, s := range ss {
					b.WriteString(s)
				}
			}
		default:
			b.WriteString(strings.ToLower(fmt.Sprintf("%v", v)))
		}
		b.WriteString("\n")
	}
	return b.String()
}
