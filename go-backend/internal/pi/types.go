package pi

import "encoding/json"

// RpcCommand represents a command sent to the Pi agent subprocess.
// See pi-mono/packages/coding-agent/src/modes/rpc/rpc-types.ts for the full protocol.
type RpcCommand struct {
	Type          string          `json:"type"`
	Message       string          `json:"message,omitempty"`
	Model         string          `json:"model,omitempty"`
	ModelId       string          `json:"modelId,omitempty"`  // For set_model command
	Provider      string          `json:"provider,omitempty"`
	ThinkingLevel string          `json:"thinkingLevel,omitempty"`
	Command       string          `json:"command,omitempty"`
	Timeout       int             `json:"timeout,omitempty"`
	WorkingDir    string          `json:"workingDir,omitempty"`
	SessionId     string          `json:"sessionId,omitempty"`
	Id            string          `json:"id,omitempty"`
	Data          interface{}     `json:"data,omitempty"`
}

// RpcResponse represents a response from the Pi agent subprocess.
type RpcResponse struct {
	Type    string      `json:"type"`
	Id      string      `json:"id,omitempty"`
	Success bool        `json:"success,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// AgentEvent represents a streaming event from the Pi agent.
// Pi's RPC protocol puts most data at the top level (not nested under "data").
// Tool fields (toolName, args, toolId) can appear either at the top level
// OR nested under "data" depending on the Pi version.
type AgentEvent struct {
	Type string `json:"type"`
	// Top-level fields from pi RPC
	AssistantMessageEvent *AssistantMessageEvent `json:"assistantMessageEvent,omitempty"`
	Message               json.RawMessage        `json:"message,omitempty"`
	Messages              json.RawMessage        `json:"messages,omitempty"`
	ToolResults           json.RawMessage        `json:"toolResults,omitempty"`
	// Top-level tool fields (Pi sends these directly, not nested under "data")
	ToolName string                 `json:"toolName,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolId   string                 `json:"toolId,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	// Legacy "data" field for error/state events
	Data AgentEventData `json:"data,omitempty"`
}

// AssistantMessageEvent is the nested event inside message_update from pi RPC.
type AssistantMessageEvent struct {
	Type         string          `json:"type"`
	ContentIndex int             `json:"contentIndex"`
	Delta        string          `json:"delta,omitempty"`
	Content      string          `json:"content,omitempty"`
	Partial      json.RawMessage `json:"partial,omitempty"`
}

// AgentEventData holds the payload of an agent event (used for error/state events).
type AgentEventData struct {
	// Text delta events (legacy)
	Text string `json:"text,omitempty"`

	// Tool execution events
	ToolName string                 `json:"toolName,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolId   string                 `json:"toolId,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`

	// Session state
	Model     string         `json:"model,omitempty"`
	Streaming bool           `json:"streaming,omitempty"`
	Input     interface{}    `json:"input,omitempty"` // float64 for usage stats, []string for model capabilities
	Output    float64        `json:"output,omitempty"`
	Cache     float64        `json:"cache,omitempty"`

	// Compaction
	CompactedMessages int `json:"compactedMessages,omitempty"`
	KeptMessages      int `json:"keptMessages,omitempty"`

	// Available models
	Models []ModelInfo `json:"models,omitempty"`
}

// ModelInfo represents a model available in Pi.
type ModelInfo struct {
	Provider string `json:"provider"`
	Id       string `json:"id"`
	Name     string `json:"name"`
}

// AgentEntry represents a single agent in the pool listing.
// Each agent runs in a per-project OpenShell namespace for isolation.
type AgentEntry struct {
	AgentId     string       `json:"agentId"`
	Project     string       `json:"project"`
	ProjectPath string       `json:"projectPath"`
	Namespace   string       `json:"namespace"` // OpenShell namespace (per-project isolation)
	State       SessionState `json:"state"`
}

// SessionState represents the current state of a Pi session.
type SessionState struct {
	Model     string  `json:"model"`
	Streaming bool    `json:"streaming"`
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Cache     float64 `json:"cache"`
	SessionId string  `json:"sessionId"`
}

// AgentMessage represents a message in the Pi conversation.
type AgentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Type    string `json:"type,omitempty"`
}

// SessionInfo represents a saved Pi session.
type SessionInfo struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Event types sent to the frontend via SSE
const (
	EventTextDelta       = "text_delta"
	EventThinkingDelta   = "thinking_delta"
	EventToolStart       = "tool_execution_start"
	EventToolEnd         = "tool_execution_end"
	EventAgentStart      = "agent_start"
	EventAgentEnd        = "agent_end"
	EventAgentSpawned    = "agent_spawned"
	EventCompactionStart = "compaction_start"
	EventCompactionEnd   = "compaction_end"
	EventError           = "error"
	EventStateUpdate     = "state_update"
	EventBranchCreated = "branch_created"
	EventCommitCreated = "commit_created"
	EventPushComplete  = "push_complete"
	EventPRCreated     = "pr_created"

	// Human-in-the-loop events
	EventApprovalRequest = "approval_request"
	EventQuestionAsked   = "question_asked"
)

// RPC command types
const (
	CmdPrompt           = "prompt"
	CmdSteer            = "steer"
	CmdFollowUp         = "follow_up"
	CmdAbort            = "abort"
	CmdGetState         = "get_state"
	CmdGetMessages      = "get_messages"
	CmdCompact          = "compact"
	CmdSetModel         = "set_model"
	CmdGetModels        = "get_available_models"
	CmdBash             = "bash"
	CmdAbortBash        = "abort_bash"
	CmdListSessions     = "list_sessions"
	CmdSwitchSession    = "switch_session"
)

// Pi RPC event types (from Pi subprocess stdout)
const (
	RpcEventAgentStart      = "agent_start"
	RpcEventAgentEnd        = "agent_end"
	RpcEventTurnEnd         = "turn_end"
	RpcEventMessageStart    = "message_start"
	RpcEventMessageUpdate   = "message_update"
	RpcEventMessageEnd      = "message_end"
	RpcEventToolStart       = "tool_execution_start"
	RpcEventToolEnd         = "tool_execution_end"
	RpcEventCompactionStart = "compaction_start"
	RpcEventCompactionEnd   = "compaction_end"
	RpcEventResponse        = "response"
	RpcEventStateUpdate     = "state_update"
	RpcEventError           = "error"
)

// ApprovalRequestData is sent to the frontend when the agent needs approval.
type ApprovalRequestData struct {
	RequestID string                 `json:"requestId"`
	Type      string                 `json:"type"` // "tool_confirm", "plan", "question"
	ToolName  string                 `json:"toolName,omitempty"`
	ToolArgs  map[string]interface{} `json:"toolArgs,omitempty"`
	Message   string                 `json:"message"`
	Risk      string                 `json:"risk"` // "low", "medium", "high"
}

// ApprovalResponse is sent from the frontend when the user responds to an approval.
type ApprovalResponse struct {
	Action  string `json:"action"` // "approve", "deny", "answer"
	Message string `json:"message,omitempty"`
}

// IsRiskyBashCommand checks if a bash command needs human approval.
func IsRiskyBashCommand(cmd string) (bool, string) {
	riskyPatterns := []struct {
		pattern string
		reason  string
	}{
		{"git push", "Pushing to remote repository"},
		{"git push-f", "Force pushing to remote"},
		{"rm -rf", "Recursive file deletion"},
		{"rm -r", "Recursive file deletion"},
		{"drop table", "Dropping database table"},
		{"curl -X DELETE", "HTTP DELETE request"},
		{"curl -X POST", "HTTP POST request to external service"},
		{"curl -X PUT", "HTTP PUT request to external service"},
		{":(){ :|:&", "Fork bomb pattern"},
	}
	for _, p := range riskyPatterns {
		if len(cmd) >= len(p.pattern) {
			for i := 0; i <= len(cmd)-len(p.pattern); i++ {
				if cmd[i:i+len(p.pattern)] == p.pattern {
					return true, p.reason
				}
			}
		}
	}
	return false, ""
}
