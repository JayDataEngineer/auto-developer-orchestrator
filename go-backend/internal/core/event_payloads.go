package core

// EventPayload is the sealed interface for typed event data.
// Only types defined in this package can implement it.
type EventPayload interface{ seal() }

// --- Text streaming ---

// TextDelta is a chunk of response text.
type TextDelta struct {
	Text      string `json:"text,omitempty"`
	AgentName string `json:"agentName,omitempty"`
}

func (TextDelta) seal() {}

// ThinkingDelta is a chunk of reasoning/thinking text.
type ThinkingDelta struct {
	Text      string `json:"text,omitempty"`
	AgentName string `json:"agentName,omitempty"`
}

func (ThinkingDelta) seal() {}

// --- Tool execution ---

// ToolStart is emitted when a tool call begins.
type ToolStart struct {
	ToolID    string         `json:"toolId,omitempty"`
	ToolName  string         `json:"toolName,omitempty"`
	ToolArgs  map[string]any `json:"args,omitempty"`
	AgentName string         `json:"agentName,omitempty"`
}

func (ToolStart) seal() {}

// ToolEnd is emitted when a tool call completes.
type ToolEnd struct {
	ToolID       string `json:"toolId,omitempty"`
	ToolName     string `json:"toolName,omitempty"`
	Result       any    `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	Artifact     any    `json:"artifact,omitempty"`
	ModelContent string `json:"modelContent,omitempty"`
	AgentName    string `json:"agentName,omitempty"`
}

func (ToolEnd) seal() {}

// ToolUpdate is emitted for long-running tool progress updates.
type ToolUpdate struct {
	ToolID    string `json:"toolId,omitempty"`
	ToolName  string `json:"toolName,omitempty"`
	Text      string `json:"text,omitempty"`
	AgentName string `json:"agentName,omitempty"`
}

func (ToolUpdate) seal() {}

// --- Agent lifecycle ---

// AgentStartData is emitted when an agent loop begins.
type AgentStartData struct{}

func (AgentStartData) seal() {}

// AgentEndData is emitted when an agent loop completes, with usage metrics.
type AgentEndData struct {
	Input         float64 `json:"input,omitempty"`          // cumulative total input tokens
	Output        float64 `json:"output,omitempty"`         // cumulative total output tokens
	Cache         float64 `json:"cache,omitempty"`
	Model         string  `json:"model,omitempty"`
	ContextWindow int     `json:"contextWindow,omitempty"`  // model's max context window
	ContextTokens int     `json:"contextTokens,omitempty"`  // actual prompt tokens in last API call (current context size)
}

func (AgentEndData) seal() {}

// AgentSpawnedData is emitted when a new agent session is created.
type AgentSpawnedData struct {
	AgentID string `json:"agentId,omitempty"`
}

func (AgentSpawnedData) seal() {}

// --- Sub-agent delegation ---

// SubAgentStartData is emitted when a delegated sub-agent begins.
type SubAgentStartData struct {
	AgentName    string `json:"agentName,omitempty"`
	Task         string `json:"task,omitempty"`
	TranscriptID string `json:"transcriptId,omitempty"`
}

func (SubAgentStartData) seal() {}

// SubAgentEndData is emitted when a delegated sub-agent completes.
type SubAgentEndData struct {
	AgentName    string `json:"agentName,omitempty"`
	Status       string `json:"status,omitempty"`
	Task         string `json:"task,omitempty"`
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	TranscriptID string `json:"transcriptId,omitempty"`
}

func (SubAgentEndData) seal() {}

// --- Step tracking ---

// StepStartData is emitted at the beginning of each agent loop round.
type StepStartData struct {
	Round int `json:"round,omitempty"`
}

func (StepStartData) seal() {}

// StepEndData is emitted at the end of each agent loop round.
type StepEndData struct {
	Round    int    `json:"round,omitempty"`
	Decision string `json:"decision,omitempty"`
}

func (StepEndData) seal() {}

// --- Error ---

// ErrorEventData is emitted when an error occurs.
type ErrorEventData struct {
	Error string `json:"error,omitempty"`
}

func (ErrorEventData) seal() {}

// --- Decision / HITL ---

// DecisionRequestData is the unified human-in-the-loop event.
type DecisionRequestData struct {
	ID            string         `json:"decisionId,omitempty"`
	SourceTool    string         `json:"sourceTool,omitempty"`
	Title         string         `json:"title,omitempty"`
	Description   string         `json:"description,omitempty"`
	Hint          string         `json:"hint,omitempty"`
	Options       []string       `json:"options,omitempty"`
	AllowFreeText bool           `json:"allowFreeText,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (DecisionRequestData) seal() {}

// --- Source / citation ---

// SourceEventData is emitted when a tool result contains a reference URL.
type SourceEventData struct {
	SourceType string `json:"sourceType,omitempty"`
	SourceURL  string `json:"sourceUrl,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
}

func (SourceEventData) seal() {}

// --- Compaction ---

// CompactionStartData is emitted when context compaction begins.
type CompactionStartData struct {
	CompactionType string `json:"compactionType,omitempty"`
}

func (CompactionStartData) seal() {}

// CompactionEndData is emitted when context compaction completes.
type CompactionEndData struct {
	CompactedMessages int     `json:"compactedMessages,omitempty"`
	KeptMessages      int     `json:"keptMessages,omitempty"`
	ContextTokens     int     `json:"contextTokens,omitempty"`
	ContextSize       int     `json:"contextSize,omitempty"`
	ContextUtil       float64 `json:"contextUtil,omitempty"`
}

func (CompactionEndData) seal() {}

// --- Background tasks ---

// TaskStartedData is emitted when a background task is registered.
type TaskStartedData struct {
	TaskID  string `json:"taskId,omitempty"`
	Command string `json:"command,omitempty"`
}

func (TaskStartedData) seal() {}

// TaskCompletedData is emitted when a background task finishes.
type TaskCompletedData struct {
	TaskID   string `json:"taskId,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
}

func (TaskCompletedData) seal() {}

// TaskBackgroundData is emitted when a foreground task is converted to background.
type TaskBackgroundData struct {
	TaskID string `json:"taskId,omitempty"`
}

func (TaskBackgroundData) seal() {}

// --- Artifacts & plans ---

// ArtifactCreatedData is emitted when a tool produces a new artifact.
type ArtifactCreatedData struct {
	Path string `json:"path,omitempty"`
	Type string `json:"type,omitempty"`
}

func (ArtifactCreatedData) seal() {}

// ArtifactUpdatedData is emitted when an existing artifact is modified.
type ArtifactUpdatedData struct {
	Path string `json:"path,omitempty"`
	Type string `json:"type,omitempty"`
}

func (ArtifactUpdatedData) seal() {}

// PlanCreatedData is emitted when a new plan is created.
type PlanCreatedData struct {
	ID      string `json:"id,omitempty"`
	Content string `json:"content,omitempty"`
}

func (PlanCreatedData) seal() {}

// PlanUpdatedData is emitted when a plan is updated.
type PlanUpdatedData struct {
	ID      string `json:"id,omitempty"`
	Content string `json:"content,omitempty"`
}

func (PlanUpdatedData) seal() {}

// --- Hook interception ---

// HookRequestData is emitted when a hook needs user input.
type HookRequestData struct {
	HookPoint string `json:"hookPoint,omitempty"`
	HookID    string `json:"hookId,omitempty"`
	Message   string `json:"message,omitempty"`
}

func (HookRequestData) seal() {}
