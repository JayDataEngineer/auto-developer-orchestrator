package llama

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLookupTool(t *testing.T) {
	spec := LookupTool("bash")
	if spec == nil {
		t.Fatal("bash tool not found")
	}
	if spec.Name != "bash" {
		t.Errorf("expected bash, got %q", spec.Name)
	}
	if spec.Category != CategoryExecution {
		t.Errorf("expected execution category, got %q", spec.Category)
	}

	if LookupTool("nonexistent") != nil {
		t.Error("expected nil for nonexistent tool")
	}
}

func TestToolsByNames(t *testing.T) {
	specs := ToolsByNames([]string{"bash", "file_read", "nonexistent"})
	if len(specs) != 2 {
		t.Errorf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Name != "bash" {
		t.Errorf("expected bash, got %q", specs[0].Name)
	}
	if specs[1].Name != "file_read" {
		t.Errorf("expected file_read, got %q", specs[1].Name)
	}
}

func TestSubAgentToolSpecs(t *testing.T) {
	// yield_artifact is always included
	specs := SubAgentToolSpecs([]string{"bash", "file_read"})
	if len(specs) != 3 {
		t.Errorf("expected 3 specs (bash + file_read + yield_artifact), got %d", len(specs))
	}
	if specs[0].Name != "yield_artifact" {
		t.Errorf("expected yield_artifact first, got %q", specs[0].Name)
	}

	// Orchestration tools are excluded from sub-agents
	specs = SubAgentToolSpecs([]string{"bash", "delegate_to"})
	foundDelegate := false
	for _, s := range specs {
		if s.Name == "delegate_to" {
			foundDelegate = true
		}
	}
	if foundDelegate {
		t.Error("delegate_to should be excluded from sub-agent tools")
	}

	// Nonexistent tools are skipped
	specs = SubAgentToolSpecs([]string{"bash", "nonexistent_tool"})
	if len(specs) != 2 {
		t.Errorf("expected 2 specs (bash + yield_artifact), got %d", len(specs))
	}
}

func TestFormatToolList(t *testing.T) {
	specs := []ToolSpec{
		{Name: "bash", Description: "run a shell command", Schema: `{"command":"ls"}`},
	}
	result := FormatToolList(specs)
	if !strings.Contains(result, "## bash") {
		t.Error("expected tool list to contain ## bash")
	}
	if !strings.Contains(result, "\"command\":\"ls\"") {
		t.Error("expected tool list to contain schema")
	}
}

func TestPersonaToolNames_Orchestrator(t *testing.T) {
	names := PersonaToolNames(PersonaOrchestrator)
	if len(names) == 0 {
		t.Fatal("expected non-empty orchestrator tool names")
	}

	// Key orchestrator tools must be present
	mustHave := []string{"bash", "delegate_to", "create_plan", "synthesize", "browse_to"}
	for _, name := range mustHave {
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("orchestrator tool list missing %q", name)
		}
	}
}

func TestPersonaToolNames_Default(t *testing.T) {
	if PersonaToolNames(PersonaType("unknown")) != nil {
		t.Error("expected nil for unknown persona type")
	}
}

func TestToOpenAITools(t *testing.T) {
	specs := []ToolSpec{
		{
			Name:             "bash",
			Description:      "run a shell command",
			ParametersSchema: `{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`,
		},
	}
	tools := ToOpenAITools(specs)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Type != "function" {
		t.Errorf("expected type function, got %q", tools[0].Type)
	}
	if tools[0].Function.Name != "bash" {
		t.Errorf("expected name bash, got %q", tools[0].Function.Name)
	}

	// Parameters should be valid JSON
	var params map[string]interface{}
	if err := json.Unmarshal(tools[0].Function.Parameters, &params); err != nil {
		t.Errorf("parameters is not valid JSON: %v", err)
	}
}

func TestSchemaExampleToJSONSchema(t *testing.T) {
	tests := []struct {
		name    string
		example string
		check   func(*testing.T, json.RawMessage)
	}{
		{
			name:    "empty",
			example: "",
			check: func(t *testing.T, out json.RawMessage) {
				var m map[string]interface{}
				json.Unmarshal(out, &m)
				if m["type"] != "object" {
					t.Error("expected type object for empty input")
				}
			},
		},
		{
			name:    "empty braces",
			example: "{}",
			check: func(t *testing.T, out json.RawMessage) {
				var m map[string]interface{}
				json.Unmarshal(out, &m)
				if m["type"] != "object" {
					t.Error("expected type object for empty braces")
				}
			},
		},
		{
			name:    "with string property",
			example: `{"command": "ls"}`,
			check: func(t *testing.T, out json.RawMessage) {
				var m map[string]interface{}
				json.Unmarshal(out, &m)
				props, ok := m["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("expected properties map")
				}
				cmd, ok := props["command"].(map[string]interface{})
				if !ok {
					t.Fatal("expected command property")
				}
				if cmd["type"] != "string" {
					t.Errorf("expected type string, got %v", cmd["type"])
				}
			},
		},
		{
			name:    "invalid json",
			example: "not json",
			check: func(t *testing.T, out json.RawMessage) {
				var m map[string]interface{}
				json.Unmarshal(out, &m)
				if m["type"] != "object" {
					t.Error("expected type object for invalid json")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := schemaExampleToJSONSchema(tt.example)
			tt.check(t, out)
		})
	}
}

func TestAllToolsInit(t *testing.T) {
	// Verify that all category init() functions ran and populated allTools
	if len(allTools) < 10 {
		t.Errorf("expected at least 10 tools in allTools, got %d", len(allTools))
	}

	// Every tool must have a non-empty Name
	for i, spec := range allTools {
		if spec.Name == "" {
			t.Errorf("allTools[%d] has empty Name", i)
		}
		if spec.Description == "" {
			t.Errorf("allTools[%d] (%s) has empty Description", i, spec.Name)
		}
	}

	// Every tool in toolIndex must be in allTools
	if len(toolIndex) != len(allTools) {
		t.Errorf("toolIndex size (%d) != allTools size (%d)", len(toolIndex), len(allTools))
	}
}
