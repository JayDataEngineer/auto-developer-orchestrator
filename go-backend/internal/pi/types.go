package pi

// RpcCommand represents a command sent to the Pi agent subprocess.
type RpcCommand struct {
	Type          string          `json:"type"`
	Message       string          `json:"message,omitempty"`
	Model         string          `json:"model,omitempty"`
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
type AgentEvent struct {
	Type string          `json:"type"`
	Data AgentEventData `json:"data,omitempty"`
}

// AgentEventData holds the payload of an agent event.
type AgentEventData struct {
	// Text delta events
	Text string `json:"text,omitempty"`

	// Tool execution events
	ToolName string                 `json:"toolName,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolId   string                 `json:"toolId,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`

	// Session state
	Model     string  `json:"model,omitempty"`
	Streaming bool    `json:"streaming,omitempty"`
	Input     float64 `json:"input,omitempty"`
	Output    float64 `json:"output,omitempty"`
	Cache     float64 `json:"cache,omitempty"`

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
	EventTextDelta        = "text_delta"
	EventThinkingDelta    = "thinking_delta"
	EventToolStart        = "tool_execution_start"
	EventToolEnd          = "tool_execution_end"
	EventAgentStart       = "agent_start"
	EventAgentEnd         = "agent_end"
	EventCompactionStart  = "compaction_start"
	EventCompactionEnd    = "compaction_end"
	EventError            = "error"
	EventStateUpdate      = "state_update"
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
	RpcEventMessageUpdate   = "message_update"
	RpcEventToolStart       = "tool_execution_start"
	RpcEventToolEnd         = "tool_execution_end"
	RpcEventCompactionStart = "compaction_start"
	RpcEventCompactionEnd   = "compaction_end"
	RpcEventResponse        = "response"
	RpcEventStateUpdate     = "state_update"
	RpcEventError           = "error"
)
