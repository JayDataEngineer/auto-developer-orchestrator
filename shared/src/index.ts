/**
 * @pux/shared — shared code between Pux Web UI and TUI.
 */

// Environment setup
export { setFetch, getFetch } from "./fetch-provider";
export { setBaseUrl, apiUrl } from "./server-url";

// State
export { usePuxStore } from "./pux-store";

// Adapters
export { puxChatAdapter } from "./pux-chat-adapter";
export { createPuxHistoryAdapter } from "./pux-history-adapter";

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
	ToolCallRecord,
	TuiView,
} from "./types";
