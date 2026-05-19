package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dop251/goja"
)

const (
	defaultTimeout = 10 * time.Second
	maxOutputChars = 50000
)

// EvalTool runs JavaScript in a sandboxed ES5.1+ runtime.
// No filesystem, no network, no imports. The agent uses this for
// deterministic transforms: JSON manipulation, batch operations,
// data pipelines, retry logic, loops, and string processing.
//
// The runtime provides:
//   - JSON.parse / JSON.stringify
//   - console.log (captures output)
//   - Math, Date, RegExp, Array, Object, String, Number, Map, Set
//   - A "data" global if the agent passes input data
type EvalTool struct {
	timeout time.Duration
	logger  *log.Logger
}

func NewEvalTool() *EvalTool {
	return &EvalTool{
		timeout: defaultTimeout,
		logger:  log.Default(),
	}
}

func (t *EvalTool) Name() string { return "eval" }

func (t *EvalTool) Description() string {
	return "Run JavaScript code in a sandboxed runtime. Use for JSON transforms, " +
		"batch operations, data pipelines, string processing, loops, and math. " +
		"No filesystem or network access. Provide 'data' parameter to make it " +
		"available as a global variable. Return a value with 'return' or set 'result'. " +
		"Captured console.log output is returned alongside the result."
}

func (t *EvalTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"code": {
				"type": "string",
				"description": "JavaScript code to execute. Use 'return' to return a value, or set the global 'result' variable."
			},
			"data": {
				"description": "Optional input data. Available as the global 'data' variable in the script. Can be any JSON value.",
				"type": "string"
			},
			"timeout_ms": {
				"type": "integer",
				"description": "Execution timeout in milliseconds (default 10000, max 30000)."
			}
		},
		"required": ["code"]
	}`)
}

func (t *EvalTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	code, _ := args["code"].(string)
	if code == "" {
		return map[string]any{"error": "no code provided"}, nil
	}

	// Parse timeout
	timeout := t.timeout
	if ms, ok := args["timeout_ms"].(float64); ok && ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
		if timeout > 30*time.Second {
			timeout = 30 * time.Second
		}
	}

	// Parse input data
	var dataValue any
	if dataStr, ok := args["data"].(string); ok && dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &dataValue); err != nil {
			return map[string]any{
				"error": fmt.Sprintf("invalid JSON in data parameter: %v", err),
			}, nil
		}
	}

	// Run with timeout
	type evalResult struct {
		value any
		err   error
	}
	ch := make(chan evalResult, 1)

	go func() {
		val, err := t.runSandboxed(code, dataValue)
		ch <- evalResult{value: val, err: err}
	}()

	select {
	case <-ctx.Done():
		return map[string]any{"error": "cancelled"}, nil
	case result := <-ch:
		if result.err != nil {
			return map[string]any{
				"error":   result.err.Error(),
				"success": false,
			}, nil
		}
		return result.value, nil
	case <-time.After(timeout):
		return map[string]any{
			"error":   fmt.Sprintf("execution timed out after %v", timeout),
			"success": false,
		}, nil
	}
}

func (t *EvalTool) runSandboxed(code string, data any) (any, error) {
	vm := goja.New()

	// Console capture
	var consoleOutput []string
	consoleLog := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = arg.String()
		}
		consoleOutput = append(consoleOutput, parts...)
		return goja.Undefined()
	}

	consoleObj := vm.NewObject()
	_ = consoleObj.Set("log", consoleLog)
	_ = consoleObj.Set("warn", consoleLog)
	_ = consoleObj.Set("error", consoleLog)
	_ = vm.Set("console", consoleObj)

	// JSON global (goja provides this natively, but ensure it's there)
	// goja includes JSON.parse/stringify by default

	// Set input data
	if data != nil {
		_ = vm.Set("data", data)
	}

	// Wrap code to capture result
	// If the code uses 'return', it's treated as a function body.
	// If not, we check for a 'result' global after execution.
	wrapped := "(function() {\n" + code + "\n})"
	result, err := vm.RunString(wrapped)
	if err != nil {
		return map[string]any{
			"error":   cleanJSerror(err),
			"success": false,
		}, nil
	}

	// Extract return value
	var returnValue any
	if result != nil && !goja.IsUndefined(result) {
		returnValue = result.Export()
	} else {
		// Check for 'result' global
		if r := vm.Get("result"); r != nil && !goja.IsUndefined(r) {
			returnValue = r.Export()
		}
	}

	// Build output
	output := map[string]any{
		"success": true,
	}

	if returnValue != nil {
		output["result"] = returnValue
	}

	if len(consoleOutput) > 0 {
		joined := ""
		for _, line := range consoleOutput {
			joined += line + "\n"
		}
		if len(joined) > maxOutputChars {
			joined = joined[:maxOutputChars] + "\n...[output truncated]"
		}
		output["output"] = joined
	}

	return output, nil
}

// cleanJSerror extracts the useful message from a goja exception.
func cleanJSerror(err error) string {
	msg := err.Error()
	// goja errors often have a prefix like "ReferenceError: ..."
	// Keep the message as-is but cap length
	if len(msg) > 500 {
		return msg[:500] + "..."
	}
	return msg
}
