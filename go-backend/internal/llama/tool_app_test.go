package llama

import (
	"strings"
	"testing"
)

func TestParseHandlerParams(t *testing.T) {
	tests := []struct {
		handler string
		want    []string
	}{
		{"python -m dre research {query}", []string{"query"}},
		{"python bot.py --signal {signals} --days {days}", []string{"signals", "days"}},
		{"python health.py", nil},
		{"echo {input}", []string{"input"}},
		{"cmd {a} {b} {a}", []string{"a", "b"}}, // dedup
	}
	for _, tt := range tests {
		got := parseHandlerParams(tt.handler)
		if len(got) != len(tt.want) {
			t.Errorf("parseHandlerParams(%q) = %v, want %v", tt.handler, got, tt.want)
			continue
		}
		for i, v := range got {
			if v != tt.want[i] {
				t.Errorf("parseHandlerParams(%q)[%d] = %q, want %q", tt.handler, i, v, tt.want[i])
			}
		}
	}
}

func TestResolveHandlerTemplate(t *testing.T) {
	got := resolveHandlerTemplate("python -m dre research {query}", map[string]interface{}{"query": "NVDA earnings"})
	if got != "python -m dre research NVDA earnings" {
		t.Errorf("got %q", got)
	}

	// Multiple params
	got = resolveHandlerTemplate("cmd {a} {b}", map[string]interface{}{"a": "x", "b": "y"})
	if got != "cmd x y" {
		t.Errorf("got %q", got)
	}

	// Missing param leaves token empty
	got = resolveHandlerTemplate("cmd {a} {b}", map[string]interface{}{"a": "x"})
	if got != "cmd x " {
		t.Errorf("got %q", got)
	}
}

func TestRegisterAndUnregisterAppTools(t *testing.T) {
	// Clean up global state after test
	defer func() {
		appToolNames = nil
		appToolRegistry = map[string]*AppToolRegistration{}
	}()

	names := RegisterAppTools([]AppToolRegistration{
		{ProjectName: "dre", ToolName: "deep_research", Handler: "python -m dre research {query}", Description: "Deep research"},
		{ProjectName: "dre", ToolName: "scrape_url", Handler: "python -m dre scrape {url}", Description: "Scrape URL"},
	})

	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "app_deep_research" {
		t.Errorf("expected app_deep_research, got %s", names[0])
	}
	if names[1] != "app_scrape_url" {
		t.Errorf("expected app_scrape_url, got %s", names[1])
	}

	// Verify lookup
	reg := LookupAppTool("app_deep_research")
	if reg == nil {
		t.Fatal("expected non-nil registration")
	}
	if reg.ProjectName != "dre" {
		t.Errorf("expected dre, got %s", reg.ProjectName)
	}

	// Verify tool is in the global registry
	spec := LookupTool("app_deep_research")
	if spec == nil {
		t.Fatal("expected tool in global registry")
	}
	if spec.Category != CategoryExecution {
		t.Errorf("expected CategoryExecution, got %s", spec.Category)
	}

	// Unregister
	UnregisterAppTools("dre")
	if LookupAppTool("app_deep_research") != nil {
		t.Error("expected nil after unregister")
	}
	if len(appToolNames) != 0 {
		t.Errorf("expected 0 app tool names, got %d", len(appToolNames))
	}
}

func TestAppToolReference(t *testing.T) {
	defer func() {
		appToolNames = nil
		appToolRegistry = map[string]*AppToolRegistration{}
	}()

	// Empty case
	if ref := AppToolReference(); ref != "" {
		t.Errorf("expected empty reference, got %q", ref)
	}

	RegisterAppTools([]AppToolRegistration{
		{ProjectName: "test", ToolName: "my_tool", Handler: "cmd {input}", Description: "Does stuff"},
	})

	ref := AppToolReference()
	if !strings.Contains(ref, "app_my_tool(input)") {
		t.Errorf("reference should contain tool signature, got: %s", ref)
	}
	if !strings.Contains(ref, "Does stuff") {
		t.Errorf("reference should contain description, got: %s", ref)
	}
}

func TestBuildAppToolSchema(t *testing.T) {
	schema := buildAppToolSchema([]string{"query"})
	if !strings.Contains(schema, `"query"`) || !strings.Contains(schema, `"required"`) {
		t.Errorf("schema missing required field: %s", schema)
	}

	// No params
	schema = buildAppToolSchema(nil)
	if schema != `{"type":"object","properties":{}}` {
		t.Errorf("unexpected empty schema: %s", schema)
	}
}
