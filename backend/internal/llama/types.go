package llama

import (
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Type aliases to core domain types.
// The canonical definitions live in internal/core/provider.go and internal/core/event.go.
// This file exists for backwards compatibility within the llama package.

type Message = core.Message
type CacheControl = core.CacheControl
type ToolCallResponse = core.ToolCallResponse
type FunctionCallData = core.FunctionCallData
type ToolCall = core.ToolCall
type ToolResult = core.ToolResult
type StreamDelta = core.StreamDelta
type ToolCallDelta = core.ToolCallDelta
type ExtraContent = core.ExtraContent
type GoogleExtra = core.GoogleExtra
type FunctionCallDelta = core.FunctionCallDelta
type StreamUsage = core.StreamUsage
type FinishReason = core.FinishReason
type ChatEventType = core.ChatEventType
type ChatEvent = core.ChatEvent
type GenerateOptions = core.GenerateOptions
type OpenAITool = core.OpenAITool
type FunctionDef = core.FunctionDef

// Event types used by SSE consumers.
type AgentEventType = core.AgentEventType
type AgentEvent = core.AgentEvent
type EventPayload = core.EventPayload
type ApprovalResponse = core.ApprovalResponse

// Typed event payload aliases
type TextDelta = core.TextDelta
type ThinkingDelta = core.ThinkingDelta
type ToolStart = core.ToolStart
type ToolEnd = core.ToolEnd
type ToolUpdate = core.ToolUpdate
type AgentStartData = core.AgentStartData
type AgentEndData = core.AgentEndData
type AgentSpawnedData = core.AgentSpawnedData
type SubAgentStartData = core.SubAgentStartData
type SubAgentEndData = core.SubAgentEndData
type StepStartData = core.StepStartData
type StepEndData = core.StepEndData
type ErrorEventData = core.ErrorEventData
type DecisionRequestData = core.DecisionRequestData
type SourceEventData = core.SourceEventData
type CompactionStartData = core.CompactionStartData
type CompactionEndData = core.CompactionEndData
type ContextUpdateData = core.ContextUpdateData
type TaskStartedData = core.TaskStartedData
type TaskCompletedData = core.TaskCompletedData
type TaskBackgroundData = core.TaskBackgroundData
type ArtifactCreatedData = core.ArtifactCreatedData
type ArtifactUpdatedData = core.ArtifactUpdatedData
type PlanCreatedData = core.PlanCreatedData
type PlanUpdatedData = core.PlanUpdatedData
type HookRequestData = core.HookRequestData
type MouseActionData = core.MouseActionData
type ProviderRetryData = core.ProviderRetryData
type SafeguardFallbackData = core.SafeguardFallbackData
type ResourceConflictData = core.ResourceConflictData
type AgentMessageData = core.AgentMessageData
type AgentStatusData = core.AgentStatusData

// Constants re-exported for convenience.
const (
	FinishStop      = core.FinishStop
	FinishToolCalls = core.FinishToolCalls
)

const (
	ChatEventContent  = core.ChatEventContent
	ChatEventThinking = core.ChatEventThinking
	ChatEventToolChunk = core.ChatEventToolChunk
	ChatEventDone     = core.ChatEventDone
	ChatEventError    = core.ChatEventError
)

const (
	EventTypeTextDelta       = core.EventTypeTextDelta
	EventTypeThinkingDelta   = core.EventTypeThinkingDelta
	EventTypeToolStart       = core.EventTypeToolStart
	EventTypeToolEnd         = core.EventTypeToolEnd
	EventTypeAgentStart      = core.EventTypeAgentStart
	EventTypeAgentEnd        = core.EventTypeAgentEnd
	EventTypeError           = core.EventTypeError
	EventTypeArtifactCreated = core.EventTypeArtifactCreated
	EventTypeArtifactUpdated = core.EventTypeArtifactUpdated
	EventTypePlanCreated     = core.EventTypePlanCreated
	EventTypePlanUpdated     = core.EventTypePlanUpdated
	EventTypeSubAgentStart   = core.EventTypeSubAgentStart
	EventTypeSubAgentEnd     = core.EventTypeSubAgentEnd
	EventTypeApprovalRequest = core.EventTypeApprovalRequest
	EventTypeCompactionStart = core.EventTypeCompactionStart
	EventTypeCompactionEnd   = core.EventTypeCompactionEnd
	EventTypeToolUpdate      = core.EventTypeToolUpdate
	EventTypeAgentSpawned    = core.EventTypeAgentSpawned
	EventTypeHookRequest     = core.EventTypeHookRequest
	EventTypeStepStart       = core.EventTypeStepStart
	EventTypeStepEnd         = core.EventTypeStepEnd
	EventTypeUserQuestion    = core.EventTypeUserQuestion
	EventTypeDecisionRequest = core.EventTypeDecisionRequest
	EventTypeSource          = core.EventTypeSource
	EventTypeContextUpdate   = core.EventTypeContextUpdate
	EventTypeMouseAction     = core.EventTypeMouseAction
	EventTypeProviderRetry     = core.EventTypeProviderRetry
	EventTypeSafeguardFallback = core.EventTypeSafeguardFallback
	EventTypeResourceConflict  = core.EventTypeResourceConflict
	EventTypeAgentMessage      = core.EventTypeAgentMessage
	EventTypeAgentStatus       = core.EventTypeAgentStatus
	EventTypeMCPEndpointChanged = core.EventTypeMCPEndpointChanged
)
