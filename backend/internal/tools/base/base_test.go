package base

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestNew(t *testing.T) {
	tool := New("my_tool", "My description", json.RawMessage(`{"type":"object"}`), func(ctx context.Context, args map[string]any) (any, error) {
		return "ok", nil
	})

	if tool.Name() != "my_tool" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "my_tool")
	}
	if tool.Description() != "My description" {
		t.Errorf("Description() = %q, want %q", tool.Description(), "My description")
	}
}

func TestTool_Execute(t *testing.T) {
	tool := New("test", "desc", json.RawMessage(`{}`), func(ctx context.Context, args map[string]any) (any, error) {
		return "result", nil
	})
	result, err := tool.Execute(context.Background(), nil)
	testutil.AssertNoError(t, err)
	if result != "result" {
		t.Errorf("Execute() = %v, want %q", result, "result")
	}
}

func TestTool_Execute_Error(t *testing.T) {
	tool := New("test", "desc", json.RawMessage(`{}`), func(ctx context.Context, args map[string]any) (any, error) {
		return nil, errors.New("boom")
	})
	_, err := tool.Execute(context.Background(), nil)
	if err == nil || err.Error() != "boom" {
		t.Errorf("expected error 'boom', got %v", err)
	}
}

func TestTool_Schema_Nil(t *testing.T) {
	tool := New("t", "d", nil, nil)
	schema := tool.Schema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
	typ, _ := parsed["type"].(string)
	if typ != "" {
		// nil schema returns empty object, which is fine
	}
}

func TestTool_ExecuteWithDefault(t *testing.T) {
	var received map[string]any
	tool := New("test", "desc", json.RawMessage(`{}`), func(ctx context.Context, args map[string]any) (any, error) {
		received = args
		return nil, nil
	})

	_, _ = tool.ExecuteWithDefault(context.Background(), map[string]any{"b": "2"}, map[string]any{"a": "1"})
	if received["a"] != "1" {
		t.Errorf("expected default 'a'='1', got %v", received["a"])
	}
	if received["b"] != "2" {
		t.Errorf("expected arg 'b'='2', got %v", received["b"])
	}
}

func TestTool_ExecuteWithDefault_ArgsOverride(t *testing.T) {
	var received map[string]any
	tool := New("test", "desc", json.RawMessage(`{}`), func(ctx context.Context, args map[string]any) (any, error) {
		received = args
		return nil, nil
	})

	_, _ = tool.ExecuteWithDefault(context.Background(), map[string]any{"key": "from_args"}, map[string]any{"key": "from_defaults"})
	if received["key"] != "from_args" {
		t.Errorf("expected args to override defaults, got %v", received["key"])
	}
}

func TestStringArg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		key     string
		wantVal string
		wantOK  bool
	}{
		{name: "present", args: map[string]any{"key": "value"}, key: "key", wantVal: "value", wantOK: true},
		{name: "empty string", args: map[string]any{"key": ""}, key: "key", wantVal: "", wantOK: false},
		{name: "missing", args: map[string]any{}, key: "key", wantVal: "", wantOK: false},
		{name: "wrong type", args: map[string]any{"key": 42}, key: "key", wantVal: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, ok := StringArg(tc.args, tc.key)
			if val != tc.wantVal || ok != tc.wantOK {
				t.Errorf("StringArg() = (%q, %t), want (%q, %t)", val, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestStringArgDefault(t *testing.T) {
	if got := StringArgDefault(map[string]any{"k": "v"}, "k", "def"); got != "v" {
		t.Errorf("StringArgDefault() = %q, want %q", got, "v")
	}
	if got := StringArgDefault(map[string]any{}, "k", "def"); got != "def" {
		t.Errorf("StringArgDefault() = %q, want %q", got, "def")
	}
}

func TestMapArg(t *testing.T) {
	val, ok := MapArg(map[string]any{"m": map[string]any{"a": float64(1)}}, "m")
	if !ok {
		t.Errorf("MapArg() = (%v, %t), want ok=true", val, ok)
	} else if v, exists := val["a"]; !exists || v != float64(1) {
		t.Errorf("MapArg() val[%q] = %v, want float64(1)", "a", v)
	}

	_, ok = MapArg(map[string]any{}, "missing")
	if ok {
		t.Error("MapArg() should return false for missing key")
	}
}

func TestIntArg(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		key     string
		wantVal int
		wantOK  bool
	}{
		{name: "float64", args: map[string]any{"k": float64(42)}, key: "k", wantVal: 42, wantOK: true},
		{name: "int", args: map[string]any{"k": 7}, key: "k", wantVal: 7, wantOK: true},
		{name: "string int", args: map[string]any{"k": "99"}, key: "k", wantVal: 99, wantOK: true},
		{name: "string bad", args: map[string]any{"k": "abc"}, key: "k", wantVal: 0, wantOK: false},
		{name: "missing", args: map[string]any{}, key: "k", wantVal: 0, wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, ok := IntArg(tc.args, tc.key)
			if val != tc.wantVal || ok != tc.wantOK {
				t.Errorf("IntArg() = (%d, %t), want (%d, %t)", val, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestBoolArg(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		key  string
		want bool
	}{
		{name: "bool true", args: map[string]any{"k": true}, key: "k", want: true},
		{name: "bool false", args: map[string]any{"k": false}, key: "k", want: false},
		{name: "string true", args: map[string]any{"k": "true"}, key: "k", want: true},
		{name: "string 1", args: map[string]any{"k": "1"}, key: "k", want: true},
		{name: "string yes", args: map[string]any{"k": "yes"}, key: "k", want: true},
		{name: "string no", args: map[string]any{"k": "no"}, key: "k", want: false},
		{name: "missing", args: map[string]any{}, key: "k", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BoolArg(tc.args, tc.key); got != tc.want {
				t.Errorf("BoolArg() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestRequiredArg(t *testing.T) {
	err := RequiredArg(map[string]any{"a": 1, "b": 2}, "a", "b")
	testutil.AssertNoError(t, err)

	err = RequiredArg(map[string]any{"a": 1}, "a", "b")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestCombine(t *testing.T) {
	add := func(tools []core.Tool) []core.Tool {
		return append(tools, New("a", "", nil, nil))
	}
	add2 := func(tools []core.Tool) []core.Tool {
		return append(tools, New("b", "", nil, nil))
	}
	combined := Combine(add, add2)
	result := combined([]core.Tool{})
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	if result[0].Name() != "a" || result[1].Name() != "b" {
		t.Errorf("unexpected tool order: %v", result)
	}
}
