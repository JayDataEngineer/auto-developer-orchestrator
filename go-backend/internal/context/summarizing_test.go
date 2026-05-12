package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// --- mock implementations ---

type mockSession struct {
	mu               sync.Mutex
	id               string
	messages         []core.Message
	truncateResults  func(keep int) (int, error)
	compactFn        func(ctx context.Context, summary string) (string, error)
	buildContextFn   func(ctx context.Context) ([]core.Message, error)
}

func (s *mockSession) ID() string                                          { return s.id }
func (s *mockSession) AppendMessage(msg core.Message) error                  { s.mu.Lock(); defer s.mu.Unlock(); s.messages = append(s.messages, msg); return nil }
func (s *mockSession) BuildContext(ctx context.Context) ([]core.Message, error) {
	if s.buildContextFn != nil {
		return s.buildContextFn(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.Message, len(s.messages))
	copy(out, s.messages)
	return out, nil
}
func (s *mockSession) Navigate(nodeID string) error                         { return nil }
func (s *mockSession) Branch(label string) (string, error)                  { return "", nil }
func (s *mockSession) Fork(nodeID string) (core.Session, error)             { return nil, nil }
func (s *mockSession) Compact(ctx context.Context, summary string) (string, error) {
	if s.compactFn != nil {
		return s.compactFn(ctx, summary)
	}
	return "comp-1", nil
}
func (s *mockSession) TruncateToolResults(keep int) (int, error) {
	if s.truncateResults != nil {
		return s.truncateResults(keep)
	}
	return 0, nil
}
func (s *mockSession) GetTree() *core.TreeNode                              { return nil }
func (s *mockSession) GetCurrentNode() string                                { return "root" }
func (s *mockSession) Close() error                                          { return nil }

type mockProvider struct {
	mu         sync.Mutex
	streamFn   func(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error)
	modelName  string
	contextSz  int
	calls      int
}

func (p *mockProvider) StreamChat(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.streamFn != nil {
		return p.streamFn(ctx, messages, tools, opts)
	}
	ch := make(chan core.ChatEvent, 2)
	ch <- core.ChatEvent{Type: core.ChatEventContent, Content: "mock summary of the conversation that is long enough to exceed the fifty character minimum threshold required for valid summaries"}
	ch <- core.ChatEvent{Type: core.ChatEventDone}
	close(ch)
	return ch, nil
}
func (p *mockProvider) ModelName() string   { return p.modelName }
func (p *mockProvider) ContextSize() int     { return p.contextSz }

type mockInner struct {
	buildContextFn func(ctx context.Context) ([]core.Message, error)
	appendMsgFn    func(msg core.Message) error
	closeCalled    bool
	usage          ContextMetrics
	sessionID      string
}

func (m *mockInner) BuildContext(ctx context.Context) ([]core.Message, error) {
	if m.buildContextFn != nil {
		return m.buildContextFn(ctx)
	}
	return []core.Message{}, nil
}
func (m *mockInner) AppendMessage(msg core.Message) error {
	if m.appendMsgFn != nil {
		return m.appendMsgFn(msg)
	}
	return nil
}
func (m *mockInner) ProcessToolResult(ctx context.Context, toolName, toolCallID, result string) (string, error) { return result, nil }
func (m *mockInner) LoadSpilledContent(ref string) (string, error) { return "", nil }
func (m *mockInner) Usage() ContextMetrics { return m.usage }
func (m *mockInner) SessionID() string    { return m.sessionID }
func (m *mockInner) Close() error          { m.closeCalled = true; return nil }

// --- tests ---

func TestTruncateForSummary_Noop(t *testing.T) {
	got := truncateForSummary("hello", 100)
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestTruncateForSummary_Truncates(t *testing.T) {
	got := truncateForSummary("hello world", 5)
	if !strings.HasPrefix(got, "hello") || !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated with ellipsis, got %q", got)
	}
}

func TestTruncateForSummary_Exact(t *testing.T) {
	got := truncateForSummary("hello", 5)
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestNewSummarizingContextManager(t *testing.T) {
	inner := &mockInner{}
	sess := &mockSession{id: "test-sess"}
	provider := &mockProvider{}
	mgr := NewSummarizingContextManager(inner, Config{}, sess, provider)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if mgr.provider != provider {
		t.Fatal("provider not set")
	}
	if mgr.session != sess {
		t.Fatal("session not set")
	}
}

func TestNewSummarizingContextManager_NilProvider(t *testing.T) {
	inner := &mockInner{}
	sess := &mockSession{id: "test-sess"}
	mgr := NewSummarizingContextManager(inner, Config{}, sess, nil)
	if mgr.provider != nil {
		t.Fatal("expected nil provider")
	}
}

func TestBuildContext_NoTriggerBelowThreshold(t *testing.T) {
	inner := &mockInner{
		buildContextFn: func(ctx context.Context) ([]core.Message, error) {
			return []core.Message{}, nil
		},
	}
	sess := &mockSession{id: "test"}
	provider := &mockProvider{}
	cfg := Config{
		ContextSize:           10000,
		FullCompactThreshold:  0.75,
		MicroCompactThreshold: 0.55,
	}

	mgr := NewSummarizingContextManager(inner, cfg, sess, provider)
	msgs, err := mgr.BuildContext(context.Background())
	if err != nil {
		t.Fatalf("BuildContext error: %v", err)
	}
	if msgs == nil {
		t.Fatal("expected non-nil messages")
	}
}

func TestBuildContext_TriggersFullCompact(t *testing.T) {
	var compactCalled atomic.Bool
	sess := &mockSession{
		id: "test",
		compactFn: func(ctx context.Context, summary string) (string, error) {
			compactCalled.Store(true)
			if summary == "" {
				t.Error("expected non-empty summary")
			}
			return "comp-1", nil
		},
	}
	provider := &mockProvider{}
	inner := &mockInner{
		buildContextFn: func(ctx context.Context) ([]core.Message, error) {
			return []core.Message{}, nil
		},
	}
	cfg := Config{
		ContextSize:           100,
		FullCompactThreshold:  0.5,
		MicroCompactThreshold: 0.3,
	}

	mgr := NewSummarizingContextManager(inner, cfg, sess, provider)

	// Simulate session returning messages that exceed 50% of 100 tokens
	sess.mu.Lock()
	sess.messages = make([]core.Message, 20)
	for i := range sess.messages {
		sess.messages[i] = core.Message{Role: "user", Content: "this is a message that has enough characters to exceed the token threshold for triggering compaction "}
	}
	sess.mu.Unlock()

	_, err := mgr.BuildContext(context.Background())
	if err != nil {
		t.Fatalf("BuildContext error: %v", err)
	}

	if !compactCalled.Load() {
		t.Fatal("expected full compact to be called")
	}
}

func TestBuildContext_TriggersMicroCompact(t *testing.T) {
	var microCalled atomic.Bool
	sess := &mockSession{
		id: "test",
		truncateResults: func(keep int) (int, error) {
			microCalled.Store(true)
			return 1, nil
		},
		compactFn: func(ctx context.Context, summary string) (string, error) {
			t.Fatal("full compact should NOT be called when only micro threshold is exceeded")
			return "", nil
		},
	}
	inner := &mockInner{
		buildContextFn: func(ctx context.Context) ([]core.Message, error) {
			return []core.Message{}, nil
		},
	}
	cfg := Config{
		ContextSize:           200,
		FullCompactThreshold:  0.9,
		MicroCompactThreshold: 0.5,
		KeepResults:           4,
	}

	// ~60 tokens worth of messages → 30% < 50% micro, so first call won't trigger
	// We need to push ratio above 50% but below 90%
	mgr := NewSummarizingContextManager(inner, cfg, sess, nil)
	sess.mu.Lock()
	sess.messages = make([]core.Message, 10)
	for i := range sess.messages {
		sess.messages[i] = core.Message{Role: "assistant", Content: strings.Repeat("x", 40)}
	}
	sess.mu.Unlock()

	_, err := mgr.BuildContext(context.Background())
	if err != nil {
		t.Fatalf("BuildContext error: %v", err)
	}

	if !microCalled.Load() {
		t.Fatal("expected micro compact to be called")
	}
}

func TestBuildContext_FullCompactFallbackToMicro(t *testing.T) {
	// When LLM summary fails (nil provider), full compact should fall back to micro
	var microCalled atomic.Bool
	sess := &mockSession{
		id: "test",
		truncateResults: func(keep int) (int, error) {
			microCalled.Store(true)
			return 1, nil
		},
	}
	inner := &mockInner{
		buildContextFn: func(ctx context.Context) ([]core.Message, error) {
			return []core.Message{}, nil
		},
	}
	cfg := Config{
		ContextSize:           100,
		FullCompactThreshold:  0.5,
		MicroCompactThreshold: 0.3,
	}

	mgr := NewSummarizingContextManager(inner, cfg, sess, nil) // nil provider → summary fails
	sess.mu.Lock()
	sess.messages = make([]core.Message, 20)
	for i := range sess.messages {
		sess.messages[i] = core.Message{Role: "user", Content: "message content to exceed threshold "}
	}
	sess.mu.Unlock()

	_, err := mgr.BuildContext(context.Background())
	if err != nil {
		t.Fatalf("BuildContext error: %v", err)
	}

	if !microCalled.Load() {
		t.Fatal("expected micro compact fallback when LLM summary fails")
	}
}

func TestFullCompact_NilSession(t *testing.T) {
	mgr := NewSummarizingContextManager(&mockInner{}, Config{}, nil, nil)
	mgr.fullCompact(context.Background(), []core.Message{{Role: "user", Content: "hi"}})
	// should not panic; test passes if we get here
}

func TestFullCompact_FewMessages(t *testing.T) {
	sess := &mockSession{id: "test"}
	mgr := NewSummarizingContextManager(&mockInner{}, Config{}, sess, nil)
	// fewer than 6 messages should be a no-op
	mgr.fullCompact(context.Background(), []core.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
	})
	// should not panic
}

func TestFullCompact_ProviderError(t *testing.T) {
	sess := &mockSession{id: "test"}
	provider := &mockProvider{
		streamFn: func(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
			return nil, assertAnError("provider unavailable")
		},
	}
	inner := &mockInner{}
	mgr := NewSummarizingContextManager(inner, Config{}, sess, provider)

	msgs := make([]core.Message, 10)
	for i := range msgs {
		msgs[i] = core.Message{Role: "user", Content: "test"}
	}

	mgr.fullCompact(context.Background(), msgs)
	// should not panic; fallback handled internally
}

func TestFullCompact_StreamError(t *testing.T) {
	sess := &mockSession{id: "test"}
	provider := &mockProvider{
		streamFn: func(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
			ch := make(chan core.ChatEvent, 2)
			ch <- core.ChatEvent{Type: core.ChatEventContent, Content: "partial"}
			ch <- core.ChatEvent{Type: core.ChatEventError, Err: assertAnError("stream failed")}
			close(ch)
			return ch, nil
		},
	}
	inner := &mockInner{}
	mgr := NewSummarizingContextManager(inner, Config{}, sess, provider)

	msgs := make([]core.Message, 10)
	for i := range msgs {
		msgs[i] = core.Message{Role: "user", Content: "test"}
	}

	mgr.fullCompact(context.Background(), msgs)
	// should handle stream error gracefully
}

func TestFullCompact_SummaryTooShort(t *testing.T) {
	var compactCalled atomic.Bool
	sess := &mockSession{
		id: "test",
		compactFn: func(ctx context.Context, summary string) (string, error) {
			compactCalled.Store(true)
			return "comp-1", nil
		},
	}
	provider := &mockProvider{
		streamFn: func(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
			ch := make(chan core.ChatEvent, 2)
			ch <- core.ChatEvent{Type: core.ChatEventContent, Content: "short"}
			ch <- core.ChatEvent{Type: core.ChatEventDone}
			close(ch)
			return ch, nil
		},
	}
	inner := &mockInner{}
	mgr := NewSummarizingContextManager(inner, Config{}, sess, provider)

	msgs := make([]core.Message, 10)
	for i := range msgs {
		msgs[i] = core.Message{Role: "user", Content: "test"}
	}

	mgr.fullCompact(context.Background(), msgs)
	if compactCalled.Load() {
		t.Fatal("expected compact NOT to be called with too-short summary")
	}
}

func TestFullCompact_WithSummary(t *testing.T) {
	provider := &mockProvider{
		streamFn: func(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
			ch := make(chan core.ChatEvent, 2)
			ch <- core.ChatEvent{Type: core.ChatEventContent, Content: "this is a comprehensive summary of the conversation that covers all key points and decisions made during the session"}
			ch <- core.ChatEvent{Type: core.ChatEventDone}
			close(ch)
			return ch, nil
		},
	}
	var compactCalled atomic.Bool
	var compactSummary string
	sess := &mockSession{
		id: "test",
		compactFn: func(ctx context.Context, summary string) (string, error) {
			compactCalled.Store(true)
			compactSummary = summary
			return "comp-1", nil
		},
	}
	inner := &mockInner{}
	mgr := NewSummarizingContextManager(inner, Config{}, sess, provider)

	msgs := make([]core.Message, 10)
	for i := range msgs {
		msgs[i] = core.Message{Role: "user", Content: "test message number " + string(rune('0'+i))}
	}

	mgr.fullCompact(context.Background(), msgs)
	if !compactCalled.Load() {
		t.Fatal("expected compact to be called")
	}
	if !strings.Contains(compactSummary, "comprehensive summary") {
		t.Fatalf("unexpected summary: %q", compactSummary)
	}
	if mgr.metrics.CompactionType != "full" {
		t.Fatalf("expected compaction type 'full', got %q", mgr.metrics.CompactionType)
	}
}

func TestGenerateSummary_AllMessageTypes(t *testing.T) {
	provider := &mockProvider{}
	sess := &mockSession{id: "test"}
	mgr := NewSummarizingContextManager(&mockInner{}, Config{}, sess, provider)

	msgs := []core.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello, can you help me?"},
		{Role: "assistant", Content: "Sure! What do you need?", ToolCalls: []core.ToolCallResponse{
			{Function: core.FunctionCallData{Name: "bash", Arguments: `{"cmd":"ls"}`}},
		}},
		{Role: "tool", Content: "file1.txt\nfile2.txt", Name: "bash", ToolCallID: "call-1"},
		{Role: "assistant", Content: "Here are the files."},
		{Role: "user", Content: "Great, thanks!"},
	}

	summary := mgr.generateSummary(context.Background(), msgs)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if !strings.Contains(summary, "mock summary") {
		t.Fatalf("unexpected summary: %q", summary)
	}

	provider.mu.Lock()
	calls := provider.calls
	provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 provider call, got %d", calls)
	}
}

func TestGenerateSummary_NilProvider(t *testing.T) {
	mgr := NewSummarizingContextManager(&mockInner{}, Config{}, &mockSession{}, nil)
	summary := mgr.generateSummary(context.Background(), []core.Message{{Role: "user", Content: "hi"}})
	if summary != "" {
		t.Fatalf("expected empty summary with nil provider, got %q", summary)
	}
}

func TestMicroCompact(t *testing.T) {
	var truncated atomic.Int32
	sess := &mockSession{
		id: "test",
		truncateResults: func(keep int) (int, error) {
			truncated.Store(int32(keep))
			return 3, nil
		},
	}
	mgr := NewSummarizingContextManager(&mockInner{}, Config{KeepResults: 2}, sess, nil)
	mgr.microCompact()
	if truncated.Load() != 2 {
		t.Fatalf("expected keep=2, got %d", truncated.Load())
	}
	if mgr.metrics.CompactionType != "micro" {
		t.Fatalf("expected compaction type 'micro', got %q", mgr.metrics.CompactionType)
	}
}

func TestMicroCompact_DefaultKeep(t *testing.T) {
	var truncated atomic.Int32
	sess := &mockSession{
		id: "test",
		truncateResults: func(keep int) (int, error) {
			truncated.Store(int32(keep))
			return 1, nil
		},
	}
	mgr := NewSummarizingContextManager(&mockInner{}, Config{KeepResults: 0}, sess, nil)
	mgr.microCompact()
	if truncated.Load() != 4 {
		t.Fatalf("expected default keep=4, got %d", truncated.Load())
	}
}

func TestUsage_Metrics(t *testing.T) {
	inner := &mockInner{
		usage: ContextMetrics{
			EstimatedTokens: 100,
			ContextSize:     1000,
			Utilization:     0.1,
		},
	}
	mgr := NewSummarizingContextManager(inner, Config{}, &mockSession{}, nil)
	mgr.metrics.CompactionType = "micro"
	mgr.metrics.LastCompaction = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	u := mgr.Usage()
	if u.CompactionType != "micro" {
		t.Fatalf("expected compaction type 'micro', got %q", u.CompactionType)
	}
	if u.EstimatedTokens != 100 {
		t.Fatalf("expected 100 tokens, got %d", u.EstimatedTokens)
	}
}

func TestArchiveConversation_DirCreation(t *testing.T) {
	dir := t.TempDir()
	spillDir := filepath.Join(dir, "spill", "sess-1")
	mgr := NewSummarizingContextManager(&mockInner{sessionID: "sess-1"}, Config{SpillDir: spillDir}, &mockSession{id: "sess-1"}, nil)

	msgs := []core.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	path := mgr.archiveConversation(msgs)
	if path == "" {
		t.Fatal("expected non-empty archive path")
	}

	// Verify the archive directory was created
	archiveDir := filepath.Join(dir, "spill", "archives")
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		t.Fatal("expected archive dir to exist")
	}

	// Verify the file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected archive file to exist")
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read archive: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Hello") {
		t.Fatal("expected archive to contain message content")
	}
	if !strings.Contains(content, "sess-1") {
		t.Fatal("expected archive to contain session ID")
	}
}

func TestArchiveConversation_EmptySpillDir(t *testing.T) {
	mgr := NewSummarizingContextManager(&mockInner{}, Config{}, &mockSession{}, nil)
	path := mgr.archiveConversation([]core.Message{{Role: "user", Content: "hi"}})
	if path != "" {
		t.Fatal("expected empty path when SpillDir is empty")
	}
}

func TestArchiveConversation_TruncatesLongContent(t *testing.T) {
	dir := t.TempDir()
	spillDir := filepath.Join(dir, "spill", "sess-1")
	mgr := NewSummarizingContextManager(&mockInner{}, Config{SpillDir: spillDir}, &mockSession{}, nil)

	longContent := strings.Repeat("x", 15000)
	msgs := []core.Message{
		{Role: "tool", Content: longContent, Name: "bash", ToolCallID: "call-1"},
	}

	path := mgr.archiveConversation(msgs)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read archive: %v", err)
	}
	if !strings.Contains(string(data), "[truncated in archive") {
		t.Fatal("expected truncation indicator in archive for long content")
	}
}

func TestArchiveConversation_AllRoles(t *testing.T) {
	dir := t.TempDir()
	spillDir := filepath.Join(dir, "spill", "sess-1")
	mgr := NewSummarizingContextManager(&mockInner{}, Config{SpillDir: spillDir}, &mockSession{id: "sess-1"}, nil)

	msgs := []core.Message{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "User message"},
		{Role: "assistant", Content: "Assistant response", ToolCalls: []core.ToolCallResponse{
			{Function: core.FunctionCallData{Name: "test_tool", Arguments: `{"arg":1}`}},
		}},
		{Role: "tool", Content: "Tool result", Name: "test_tool", ToolCallID: "call-1"},
	}

	path := mgr.archiveConversation(msgs)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read archive: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[System]") {
		t.Fatal("expected System section")
	}
	if !strings.Contains(content, "[User]") {
		t.Fatal("expected User section")
	}
	if !strings.Contains(content, "[Assistant]") {
		t.Fatal("expected Assistant section")
	}
	if !strings.Contains(content, "test_tool") {
		t.Fatal("expected tool call info")
	}
	if !strings.Contains(content, "[Tool Result: test_tool]") {
		t.Fatal("expected Tool Result section")
	}
}

func TestCleanupOldArchives(t *testing.T) {
	dir := t.TempDir()

	// Create 15 archive files (timestamps sort lexicographically)
	for i := 0; i < 15; i++ {
		name := fmt.Sprintf("conversation-20250101-%04d.md", i)
		os.WriteFile(filepath.Join(dir, name), []byte("content"), 0644)
	}

	mgr := NewSummarizingContextManager(&mockInner{}, Config{}, &mockSession{}, nil)
	mgr.cleanupOldArchives(dir, 10)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 10 {
		t.Fatalf("expected 10 remaining archives, got %d", len(entries))
	}

	// The last 10 (newest) should remain
	expectedNames := []string{
		"conversation-20250101-0005.md",
		"conversation-20250101-0006.md",
		"conversation-20250101-0007.md",
		"conversation-20250101-0008.md",
		"conversation-20250101-0009.md",
		"conversation-20250101-0010.md",
		"conversation-20250101-0011.md",
		"conversation-20250101-0012.md",
		"conversation-20250101-0013.md",
		"conversation-20250101-0014.md",
	}
	for i, e := range entries {
		if e.Name() != expectedNames[i] {
			t.Fatalf("entry[%d] = %q, want %q", i, e.Name(), expectedNames[i])
		}
	}
}

func TestCleanupOldArchives_NoopWhenBelowLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("archive-%d.md", i)
		os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644)
	}

	mgr := NewSummarizingContextManager(&mockInner{}, Config{}, &mockSession{}, nil)
	mgr.cleanupOldArchives(dir, 10)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (no cleanup), got %d", len(entries))
	}
}

func TestSessionID_Delegation(t *testing.T) {
	inner := &mockInner{sessionID: "inner-session"}
	mgr := NewSummarizingContextManager(inner, Config{}, &mockSession{id: "raw-session"}, nil)
	id := mgr.SessionID()
	if id != "inner-session" {
		t.Fatalf("expected 'inner-session', got %q", id)
	}
}

func TestClose_Delegation(t *testing.T) {
	inner := &mockInner{}
	mgr := NewSummarizingContextManager(inner, Config{}, &mockSession{}, nil)
	mgr.Close()
	if !inner.closeCalled {
		t.Fatal("expected inner.Close to be called")
	}
}

func TestSessionID_DirectCall(t *testing.T) {
	inner := &mockInner{}
	mgr := NewSummarizingContextManager(inner, Config{}, &mockSession{}, nil)
	mgr.SessionID()
}

// assertAnError returns a simple error for testing.
func assertAnError(msg string) error {
	return &testError{msg: msg}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestEmitCompactionEvent_WithSubscriber(t *testing.T) {
	ch := make(chan core.AgentEvent, 2)
	var subCh chan<- core.AgentEvent = ch
	sess := &mockSession{
		id: "test",
		truncateResults: func(keep int) (int, error) {
			return 1, nil
		},
	}
	cfg := Config{
		ContextSize:           200,
		MicroCompactThreshold: 0.5,
		FullCompactThreshold:  0.9,
		KeepResults:           2,
	}
	mgr := NewSummarizingContextManager(&mockInner{
		buildContextFn: func(ctx context.Context) ([]core.Message, error) {
			return []core.Message{}, nil
		},
	}, cfg, sess, nil)

	// Simulate subscriber injection via context (as BuildContext does)
	ctx := context.WithValue(context.Background(), core.SubscriberKey{}, subCh)
	sess.mu.Lock()
	sess.messages = make([]core.Message, 10)
	for i := range sess.messages {
		sess.messages[i] = core.Message{Role: "assistant", Content: strings.Repeat("x", 40)}
	}
	sess.mu.Unlock()

	// BuildContext caches the subscriber channel and triggers compaction
	_, err := mgr.BuildContext(ctx)
	if err != nil {
		t.Fatalf("BuildContext error: %v", err)
	}

	// The event should be on the channel from BuildContext's compaction
	select {
	case evt := <-ch:
		if evt.Type != core.EventTypeCompactionEnd {
			t.Fatalf("expected compaction_end event, got %q", evt.Type)
		}
		if evt.Data.CompactionType != "micro" {
			t.Fatalf("expected compaction type 'micro', got %q", evt.Data.CompactionType)
		}
		if evt.Data.CompactedMessages != 1 {
			t.Fatalf("expected 1 compacted message, got %d", evt.Data.CompactedMessages)
		}
		if evt.Data.ContextTokens == 0 {
			t.Fatal("expected non-zero context tokens")
		}
		if evt.Data.ContextSize == 0 {
			t.Fatal("expected non-zero context size")
		}
	default:
		t.Fatal("expected compaction event on subscriber channel")
	}
}

func TestEmitCompactionEvent_NoSubscriber(t *testing.T) {
	sess := &mockSession{
		id: "test",
		truncateResults: func(keep int) (int, error) {
			return 1, nil
		},
	}
	mgr := NewSummarizingContextManager(&mockInner{}, Config{KeepResults: 2}, sess, nil)
	// No subscriber — should not panic
	mgr.microCompact()
}
