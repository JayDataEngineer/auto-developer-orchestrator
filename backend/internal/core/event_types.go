package core

// AgentEventType identifies the type of an agent event emitted to subscribers.
type AgentEventType string

const (
	EventTypeTextDelta       AgentEventType = "text_delta"
	EventTypeThinkingDelta   AgentEventType = "thinking_delta"
	EventTypeToolStart       AgentEventType = "tool_execution_start"
	EventTypeToolEnd         AgentEventType = "tool_execution_end"
	EventTypeAgentStart      AgentEventType = "agent_start"
	EventTypeAgentEnd        AgentEventType = "agent_end"
	EventTypeError           AgentEventType = "error"
	EventTypeArtifactCreated AgentEventType = "artifact_created"
	EventTypeArtifactUpdated AgentEventType = "artifact_updated"
	EventTypePlanCreated     AgentEventType = "plan_created"
	EventTypePlanUpdated     AgentEventType = "plan_updated"
	EventTypeSubAgentStart   AgentEventType = "subagent_start"
	EventTypeSubAgentEnd     AgentEventType = "subagent_end"
	EventTypeApprovalRequest AgentEventType = "approval_request"
	EventTypeCompactionStart AgentEventType = "compaction_start"
	EventTypeCompactionEnd   AgentEventType = "compaction_end"
	EventTypeToolUpdate      AgentEventType = "tool_update"
	EventTypeAgentSpawned    AgentEventType = "agent_spawned"
	EventTypeStepStart       AgentEventType = "step_start"
	EventTypeStepEnd         AgentEventType = "step_end"
	EventTypeHookRequest     AgentEventType = "hook_request"
	EventTypeDecisionRequest AgentEventType = "decision_request" // unified HITL
	EventTypeUserQuestion    AgentEventType = "user_question"    // legacy, replaced by decision_request
	EventTypeSource          AgentEventType = "source"           // citation/reference link
	EventTypeTaskStarted     AgentEventType = "task_started"     // background task registered
	EventTypeTaskCompleted   AgentEventType = "task_completed"   // background task finished
	EventTypeTaskBackground  AgentEventType = "task_background"  // foreground → background conversion
	EventTypeContextUpdate   AgentEventType = "context_update"   // per-round context metrics
	EventTypeMouseAction     AgentEventType = "mouse_action"     // visual mouse overlay for sandbox
)
