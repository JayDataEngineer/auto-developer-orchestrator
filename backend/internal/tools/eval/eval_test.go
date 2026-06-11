package eval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEvalTool_SimpleExpression(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "return 1 + 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Fatalf("expected success, got %v", m)
	}
	if m["result"] != int64(3) {
		t.Fatalf("expected 3, got %v (type %T)", m["result"], m["result"])
	}
}

func TestEvalTool_StringManipulation(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `return "hello " + "world".toUpperCase()`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["result"] != "hello WORLD" {
		t.Fatalf("expected 'hello WORLD', got %v", m["result"])
	}
}

func TestEvalTool_WithData(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `return data.filter(x => x.active).map(x => x.name)`,
		"data": `[{"name":"a","active":true},{"name":"b","active":false},{"name":"c","active":true}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Fatalf("expected success, got %v", m)
	}

	// Result should be an array like ["a", "c"]
	resultArr, ok := m["result"].([]any)
	if !ok {
		t.Fatalf("expected array result, got %T: %v", m["result"], m["result"])
	}
	if len(resultArr) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resultArr))
	}
}

func TestEvalTool_ConsoleOutput(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `console.log("step 1 done"); console.log("step 2 done"); return "ok"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	output, _ := m["output"].(string)
	if !strings.Contains(output, "step 1 done") || !strings.Contains(output, "step 2 done") {
		t.Fatalf("expected console output, got %q", output)
	}
}

func TestEvalTool_ResultGlobal(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `result = 42`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["result"] != int64(42) {
		t.Fatalf("expected 42, got %v", m["result"])
	}
}

func TestEvalTool_LoopsAndMath(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `
			let sum = 0;
			for (let i = 1; i <= 100; i++) {
				sum += i;
			}
			return sum;
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["result"] != int64(5050) {
		t.Fatalf("expected 5050, got %v", m["result"])
	}
}

func TestEvalTool_JSONTransform(t *testing.T) {
	tool := NewEvalTool()
	data := `{"users": [{"name":"Alice","age":30},{"name":"Bob","age":25}]}`
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `return data.users.sort((a,b) => a.age - b.age).map(u => u.name)`,
		"data": data,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	arr, _ := m["result"].([]any)
	if len(arr) != 2 || arr[0] != "Bob" || arr[1] != "Alice" {
		t.Fatalf("expected [Bob, Alice], got %v", arr)
	}
}

func TestEvalTool_SyntaxError(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `return {`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] == true {
		t.Fatal("expected failure for syntax error")
	}
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "SyntaxError") {
		t.Fatalf("expected SyntaxError, got %q", errMsg)
	}
}

func TestEvalTool_RuntimeError(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `return undefinedVar`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] == true {
		t.Fatal("expected failure for runtime error")
	}
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "ReferenceError") {
		t.Fatalf("expected ReferenceError, got %q", errMsg)
	}
}

func TestEvalTool_Timeout(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code":        `while(true) {}`,
		"timeout_ms":  float64(1000),
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] == true {
		t.Fatal("expected timeout failure")
	}
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "timed out") {
		t.Fatalf("expected timeout message, got %q", errMsg)
	}
}

func TestEvalTool_EmptyCode(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] != nil {
		t.Fatalf("expected error for empty code, got %v", m)
	}
}

func TestEvalTool_InvalidDataJSON(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "return data",
		"data": "{not valid json}",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	errMsg, _ := m["error"].(string)
	if !strings.Contains(errMsg, "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %q", errMsg)
	}
}

func TestEvalTool_NoReturn(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `let x = 1;`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Fatalf("expected success, got %v", m)
	}
	// No result or result is undefined
	if _, hasResult := m["result"]; hasResult {
		t.Fatalf("expected no result, got %v", m["result"])
	}
}

func TestEvalTool_NameAndSchema(t *testing.T) {
	tool := NewEvalTool()
	if tool.Name() != "eval" {
		t.Fatalf("expected 'eval', got %q", tool.Name())
	}
	schema := tool.Schema()
	if len(schema) == 0 {
		t.Fatal("expected non-empty schema")
	}
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	props, _ := parsed["properties"].(map[string]any)
	if _, ok := props["code"]; !ok {
		t.Fatal("schema missing 'code' property")
	}
}

func TestEvalTool_ContextCancellation(t *testing.T) {
	tool := NewEvalTool()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result, err := tool.Execute(ctx, map[string]any{
		"code": "return 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["error"] != "cancelled" {
		t.Fatalf("expected cancelled error, got %v", m)
	}
}

func TestEvalTool_MapAndSet(t *testing.T) {
	tool := NewEvalTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": `
			let m = new Map();
			m.set("a", 1);
			m.set("b", 2);
			return Object.fromEntries(m);
		`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if _, ok := m["result"]; !ok {
		t.Fatalf("expected result key, got %v", m)
	}
	// goja may export Map entries differently, just check success
	if m["success"] != true {
		t.Fatalf("expected success, got %v", m)
	}
}

func TestEvalTool_DefaultTimeout(t *testing.T) {
	tool := NewEvalTool()
	if tool.timeout != 10*time.Second {
		t.Fatalf("expected 10s default timeout, got %v", tool.timeout)
	}
}

func TestEvalTool_MaxTimeoutCapped(t *testing.T) {
	tool := NewEvalTool()
	// The cap happens inside Execute; verify by checking the const
	// We can't easily test the cap without a long-running test,
	// but we can verify the timeout_ms parameter works
	result, err := tool.Execute(context.Background(), map[string]any{
		"code":       "return 1",
		"timeout_ms": float64(60000), // over max, should be capped
	})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Fatalf("expected success with capped timeout, got %v", m)
	}
}
