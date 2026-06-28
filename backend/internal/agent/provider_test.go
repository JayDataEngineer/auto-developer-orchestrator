package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// sseBuilder constructs Anthropic-format SSE response bodies. Each call to
// event() emits one `event: <t>\ndata: <json>\n\n` block.
type sseBuilder struct {
	b strings.Builder
}

func (b *sseBuilder) event(typ string, payload any) {
	b.b.WriteString("event: ")
	b.b.WriteString(typ)
	b.b.WriteString("\ndata: ")
	enc := json.NewEncoder(&b.b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
	b.b.WriteString("\n")
}

func (b *sseBuilder) bytes() []byte { return []byte(b.b.String()) }

// TestProvider_TextRoundTrip simulates a basic content-only response. The
// fake server replies with a fixed SSE stream of [message_start → text
// deltas → message_delta(end_turn) → message_stop]. Provider should emit
// ChatEventContent chunks and a final ChatEventDone with FinishStop.
func TestProvider_TextRoundTrip(t *testing.T) {
	const apiRoot = "/v1/messages"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiRoot {
			t.Errorf("request path = %q, want %q", r.URL.Path, apiRoot)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header missing")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		var b sseBuilder
		b.event("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant",
				"content": []any{}, "stop_reason": nil,
				"usage": map[string]any{"input_tokens": 10, "output_tokens": 1},
			},
		})
		b.event("content_block_start", map[string]any{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "text": ""},
		})
		b.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "Hello, "},
		})
		b.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "world!"},
		})
		b.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		b.event("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
		b.event("message_stop", map[string]any{"type": "message_stop"})

		_, _ = w.Write(b.bytes())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	p, err := NewProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "claude-test-model",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := p.StreamChat(ctx,
		[]core.Message{{Role: "user", Content: "hi"}},
		nil,
		core.GenerateOptions{},
	)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var sb strings.Builder
	var done *core.ChatEvent
	for ev := range events {
		switch ev.Type {
		case core.ChatEventContent:
			sb.WriteString(ev.Content)
		case core.ChatEventDone:
			done = &ev
		case core.ChatEventError:
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	if got := sb.String(); got != "Hello, world!" {
		t.Errorf("content = %q, want %q", got, "Hello, world!")
	}
	if done == nil {
		t.Fatal("no Done event received")
	}
	if done.Finish != core.FinishStop {
		t.Errorf("finish = %q, want %q", done.Finish, core.FinishStop)
	}
	if done.Usage == nil || done.Usage.CompletionTokens != 5 {
		t.Errorf("usage = %+v, want completion_tokens=5", done.Usage)
	}
}

// TestProvider_ToolUseChunking verifies tool_use events are converted to
// ChatEventToolChunk with proper index tagging so the loop's accumulator can
// stitch them. The fixture emits a tool_use block start (id+name) followed
// by three input_json_delta chunks, then stop_reason=tool_use.
func TestProvider_ToolUseChunking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		var b sseBuilder
		b.event("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant",
				"content": []any{}, "stop_reason": nil,
				"usage": map[string]any{"input_tokens": 10, "output_tokens": 1},
			},
		})
		// Index 0: text preamble
		b.event("content_block_start", map[string]any{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "text": ""},
		})
		b.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "Let me check."},
		})
		b.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})

		// Index 1: tool_use
		b.event("content_block_start", map[string]any{
			"type": "content_block_start", "index": 1,
			"content_block": map[string]any{
				"type": "tool_use", "id": "toolu_1", "name": "bash", "input": map[string]any{},
			},
		})
		b.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 1,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": "{\"cmd\":"},
		})
		b.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 1,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": " \"ls\"}"},
		})
		b.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 1})

		b.event("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "tool_use"},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 20},
		})
		b.event("message_stop", map[string]any{"type": "message_stop"})

		_, _ = w.Write(b.bytes())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	p, _ := NewProvider(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := p.StreamChat(ctx,
		[]core.Message{{Role: "user", Content: "run ls"}},
		[]core.OpenAITool{{
			Type: "function",
			Function: core.FunctionDef{
				Name:        "bash",
				Description: "run a bash command",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}`),
			},
		}},
		core.GenerateOptions{},
	)
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var contentParts []string
	var toolChunks []core.ToolCallDelta
	var finish core.FinishReason
	for ev := range events {
		switch ev.Type {
		case core.ChatEventContent:
			contentParts = append(contentParts, ev.Content)
		case core.ChatEventToolChunk:
			toolChunks = append(toolChunks, ev.Deltas...)
		case core.ChatEventDone:
			finish = ev.Finish
		case core.ChatEventError:
			t.Fatalf("error: %v", ev.Err)
		}
	}

	if got, want := strings.Join(contentParts, ""), "Let me check."; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if finish != core.FinishToolCalls {
		t.Errorf("finish = %q, want %q", finish, core.FinishToolCalls)
	}

	if len(toolChunks) != 3 {
		t.Fatalf("tool chunks = %d, want 3 (1 header + 2 json deltas)", len(toolChunks))
	}
	// First chunk: index=1, id=toolu_1, name=bash.
	if toolChunks[0].Index != 1 || toolChunks[0].ID != "toolu_1" || toolChunks[0].Function.Name != "bash" {
		t.Errorf("first chunk = %+v, want index=1 id=toolu_1 name=bash", toolChunks[0])
	}
	// Stitched args should equal the original JSON.
	args := toolChunks[1].Function.Arguments + toolChunks[2].Function.Arguments
	if args != `{"cmd": "ls"}` {
		t.Errorf("stitched args = %q, want %q", args, `{"cmd": "ls"}`)
	}
}

// TestProvider_SystemAndToolResultEncoding verifies the request body the
// provider emits to Anthropic is shaped correctly. The fake server captures
// the request, then we assert system + message + tool_result fields.
func TestProvider_SystemAndToolResultEncoding(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		var b sseBuilder
		b.event("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": "m", "type": "message", "role": "assistant",
				"content": []any{}, "stop_reason": nil,
				"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
			},
		})
		b.event("content_block_start", map[string]any{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "text": ""},
		})
		b.event("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		})
		b.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		b.event("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
		b.event("message_stop", map[string]any{"type": "message_stop"})
		_, _ = w.Write(b.bytes())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	p, _ := NewProvider(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "test-model"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := []core.Message{
		{Role: "system", Content: "You are a helpful CTO."},
		{Role: "user", Content: "run bash"},
		// Simulated assistant turn that already made a tool call.
		{
			Role:    "assistant",
			Content: "",
			ToolCalls: []core.ToolCallResponse{{
				ID:   "toolu_1",
				Type: "function",
				Function: core.FunctionCallData{
					Name:      "bash",
					Arguments: `{"cmd":"ls"}`,
				},
			}},
		},
		// Tool result fed back.
		{
			Role:       "tool",
			Content:    "file1\nfile2",
			ToolCallID: "toolu_1",
		},
	}

	events, err := p.StreamChat(ctx, messages, nil, core.GenerateOptions{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	// Drain so the request body is fully written.
	for range events {
	}

	var sent map[string]any
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("request not valid JSON: %v\nbody: %s", err, captured)
	}

	// System prompt as array of {type:text, text:...}.
	sys, _ := sent["system"].([]any)
	if len(sys) != 1 {
		t.Fatalf("system len = %d, want 1", len(sys))
	}
	sysBlock, _ := sys[0].(map[string]any)
	if sysBlock["text"] != "You are a helpful CTO." {
		t.Errorf("system[0].text = %q", sysBlock["text"])
	}

	// 3 conversation messages (system stripped, others preserved).
	msgs, _ := sent["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d, want 3", len(msgs))
	}

	// user → text content.
	m0, _ := msgs[0].(map[string]any)
	if m0["role"] != "user" {
		t.Errorf("msgs[0].role = %q, want user", m0["role"])
	}

	// assistant with tool_use block — should have content array containing
	// a tool_use entry.
	m1, _ := msgs[1].(map[string]any)
	if m1["role"] != "assistant" {
		t.Errorf("msgs[1].role = %q, want assistant", m1["role"])
	}
	blocks, _ := m1["content"].([]any)
	var sawToolUse bool
	for _, b := range blocks {
		bm, _ := b.(map[string]any)
		if bm["type"] == "tool_use" && bm["id"] == "toolu_1" && bm["name"] == "bash" {
			sawToolUse = true
		}
	}
	if !sawToolUse {
		t.Errorf("msgs[1] missing tool_use block: %v", m1)
	}

	// tool result → user-role message with tool_result block referencing
	// the original tool_use_id.
	m2, _ := msgs[2].(map[string]any)
	if m2["role"] != "user" {
		t.Errorf("msgs[2].role = %q, want user", m2["role"])
	}
	resBlocks, _ := m2["content"].([]any)
	if len(resBlocks) != 1 {
		t.Fatalf("msgs[2].content len = %d, want 1", len(resBlocks))
	}
	resBlock, _ := resBlocks[0].(map[string]any)
	if resBlock["type"] != "tool_result" {
		t.Errorf("msgs[2].content[0].type = %q, want tool_result", resBlock["type"])
	}
	if resBlock["tool_use_id"] != "toolu_1" {
		t.Errorf("msgs[2].content[0].tool_use_id = %q", resBlock["tool_use_id"])
	}

	if sent["model"] != "test-model" {
		t.Errorf("model = %v", sent["model"])
	}
}

// TestNewProvider_RequiresAPIKey confirms missing key fails fast.
func TestNewProvider_RequiresAPIKey(t *testing.T) {
	_, err := NewProvider(ProviderConfig{})
	if err == nil {
		t.Fatal("NewProvider with empty config: expected error")
	}
}

// TestMapStopReason covers the stop-reason switch.
func TestMapStopReason(t *testing.T) {
	cases := []struct {
		in   anthropic.StopReason
		want core.FinishReason
	}{
		{anthropic.StopReasonToolUse, core.FinishToolCalls},
		{anthropic.StopReasonEndTurn, core.FinishStop},
		{anthropic.StopReasonMaxTokens, core.FinishStop},
		{anthropic.StopReasonStopSequence, core.FinishStop},
		{anthropic.StopReasonPauseTurn, core.FinishStop},
		{anthropic.StopReasonRefusal, core.FinishStop},
	}
	for _, c := range cases {
		if got := mapStopReason(c.in); got != c.want {
			t.Errorf("mapStopReason(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
