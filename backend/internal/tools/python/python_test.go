package python

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

// skipIfNoPython skips the test if python3 is not available.
func skipIfNoPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		if _, err := exec.LookPath("python"); err != nil {
			t.Skip("python3 not found on PATH")
		}
	}
}

func TestPythonTool_Interface(t *testing.T) {
	tool := NewPythonTool()
	if tool.Name() != "python" {
		t.Errorf("expected name 'python', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
	schema := tool.Schema()
	if !json.Valid(schema) {
		t.Errorf("schema should be valid JSON, got: %s", schema)
	}
}

func TestPythonTool_SimplePrint(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "print(1 + 2)",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("expected success=true, got %v", m)
	}
	if m["output"] != "3\n" {
		t.Errorf("expected output '3\\n', got %q", m["output"])
	}
}

func TestPythonTool_MultiLine(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "total = 0\nfor i in range(5):\n    total += i\nprint(total)",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("expected success=true, got %v", m)
	}
	if m["output"] != "10\n" {
		t.Errorf("expected output '10\\n', got %q", m["output"])
	}
}

func TestPythonTool_DataParameter(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool()
	dataJSON := `[1, 2, 3, 4, 5]`
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "print(sum(data))",
		"data": dataJSON,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("expected success=true, got %v", m)
	}
	if m["output"] != "15\n" {
		t.Errorf("expected output '15\\n', got %q", m["output"])
	}
}

func TestPythonTool_SyntaxError(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "print(",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] == true {
		t.Error("expected success=false for syntax error")
	}
	if m["error"] == nil || m["error"] == "" {
		t.Error("expected error to contain syntax error message")
	}
}

func TestPythonTool_EmptyCode(t *testing.T) {
	tool := NewPythonTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["error"] != "no code provided" {
		t.Errorf("expected 'no code provided' error, got %v", m)
	}
}

func TestPythonTool_Timeout(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool(WithTimeout(2 * time.Second))
	result, err := tool.Execute(context.Background(), map[string]any{
		"code":       "import time; time.sleep(60)",
		"timeout_ms": float64(2000),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] == true {
		t.Error("expected success=false for timeout")
	}
	errMsg, _ := m["error"].(string)
	if errMsg == "" {
		t.Error("expected timeout error message")
	}
}

func TestPythonTool_ContextCancellation(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	result, err := tool.Execute(ctx, map[string]any{
		"code": "import time; time.sleep(60)",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] == true {
		t.Error("expected success=false for cancelled context")
	}
}

func TestPythonTool_StderrCapture(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "import sys; sys.stderr.write('stderr output\\n')",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("expected success=true, got %v", m)
	}
	if m["error"] != "stderr output\n" {
		t.Errorf("expected stderr capture, got %q", m["error"])
	}
}

func TestPythonTool_WithWorkDir(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool(WithWorkDir("/tmp"))
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "import os; print(os.getcwd())",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("expected success=true, got %v", m)
	}
	if m["output"] != "/tmp\n" {
		t.Errorf("expected workdir '/tmp', got %q", m["output"])
	}
}

func TestPythonTool_DataDict(t *testing.T) {
	skipIfNoPython(t)
	tool := NewPythonTool()
	dataJSON := `{"name": "test", "values": [1, 2, 3]}`
	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "print(data['name'], len(data['values']))",
		"data": dataJSON,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("expected success=true, got %v", m)
	}
	if m["output"] != "test 3\n" {
		t.Errorf("expected output 'test 3\\n', got %q", m["output"])
	}
}
