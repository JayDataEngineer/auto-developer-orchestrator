package context

import (
	"context"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestSummarizeText_NilProvider(t *testing.T) {
	result, err := SummarizeText(context.Background(), nil, "hello world", 5)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello world" {
		t.Fatalf("expected original text, got %q", result)
	}
}

func TestSummarizeText_ShortText(t *testing.T) {
	provider := &mockProviderForSummary{response: "summarized"}
	result, err := SummarizeText(context.Background(), provider, "short", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result != "short" {
		t.Fatalf("expected original text (already short), got %q", result)
	}
}

func TestSummarizeText_LongText(t *testing.T) {
	longText := ""
	for i := 0; i < 500; i++ {
		longText += "This is a line of text with various details. "
	}

	provider := &mockProviderForSummary{response: "Key finding: the analysis is complete. Result: all tests pass."}
	result, err := SummarizeText(context.Background(), provider, longText, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Key finding: the analysis is complete. Result: all tests pass." {
		t.Fatalf("expected summary, got %q", result)
	}
}

func TestSummarizeText_EmptyResponse(t *testing.T) {
	longText := ""
	for i := 0; i < 500; i++ {
		longText += "Lots of content here. "
	}

	provider := &mockProviderForSummary{response: ""}
	result, err := SummarizeText(context.Background(), provider, longText, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Empty/short response → returns empty string, no error
	if result != "" {
		t.Fatalf("expected empty result for short LLM response, got %q", result)
	}
}

func TestContextStatusTool_Execute(t *testing.T) {
	mgr := &mockContextManagerWithMetrics{
		metrics: ContextMetrics{
			EstimatedTokens: 10000,
			ContextSize:     32768,
			Utilization:     0.31,
			MessageCount:    25,
			CompactionType:  "micro",
			SpilledCount:    3,
		},
	}

	tool := NewContextStatusTool(mgr)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	m := result.(map[string]any)
	if m["estimated_tokens"] != 10000 {
		t.Fatalf("expected 10000 tokens, got %v", m["estimated_tokens"])
	}
	if m["context_size"] != 32768 {
		t.Fatalf("expected 32768 context size, got %v", m["context_size"])
	}
	if m["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", m["status"])
	}
	if m["message_count"] != 25 {
		t.Fatalf("expected 25 messages, got %v", m["message_count"])
	}
}

func TestContextStatusTool_HighUtilization(t *testing.T) {
	mgr := &mockContextManagerWithMetrics{
		metrics: ContextMetrics{
			EstimatedTokens: 20000,
			ContextSize:     32768,
			Utilization:     0.61,
		},
	}

	tool := NewContextStatusTool(mgr)
	result, _ := tool.Execute(context.Background(), nil)
	m := result.(map[string]any)
	if m["status"] != "high" {
		t.Fatalf("expected status high, got %v", m["status"])
	}
}

func TestContextStatusTool_CriticalUtilization(t *testing.T) {
	mgr := &mockContextManagerWithMetrics{
		metrics: ContextMetrics{
			EstimatedTokens: 26000,
			ContextSize:     32768,
			Utilization:     0.79,
		},
	}

	tool := NewContextStatusTool(mgr)
	result, _ := tool.Execute(context.Background(), nil)
	m := result.(map[string]any)
	if m["status"] != "critical" {
		t.Fatalf("expected status critical, got %v", m["status"])
	}
}

func TestSummarizeTool_Schema(t *testing.T) {
	tool := NewSummarizeTool(nil, nil, nil)
	if tool.Name() != "summarize_context" {
		t.Fatalf("expected summarize_context, got %q", tool.Name())
	}
	if len(tool.Schema()) == 0 {
		t.Fatal("expected non-empty schema")
	}
}

func TestContextStatusTool_Schema(t *testing.T) {
	tool := NewContextStatusTool(nil)
	if tool.Name() != "context_status" {
		t.Fatalf("expected context_status, got %q", tool.Name())
	}
	if len(tool.Schema()) == 0 {
		t.Fatal("expected non-empty schema")
	}
}

// ── Mock helpers ──

type mockProviderForSummary struct {
	response string
}

func (m *mockProviderForSummary) StreamChat(_ context.Context, _ []core.Message, _ []core.OpenAITool, _ core.GenerateOptions) (<-chan core.ChatEvent, error) {
	ch := make(chan core.ChatEvent, 2)
	ch <- core.ChatEvent{Type: core.ChatEventContent, Content: m.response}
	ch <- core.ChatEvent{Type: core.ChatEventDone}
	close(ch)
	return ch, nil
}

func (m *mockProviderForSummary) ModelName() string  { return "test" }
func (m *mockProviderForSummary) ContextSize() int   { return 32768 }
func (m *mockProviderForSummary) Generate(_ context.Context, _ []core.Message, _ []core.OpenAITool, _ core.GenerateOptions) (core.GenerateResponse, error) {
	return core.GenerateResponse{}, nil
}

type mockContextManagerWithMetrics struct {
	metrics ContextMetrics
}

func (m *mockContextManagerWithMetrics) BuildContext(_ context.Context) ([]core.Message, error) {
	return nil, nil
}
func (m *mockContextManagerWithMetrics) AppendMessage(_ core.Message) error { return nil }
func (m *mockContextManagerWithMetrics) ProcessToolResult(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockContextManagerWithMetrics) LoadSpilledContent(_ string) (string, error) {
	return "", nil
}
func (m *mockContextManagerWithMetrics) Usage() ContextMetrics { return m.metrics }
func (m *mockContextManagerWithMetrics) SessionID() string     { return "test" }
func (m *mockContextManagerWithMetrics) Close() error          { return nil }
