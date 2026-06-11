package base

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

type ToolFunc func(ctx context.Context, args map[string]any) (any, error)

type Config struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Execute     ToolFunc
}

type Tool struct {
	name        string
	description string
	schema      json.RawMessage
	execute     ToolFunc
}

func New(name, description string, schema json.RawMessage, fn ToolFunc) *Tool {
	return &Tool{
		name:        name,
		description: description,
		schema:      schema,
		execute:     fn,
	}
}

func (t *Tool) Name() string        { return t.name }
func (t *Tool) Description() string { return t.description }
func (t *Tool) Schema() json.RawMessage {
	if t.schema == nil {
		return json.RawMessage(`{}`)
	}
	return t.schema
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.execute(ctx, args)
}

func (t *Tool) ExecuteWithDefault(ctx context.Context, args map[string]any, defaults map[string]any) (any, error) {
	merged := make(map[string]any)
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range args {
		merged[k] = v
	}
	return t.execute(ctx, merged)
}

func StringArg(args map[string]any, key string) (string, bool) {
	if v, ok := args[key].(string); ok && v != "" {
		return v, true
	}
	return "", false
}

func StringArgDefault(args map[string]any, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

func MapArg(args map[string]any, key string) (map[string]any, bool) {
	if v, ok := args[key].(map[string]any); ok {
		return v, true
	}
	return nil, false
}

func IntArg(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case string:
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i, true
		}
	}
	return 0, false
}

func BoolArg(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	if v, ok := args[key].(string); ok {
		return v == "true" || v == "1" || v == "yes"
	}
	return false
}

func RequiredArg(args map[string]any, keys ...string) error {
	for _, k := range keys {
		if _, ok := args[k]; !ok {
			return &core.ToolError{ToolName: "unknown", Message: fmt.Sprintf("missing required parameter '%s'", k)}
		}
	}
	return nil
}

type RegistryFunc func(tools []core.Tool) []core.Tool

func Combine(fns ...RegistryFunc) RegistryFunc {
	return func(tools []core.Tool) []core.Tool {
		for _, fn := range fns {
			tools = fn(tools)
		}
		return tools
	}
}