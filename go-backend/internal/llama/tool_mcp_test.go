package llama

import (
	"encoding/json"
	"testing"
)

func TestSchemaToExample(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		want   string
	}{
		{
			name:   "empty",
			schema: "",
			want:   "{}",
		},
		{
			name:   "string param",
			schema: `{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}`,
			want:   `{"query":"..."}`,
		},
		{
			name:   "integer param",
			schema: `{"type":"object","properties":{"limit":{"type":"integer","description":"Max results"}},"required":["limit"]}`,
			want:   `{"limit":0}`,
		},
		{
			name:   "boolean param",
			schema: `{"type":"object","properties":{"verbose":{"type":"boolean"}},"required":["verbose"]}`,
			want:   `{"verbose":false}`,
		},
		{
			name:   "enum default",
			schema: `{"type":"object","properties":{"method":{"type":"string","enum":["GET","POST","PUT","DELETE"]}},"required":["method"]}`,
			want:   `{"method":"GET"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SchemaToExample(tt.schema)

			// Both got and want should be valid JSON with same structure
			var gotMap, wantMap map[string]interface{}
			if err := json.Unmarshal([]byte(got), &gotMap); err != nil {
				t.Fatalf("got is not valid JSON: %v\n%q", err, got)
			}
			if err := json.Unmarshal([]byte(tt.want), &wantMap); err != nil {
				t.Fatalf("want is not valid JSON: %v", err)
			}

			for k, wantV := range wantMap {
				gotV, ok := gotMap[k]
				if !ok {
					t.Errorf("missing key %q in got: %v", k, gotMap)
					continue
				}
				if gotV != wantV || !ok {
					t.Errorf("%q: got %v, want %v", k, gotV, wantV)
				}
			}
		})
	}
}

func TestExtractParamSummary(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		check  func(t *testing.T, result string)
	}{
		{
			name:   "empty",
			schema: "",
			check: func(t *testing.T, r string) {
				if r != "..." {
					t.Errorf("expected '...', got %q", r)
				}
			},
		},
		{
			name:   "required only",
			schema: `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
			check: func(t *testing.T, r string) {
				if r != "query" {
					t.Errorf("expected 'query', got %q", r)
				}
			},
		},
		{
			name:   "required + notable optional",
			schema: `{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"integer","description":"max number of results"}},"required":["query"]}`,
			check: func(t *testing.T, r string) {
				if !stringsContains(r, "query") {
					t.Errorf("expected to contain 'query', got %q", r)
				}
				if !stringsContains(r, "max_results?") {
					t.Errorf("expected to contain 'max_results?', got %q", r)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractParamSummary(tt.schema)
			tt.check(t, result)
		})
	}
}

func stringsContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestContainsStr(t *testing.T) {
	if !containsStr([]string{"a", "b", "c"}, "b") {
		t.Error("expected true for contains 'b'")
	}
	if containsStr([]string{"a", "b", "c"}, "d") {
		t.Error("expected false for contains 'd'")
	}
	if containsStr(nil, "a") {
		t.Error("expected false for nil slice")
	}
	if containsStr([]string{}, "a") {
		t.Error("expected false for empty slice")
	}
}
