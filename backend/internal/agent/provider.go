// Package agent implements the server-side agent loop: an LLM provider that
// talks to Anthropic's Messages API, a Plan/Act/Observe Loop that dispatches
// tool calls back to the existing MCP tool surface, and a delegate_to tool
// for multi-agent delegation (CTO → specialist employees).
//
// The loop is intentionally thin — no hooks, no DB persistence, no SSE
// fan-out. It calls the same MCP tools any external client would.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// AnthropicProvider implements core.LLMProvider against Anthropic's Messages
// API via the official SDK. The conversion between core.Message and the
// SDK's MessageParam is the only non-trivial surface — Anthropic uses a
// "system" top-level param + content-block arrays, while core.Message is
// the OpenAI-style {role, content, tool_calls} shape.
type AnthropicProvider struct {
	client    anthropic.Client
	model     string
	maxTokens int
	ctxSize   int
}

// ProviderConfig carries the knobs a caller can set. Defaults are applied
// by NewProvider — empty fields are not sent to the SDK.
type ProviderConfig struct {
	APIKey      string // required
	BaseURL     string // optional — empty uses api.anthropic.com
	Model       string // optional — empty defaults to claude-sonnet-4-6
	MaxTokens   int    // optional — defaults to 8192
	ContextSize int    // optional — defaults to 200_000 (Sonnet window)
}

// NewProvider constructs a provider from config. Required: APIKey. The
// returned provider is safe for concurrent use — the underlying SDK client
// is goroutine-safe.
func NewProvider(cfg ProviderConfig) (*AnthropicProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("agent: ProviderConfig.APIKey is required")
	}
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	maxTok := cfg.MaxTokens
	if maxTok <= 0 {
		maxTok = 8192
	}
	ctxSize := cfg.ContextSize
	if ctxSize <= 0 {
		ctxSize = 200_000
	}

	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &AnthropicProvider{
		client:    anthropic.NewClient(opts...),
		model:     model,
		maxTokens: maxTok,
		ctxSize:   ctxSize,
	}, nil
}

// ModelName returns the configured model identifier.
func (p *AnthropicProvider) ModelName() string { return p.model }

// ContextSize returns the configured context window in tokens.
func (p *AnthropicProvider) ContextSize() int { return p.ctxSize }

// StreamChat issues a streaming Messages request and returns a channel of
// core.ChatEvent. The channel is closed after the stream terminates (cleanly
// or on error). If the request itself fails (HTTP / network), the returned
// error is non-nil and the channel is nil.
//
// Event mapping (Anthropic SSE → core.ChatEvent):
//
//	content_block_delta.text_delta  → Content
//	content_block_delta.thinking_delta → Thinking
//	content_block_start.tool_use    → ToolChunk (id + name)
//	content_block_delta.input_json_delta → ToolChunk (args fragment)
//	message_delta                   → Done (finish reason + usage)
//
// Tool-call deltas are tagged with the Anthropic content-block Index, so the
// loop's accumulator (in loop.go) can stitch them by index — same shape as
// OpenAI's ToolCallDelta.Index.
func (p *AnthropicProvider) StreamChat(
	ctx context.Context,
	messages []core.Message,
	tools []core.OpenAITool,
	opts core.GenerateOptions,
) (<-chan core.ChatEvent, error) {
	params := p.buildParams(messages, tools, opts)

	stream := p.client.Messages.NewStreaming(ctx, params)
	if stream.Err() != nil {
		return nil, fmt.Errorf("anthropic: init stream: %w", stream.Err())
	}

	out := make(chan core.ChatEvent, 32)
	go func() {
		defer close(out)
		p.drainStream(stream, out)
	}()
	return out, nil
}

// buildParams translates core types into the SDK's request shape.
func (p *AnthropicProvider) buildParams(
	messages []core.Message,
	tools []core.OpenAITool,
	opts core.GenerateOptions,
) anthropic.MessageNewParams {
	maxTok := int64(p.maxTokens)
	if opts.MaxTokens > 0 {
		maxTok = int64(opts.MaxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: maxTok,
	}

	// System prompt: pull any leading system messages into the top-level
	// System field. Multiple system messages are concatenated — Anthropic
	// accepts an array of TextBlockParam, so we keep them as separate
	// entries rather than joining with newlines (preserves structure).
	for _, m := range messages {
		if m.Role != string(core.RoleSystem) {
			break
		}
		params.System = append(params.System, anthropic.TextBlockParam{
			Text: m.Content,
		})
	}

	// Conversation messages: convert role + content + tool calls.
	for _, m := range messages {
		if m.Role == string(core.RoleSystem) {
			continue
		}
		params.Messages = append(params.Messages, toMessageParam(m))
	}

	if len(tools) > 0 {
		params.Tools = make([]anthropic.ToolUnionParam, 0, len(tools))
		for _, t := range tools {
			tp := toToolParam(t)
			params.Tools = append(params.Tools, anthropic.ToolUnionParam{
				OfTool: &tp,
			})
		}
	}

	// Thinking flag (Anthropic extended thinking). The budget must be ≥1024
	// and ≤ max_tokens; we cap at max_tokens/2 to leave room for the final
	// response. Only enabled when opts.Thinking is true AND max_tokens>1024.
	if opts.Thinking && maxTok > 1024 {
		budget := max(maxTok/2, 1024)
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
	}

	return params
}

// drainStream consumes the SSE stream end-to-end, emitting ChatEvents.
// It owns the lifetime of the stream — Close is always called.
func (p *AnthropicProvider) drainStream(stream *ssestream.Stream[anthropic.MessageStreamEventUnion], out chan<- core.ChatEvent) {
	defer stream.Close()

	for stream.Next() {
		evt := stream.Current()
		switch evt.Type {
		case "content_block_start":
			blk := evt.AsContentBlockStart()
			tu := blk.ContentBlock.AsToolUse()
			if tu.ID == "" {
				continue // not a tool_use block — ignore
			}
			out <- core.ChatEvent{
				Type: core.ChatEventToolChunk,
				Deltas: []core.ToolCallDelta{{
					Index: int(blk.Index),
					ID:    tu.ID,
					Type:  "function",
					Function: core.FunctionCallDelta{
						Name: tu.Name,
					},
				}},
			}
		case "content_block_delta":
			blk := evt.AsContentBlockDelta()
			switch d := blk.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				out <- core.ChatEvent{Type: core.ChatEventContent, Content: d.Text}
			case anthropic.ThinkingDelta:
				out <- core.ChatEvent{Type: core.ChatEventThinking, Content: d.Thinking}
			case anthropic.InputJSONDelta:
				out <- core.ChatEvent{
					Type: core.ChatEventToolChunk,
					Deltas: []core.ToolCallDelta{{
						Index: int(blk.Index),
						Function: core.FunctionCallDelta{
							Arguments: d.PartialJSON,
						},
					}},
				}
			}
		case "message_delta":
			blk := evt.AsMessageDelta()
			finish := mapStopReason(blk.Delta.StopReason)
			usage := &core.StreamUsage{
				PromptTokens:     int(blk.Usage.InputTokens),
				CompletionTokens: int(blk.Usage.OutputTokens),
			}
			out <- core.ChatEvent{
				Type:   core.ChatEventDone,
				Finish: finish,
				Usage:  usage,
			}
			return
		case "message_stop":
			// Belt-and-suspenders: some servers send message_stop without
			// a preceding message_delta. Emit a Done so the loop terminates.
			out <- core.ChatEvent{Type: core.ChatEventDone, Finish: core.FinishStop}
			return
		}
	}

	if err := stream.Err(); err != nil {
		out <- core.ChatEvent{Type: core.ChatEventError, Err: fmt.Errorf("anthropic stream: %w", err)}
	}
}

// mapStopReason translates Anthropic stop reasons to core.FinishReason.
// end_turn / max_tokens / stop_sequence / pause_turn / refusal all behave
// like OpenAI's "stop" (no further tool calls expected). tool_use → tool_calls.
func mapStopReason(r anthropic.StopReason) core.FinishReason {
	if r == anthropic.StopReasonToolUse {
		return core.FinishToolCalls
	}
	return core.FinishStop
}

// toMessageParam converts a core.Message into the SDK's MessageParam. It
// handles three shapes:
//   - assistant w/ ToolCalls → multiple content blocks (text + tool_use)
//   - tool result (RoleTool) → a user-role message with a tool_result block
//   - plain user/assistant text → single text block
func toMessageParam(m core.Message) anthropic.MessageParam {
	role := anthropic.MessageParamRoleUser
	if m.Role == string(core.RoleAssistant) {
		role = anthropic.MessageParamRoleAssistant
	}

	// Tool result: bare-role "tool" → repack as user-role tool_result block.
	if m.Role == string(core.RoleTool) || m.ToolCallID != "" {
		return anthropic.NewUserMessage(anthropic.NewToolResultBlock(
			m.ToolCallID, m.Content, false,
		))
	}

	var blocks []anthropic.ContentBlockParamUnion
	if m.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	for _, tc := range m.ToolCalls {
		var args any = json.RawMessage(tc.Function.Arguments)
		if tc.Function.Arguments == "" {
			args = map[string]any{}
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, args, tc.Function.Name))
	}
	if len(blocks) == 0 {
		// Anthropic rejects empty content — send a single empty text block
		// (matches SDK examples).
		blocks = []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock("")}
	}
	return anthropic.MessageParam{Role: role, Content: blocks}
}

// toToolParam converts an OpenAI-style tool definition to Anthropic's shape.
// Anthropic wants input_schema as a parsed JSON object (Properties/Required),
// not a raw JSON string — so we parse the schema lazily.
func toToolParam(t core.OpenAITool) anthropic.ToolParam {
	var schema map[string]any
	if len(t.Function.Parameters) > 0 {
		_ = json.Unmarshal(t.Function.Parameters, &schema)
	}
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	reqStrs := make([]string, 0, len(required))
	for _, r := range required {
		if s, ok := r.(string); ok {
			reqStrs = append(reqStrs, s)
		}
	}
	tp := anthropic.ToolParam{
		Name: t.Function.Name,
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: props,
			Required:   reqStrs,
		},
	}
	if t.Function.Description != "" {
		tp.Description = param.NewOpt(t.Function.Description)
	}
	return tp
}
