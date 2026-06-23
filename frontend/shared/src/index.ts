/**
 * @pux/shared — shared code between Pux Web UI and TUI.
 */

// Environment setup
export { setFetch, getFetch } from "./fetch-provider";
export { setBaseUrl, apiUrl } from "./server-url";

// State
export { usePuxStore } from "./pux-store";
export type { RunningAgentInfo, MCPServerInfo } from "./pux-store";

// Adapters
export { puxChatAdapter } from "./pux-chat-adapter";
export { createPuxHistoryAdapter, storedMessagesToThreadLikes, processHistoryResponse } from "./pux-history-adapter";

// Utilities
export { formatToolResult } from "./format-tool-result";
export { getToolArgPreview } from "./tool-arg-preview";
export { relativeTime } from "./relative-time";

// Types
export type {
	TokenUsage,
	ContextMetrics,
	PendingDecision,
	DecisionHint,
	Conversation,
	Project,
	WorkbenchTab,
	AgentState,
	AgentRound,
	ToolCallRecord,
	PersistedToolCall,
	SubAgentRecord,
	TuiView,
	ModelCost,
	ModelInfo,
	ProviderInfo,
	ProvidersMap,
} from "./types";
