package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultMaxPerParent = 3
	defaultMaxTotal     = 10
)

// SubAgentManager manages the lifecycle of sub-agent Pi subprocesses.
// Sub-agents are specialized Pi instances spawned on behalf of a parent agent.
type SubAgentManager struct {
	mu             sync.Mutex
	agents         map[string]*SubAgentInstance // subAgentID → instance
	children       map[string][]string          // parentID → []subAgentID
	pool           *PiPool                      // reference for limit checks
	logger         *zap.Logger
	sandboxMgr     interface{} // *sandbox.Manager, nil if unavailable
	browserBaseURL string
	maxPerParent   int
	maxTotal       int
}

// SubAgentManagerOption configures a SubAgentManager.
type SubAgentManagerOption func(*SubAgentManager)

// WithSandboxManager sets the sandbox manager for sub-agents.
func WithSandboxManager(mgr interface{}) SubAgentManagerOption {
	return func(m *SubAgentManager) { m.sandboxMgr = mgr }
}

// WithBrowserBaseURL sets the browser API base URL for web sub-agents.
func WithBrowserBaseURL(url string) SubAgentManagerOption {
	return func(m *SubAgentManager) { m.browserBaseURL = url }
}

// WithMaxPerParent sets the maximum number of concurrent sub-agents per parent.
func WithMaxPerParent(n int) SubAgentManagerOption {
	return func(m *SubAgentManager) { m.maxPerParent = n }
}

// WithMaxTotal sets the maximum total number of concurrent sub-agents.
func WithMaxTotal(n int) SubAgentManagerOption {
	return func(m *SubAgentManager) { m.maxTotal = n }
}

// NewSubAgentManager creates a new SubAgentManager.
func NewSubAgentManager(pool *PiPool, logger *zap.Logger, opts ...SubAgentManagerOption) *SubAgentManager {
	m := &SubAgentManager{
		agents:       make(map[string]*SubAgentInstance),
		children:     make(map[string][]string),
		pool:         pool,
		logger:       logger,
		maxPerParent: defaultMaxPerParent,
		maxTotal:     defaultMaxTotal,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Spawn creates and starts a new sub-agent.
// Returns the sub-agent ID immediately; the agent runs asynchronously.
func (m *SubAgentManager) Spawn(ctx context.Context, cfg SubAgentConfig) (string, error) {
	cfg.InitDefaults()
	if err := cfg.Validate(); err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check limits
	if len(m.agents) >= m.maxTotal {
		return "", fmt.Errorf("max total sub-agents (%d) reached", m.maxTotal)
	}
	children := m.children[cfg.ParentID]
	active := 0
	for _, id := range children {
		if inst, ok := m.agents[id]; ok && !inst.Status.IsTerminal() {
			active++
		}
	}
	if active >= m.maxPerParent {
		return "", fmt.Errorf("max sub-agents per parent (%d) reached for %s", m.maxPerParent, cfg.ParentID)
	}

	// Build specialized prompt
	promptCfg := SubAgentPromptConfig{
		ProjectDir:     cfg.ProjectDir,
		Type:           cfg.Type,
		BrowserBaseURL: m.browserBaseURL,
	}
	systemPrompt := BuildSubAgentPrompt(promptCfg)

	// Create PiClient with custom prompt
	client, err := NewPiClientWithPrompt(cfg.ProjectDir, cfg.AgentID, systemPrompt, m.logger, m.sandboxMgr)
	if err != nil {
		return "", fmt.Errorf("failed to create pi client for sub-agent: %w", err)
	}

	inst := &SubAgentInstance{
		ID:        cfg.AgentID,
		Config:    cfg,
		Client:    client,
		Status:    StatusPending,
		Done:      make(chan struct{}),
		StartTime: time.Now(),
	}

	m.agents[cfg.AgentID] = inst
	m.children[cfg.ParentID] = append(m.children[cfg.ParentID], cfg.AgentID)

	m.logger.Info("Sub-agent spawned",
		zap.String("subAgentId", cfg.AgentID),
		zap.String("type", string(cfg.Type)),
		zap.String("parentId", cfg.ParentID),
	)

	// Subscribe to events and send prompt in background
	go m.runSubAgent(inst)

	return cfg.AgentID, nil
}

// runSubAgent sends the task prompt and collects events until completion.
func (m *SubAgentManager) runSubAgent(inst *SubAgentInstance) {
	subID := fmt.Sprintf("sub-collect-%s", inst.ID)
	events := inst.Client.Subscribe(subID)
	defer inst.Client.Unsubscribe(subID)

	// Set model if specified
	if inst.Config.Model != "" {
		if err := inst.Client.SetModel("litellm", inst.Config.Model); err != nil {
			m.logger.Warn("Failed to set sub-agent model (non-fatal)", zap.Error(err))
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Send the task prompt
	inst.mu.Lock()
	inst.Status = StatusRunning
	inst.mu.Unlock()

	if err := inst.Client.SendPrompt(inst.Config.Task, "", ""); err != nil {
		m.finishInstance(inst, StatusFailed, "", fmt.Sprintf("failed to send prompt: %v", err))
		return
	}

	// Collect events
	for {
		event, ok := <-events
		if !ok {
			m.finishInstance(inst, StatusFailed, inst.output.String(), "event stream closed unexpectedly")
			return
		}

		inst.mu.Lock()
		switch event.Type {
		case RpcEventMessageUpdate:
			if event.AssistantMessageEvent != nil && event.AssistantMessageEvent.Type == "text_delta" {
				inst.output.WriteString(event.AssistantMessageEvent.Delta)
			}
			inst.mu.Unlock()
		case RpcEventTurnEnd:
			// Some models (e.g., qwen-cloud) send full text in turn_end instead of message_update deltas.
			// Only overwrite if turn_end has actual text content; otherwise preserve accumulated deltas.
			text := extractTextFromMessage(event.Message)
			if text != "" && inst.output.Len() == 0 {
				inst.output.WriteString(text)
			}
			inst.mu.Unlock()
		case RpcEventMessageEnd:
			// Fallback: extract text from message_end if no deltas collected yet
			// Only extract from assistant messages (user messages also trigger message_end)
			if inst.output.Len() == 0 && isAssistantMessage(event.Message) {
				text := extractTextFromMessage(event.Message)
				if text != "" {
					inst.output.WriteString(text)
				}
			}
			inst.mu.Unlock()
		case RpcEventToolStart:
			inst.toolCount++
			inst.mu.Unlock()
		case RpcEventAgentEnd:
			output := inst.output.String()
			toolCount := inst.toolCount

			// Final fallback: extract text from agent_end messages if still empty
			if output == "" && len(event.Messages) > 0 {
				output = extractLastAssistantText(event.Messages)
			}

			inst.mu.Unlock()

			// Extract usage from agent_end messages
			var inputTokens, outputTokens, cacheTokens float64
			if len(event.Messages) > 0 {
				inputTokens, outputTokens, cacheTokens = extractUsageFromMessages(event.Messages)
			}

			result := &SubAgentResult{
				SubAgentID:   inst.ID,
				Type:         inst.Config.Type,
				Status:       StatusComplete,
				Output:       output,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				CacheTokens:  cacheTokens,
				DurationMs:   time.Since(inst.StartTime).Milliseconds(),
				ToolCalls:    toolCount,
			}
			m.finishWithResult(inst, result)
			return
		case RpcEventError:
			output := inst.output.String()
			errMsg := event.Data.Error
			if errMsg == "" {
				errMsg = "unknown error from pi subprocess"
			}
			inst.mu.Unlock()
			m.finishInstance(inst, StatusFailed, output, errMsg)
			return
		default:
			inst.mu.Unlock()
		}
	}
}

// isAssistantMessage checks if a raw message JSON has role "assistant".
func isAssistantMessage(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var msg struct {
		Role string `json:"role"`
	}
	json.Unmarshal(raw, &msg)
	return msg.Role == "assistant"
}

// extractTextFromMessage extracts text content from a single message JSON.
// Handles both array content format: {"content":[{"type":"text","text":"..."}]}
// and simple string format: {"content":"text here"}
func extractTextFromMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try array content format (Anthropic-style)
	var msg struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &msg) == nil && len(msg.Content) > 0 {
		var sb strings.Builder
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
		return sb.String()
	}

	// Try simple string content format
	var msgSimple struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if json.Unmarshal(raw, &msgSimple) == nil && msgSimple.Content != "" {
		return msgSimple.Content
	}

	return ""
}

// extractLastAssistantText extracts text from the last assistant message in an agent_end messages array.
func extractLastAssistantText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var msgs []struct {
		Role    string `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &msgs) != nil {
		return ""
	}

	// Find the last assistant message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return extractTextFromMessage(msgs[i].Content)
		}
	}
	return ""
}

// extractUsageFromMessages parses usage data from agent_end messages.
func extractUsageFromMessages(raw json.RawMessage) (input, output, cache float64) {
	var msgs []struct {
		Role string `json:"role"`
		Usage struct {
			Input     float64 `json:"input"`
			Output    float64 `json:"output"`
			CacheRead float64 `json:"cacheRead"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &msgs) != nil {
		return 0, 0, 0
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Usage.Input, msgs[i].Usage.Output, msgs[i].Usage.CacheRead
		}
	}
	return 0, 0, 0
}

// finishInstance sets the instance to a terminal state with a result.
func (m *SubAgentManager) finishInstance(inst *SubAgentInstance, status SubAgentStatus, output, errMsg string) {
	result := &SubAgentResult{
		SubAgentID: inst.ID,
		Type:       inst.Config.Type,
		Status:     status,
		Output:     output,
		Error:      errMsg,
		DurationMs: time.Since(inst.StartTime).Milliseconds(),
	}
	m.finishWithResult(inst, result)
}

// finishWithResult sets the final result and closes the Done channel.
func (m *SubAgentManager) finishWithResult(inst *SubAgentInstance, result *SubAgentResult) {
	inst.mu.Lock()
	inst.Status = result.Status
	inst.Result = result
	inst.mu.Unlock()

	// Close Done channel (non-blocking — only close once)
	select {
	case <-inst.Done:
		// Already closed
	default:
		close(inst.Done)
	}

	m.logger.Info("Sub-agent finished",
		zap.String("subAgentId", inst.ID),
		zap.String("status", string(result.Status)),
		zap.Int64("durationMs", result.DurationMs),
	)
}

// GetResult blocks until the sub-agent reaches a terminal state, then returns the result.
func (m *SubAgentManager) GetResult(ctx context.Context, subAgentID string) (*SubAgentResult, error) {
	inst, err := m.GetInstance(subAgentID)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-inst.Done:
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return inst.Result, nil
	}
}

// GetStatus returns the current non-blocking status of a sub-agent.
func (m *SubAgentManager) GetStatus(subAgentID string) (SubAgentStatus, error) {
	inst, err := m.GetInstance(subAgentID)
	if err != nil {
		return "", err
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.Status, nil
}

// GetInstance returns the SubAgentInstance for the given ID.
func (m *SubAgentManager) GetInstance(subAgentID string) (*SubAgentInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.agents[subAgentID]
	if !ok {
		return nil, fmt.Errorf("sub-agent %s not found", subAgentID)
	}
	return inst, nil
}

// Abort cancels a running sub-agent.
func (m *SubAgentManager) Abort(subAgentID string) error {
	inst, err := m.GetInstance(subAgentID)
	if err != nil {
		return err
	}

	inst.mu.Lock()
	if inst.Status.IsTerminal() {
		inst.mu.Unlock()
		return nil // Already terminal
	}
	inst.mu.Unlock()

	// Abort the pi client
	if err := inst.Client.Abort(); err != nil {
		m.logger.Warn("Failed to abort sub-agent client", zap.Error(err))
	}

	m.finishInstance(inst, StatusAborted, "", "aborted by user")
	return nil
}

// CleanupForParent destroys all sub-agents belonging to a parent agent.
func (m *SubAgentManager) CleanupForParent(parentID string) {
	m.mu.Lock()
	ids := m.children[parentID]
	delete(m.children, parentID)
	m.mu.Unlock()

	for _, id := range ids {
		m.cleanupInstance(id)
	}
}

// ListByParent returns results for all sub-agents of a parent.
func (m *SubAgentManager) ListByParent(parentID string) []SubAgentResult {
	m.mu.Lock()
	ids := make([]string, len(m.children[parentID]))
	copy(ids, m.children[parentID])
	m.mu.Unlock()

	var results []SubAgentResult
	for _, id := range ids {
		inst, err := m.GetInstance(id)
		if err != nil {
			continue
		}
		inst.mu.Lock()
		r := SubAgentResult{
			SubAgentID: inst.ID,
			Type:       inst.Config.Type,
			Status:     inst.Status,
			DurationMs: time.Since(inst.StartTime).Milliseconds(),
			ToolCalls:  inst.toolCount,
		}
		if inst.Result != nil {
			r = *inst.Result
		}
		inst.mu.Unlock()
		results = append(results, r)
	}
	return results
}

// Shutdown cleans up all sub-agents.
func (m *SubAgentManager) Shutdown() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	for _, id := range ids {
		m.cleanupInstance(id)
	}

	m.logger.Info("SubAgentManager shutdown complete")
}

// cleanupInstance removes a single sub-agent and closes its client.
func (m *SubAgentManager) cleanupInstance(subAgentID string) {
	m.mu.Lock()
	inst, ok := m.agents[subAgentID]
	if ok {
		delete(m.agents, subAgentID)
		// Remove from children map
		for parentID, children := range m.children {
			for i, id := range children {
				if id == subAgentID {
					m.children[parentID] = append(children[:i], children[i+1:]...)
					break
				}
			}
		}
	}
	m.mu.Unlock()

	if inst != nil {
		inst.Client.Close()
		// Ensure Done is closed
		select {
		case <-inst.Done:
		default:
			m.finishInstance(inst, StatusAborted, "", "shutdown")
		}
	}
}

// Output returns the accumulated output text for a running sub-agent.
// Used for SSE streaming before agent_end.
func (inst *SubAgentInstance) Output() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.output.String()
}

// ToolCount returns the number of tool calls made by the sub-agent.
func (inst *SubAgentInstance) ToolCount() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.toolCount
}

// GetResult returns the final result (nil if not yet complete).
func (inst *SubAgentInstance) GetResult() *SubAgentResult {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.Result
}

// CollectOutput appends a text delta to the instance output.
func (inst *SubAgentInstance) CollectOutput(delta string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.output.WriteString(delta)
}

// IncrementToolCount increments the tool call counter.
func (inst *SubAgentInstance) IncrementToolCount() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.toolCount++
}

// IsTerminalState returns whether the instance is in a terminal state.
func (inst *SubAgentInstance) IsTerminalState() bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.Status.IsTerminal()
}

// String implements fmt.Stringer for SubAgentType.
func (t SubAgentType) String() string {
	return string(t)
}

// String implements fmt.Stringer for SubAgentStatus.
func (s SubAgentStatus) String() string {
	return string(s)
}

// ensure types satisfy interfaces
var _ fmt.Stringer = SubAgentType("")
var _ fmt.Stringer = SubAgentStatus("")

// ensure strings.Builder is used
var _ = strings.Builder{}
