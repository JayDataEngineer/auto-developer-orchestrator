package meta

import (
	"context"
	"encoding/json"
	"time"
)

// WaitTool implements core.Tool for waiting/pausing.
type WaitTool struct{}

func NewWaitTool() *WaitTool { return &WaitTool{} }

func (t *WaitTool) Name() string        { return "wait" }
func (t *WaitTool) Description() string { return "Wait/pause for a specified number of seconds" }

func (t *WaitTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"seconds": {"type": "integer", "description": "Number of seconds to wait (max 30)"}
		}
	}`)
}

func (t *WaitTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	seconds := 2
	if s, ok := args["seconds"].(float64); ok && s > 0 && s <= 30 {
		seconds = int(s)
	}
	time.Sleep(time.Duration(seconds) * time.Second)
	return map[string]any{"output": "Waited " + itoa(seconds) + " seconds"}, nil
}

// YieldArtifactTool signals that the agent's task is complete.
type YieldArtifactTool struct{}

func NewYieldArtifactTool() *YieldArtifactTool { return &YieldArtifactTool{} }

func (t *YieldArtifactTool) Name() string        { return "yield_artifact" }
func (t *YieldArtifactTool) Description() string { return "Signal that the task is complete and return the result" }

func (t *YieldArtifactTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"output": {"type": "string", "description": "The final output of the task"}
		},
		"required": ["output"]
	}`)
}

func (t *YieldArtifactTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	output, _ := args["output"].(string)
	return map[string]any{"yielded": true, "output": output}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
