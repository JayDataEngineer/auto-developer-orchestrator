package testutil

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// StubTool is a generic tool stub implementing core.Tool for testing.
// Use it anywhere a core.Tool is needed without real execution.
type StubTool struct {
	NameValue      string
	DescValue      string
	SchemaValue    json.RawMessage
	ExecuteFn      func(ctx context.Context, args map[string]any) (any, error)
}

func (s *StubTool) Name() string {
	if s.NameValue == "" {
		return "stub"
	}
	return s.NameValue
}

func (s *StubTool) Description() string {
	if s.DescValue == "" {
		return "stub tool"
	}
	return s.DescValue
}

func (s *StubTool) Schema() json.RawMessage {
	if s.SchemaValue == nil {
		return json.RawMessage(`{}`)
	}
	return s.SchemaValue
}

func (s *StubTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if s.ExecuteFn != nil {
		return s.ExecuteFn(ctx, args)
	}
	return nil, nil
}

// NewStubTool creates a StubTool with the given name.
func NewStubTool(name string) *StubTool {
	return &StubTool{NameValue: name}
}

// AssertValidSchema verifies a single tool's schema is valid JSON with type=object.
func AssertValidSchema(t *testing.T, tool core.Tool) {
	t.Helper()
	schema := tool.Schema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("tool %q has invalid schema JSON: %v\nraw: %s", tool.Name(), err, string(schema))
	}
	typ, _ := parsed["type"].(string)
	if typ != "object" {
		t.Fatalf("tool %q schema type=%q, want %q", tool.Name(), typ, "object")
	}
}

// AssertValidSchemas verifies all given tools have valid JSON schemas with type=object.
func AssertValidSchemas(t *testing.T, tools []core.Tool) {
	t.Helper()
	for _, tool := range tools {
		AssertValidSchema(t, tool)
	}
}

// AssertToolNames verifies the tools slice matches expected names in order.
func AssertToolNames(t *testing.T, tools []core.Tool, expected []string) {
	t.Helper()
	if len(tools) != len(expected) {
		t.Fatalf("expected %d tools, got %d\n  got:      %v\n  expected: %v",
			len(expected), len(tools), toolNames(tools), expected)
	}
	for i, name := range expected {
		if tools[i].Name() != name {
			t.Errorf("tool[%d]: expected %q, got %q", i, name, tools[i].Name())
		}
	}
}

// AssertMissingParam verifies a tool returns an error when called without the required parameter.
func AssertMissingParam(t *testing.T, tool core.Tool, paramName string) {
	t.Helper()
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatalf("tool %q: expected error for missing parameter %q", tool.Name(), paramName)
	}
}

// AssertErrorContains verifies the error message contains the expected substring.
func AssertErrorContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q\ngot: %v", substr, err)
	}
}

// AssertNoError verifies err is nil. Shorthand for t.Fatalf on unexpected errors.
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertIntField checks that a result map has an int field with the expected value.
func AssertIntField(t *testing.T, result map[string]any, key string, want int) {
	t.Helper()
	got, ok := result[key].(int)
	if !ok {
		// JSON unmarshal may produce float64
		if f, ok := result[key].(float64); ok {
			got = int(f)
		} else {
			t.Fatalf("result[%q] type=%T, want int/float64", key, result[key])
		}
	}
	if got != want {
		t.Errorf("result[%q] = %d, want %d", key, got, want)
	}
}

// AssertStringField checks that a result map has a string field with the expected value.
func AssertStringField(t *testing.T, result map[string]any, key, want string) {
	t.Helper()
	got, ok := result[key].(string)
	if !ok {
		t.Fatalf("result[%q] type=%T, want string", key, result[key])
	}
	if got != want {
		t.Errorf("result[%q] = %q, want %q", key, got, want)
	}
}

// AssertBoolField checks that a result map has a bool field with the expected value.
func AssertBoolField(t *testing.T, result map[string]any, key string, want bool) {
	t.Helper()
	got, ok := result[key].(bool)
	if !ok {
		t.Fatalf("result[%q] type=%T, want bool", key, result[key])
	}
	if got != want {
		t.Errorf("result[%q] = %v, want %v", key, got, want)
	}
}

// AssertJSONValid unmarshals raw JSON and fails if it is not valid.
func AssertJSONValid(t *testing.T, data []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, string(data))
	}
}

// toolNames extracts names from a tool slice for error messages.
func toolNames(tools []core.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}
	return names
}
