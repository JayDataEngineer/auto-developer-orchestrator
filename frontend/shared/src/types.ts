/**
 * Shared types for Pux interfaces (web + TUI).
 */

export interface TokenUsage {
	input: number;
	output: number;
	cache: number;
	model?: string;
}

export interface ContextMetrics {
	contextTokens: number;
	contextSize: number;
	contextUtil: number;
	compactionType: string;
}

// ── HITL Decision (unified) ──

export type DecisionHint = "question" | "approval" | "plan_review";

export interface PendingDecision {
	decisionId: string;
	sourceTool: string;
	title: string;
	description: string;
	hint: DecisionHint;
	options?: string[];
	allowFreeText?: boolean;
	metadata?: Record<string, unknown>;
}

export interface Conversation {
	project: string;
	agentId: string;
	lastMessage: string;
	lastAt: string;
	messageCount: number;
	title: string;
	status?: "processing" | "unread" | "read" | "";
}

export interface Project {
	name: string;
	path: string;
	description?: string;
	version?: string;
	hasManifest?: boolean;
}

export type WorkbenchTab = "vnc" | "editor" | "scheduler" | "workers" | "settings";

// ── Agent monitoring ──

export interface AgentRound {
	thinking?: string;
	toolCalls: ToolCallRecord[];
	text?: string;
}

export interface AgentState {
	agentId: string;
	agentName: string;
	task: string;
	status: "running" | "complete" | "error";
	startedAt: number;
	endedAt?: number;
	rounds: AgentRound[];
	toolCalls: ToolCallRecord[];
	thinkingText?: string;
	text?: string;
	result?: string;
	error?: string;
	transcriptId?: string;
}

export interface ToolCallRecord {
	toolName: string;
	toolCallId?: string;
	args?: unknown;
	result?: unknown;
	isError?: boolean;
	timestamp: number;
	endedAt?: number;
}

// ── Persisted sub-agent trace (from tool_calls JSON) ──

export interface PersistedToolCall {
	id?: string;
	name: string;
	args?: Record<string, unknown>;
	result?: string;
	error?: string;
}

export interface SubAgentRecord {
	name: string;
	status: string;
	toolCalls: PersistedToolCall[];
	thinking?: string;
	text?: string;
	result?: string;
	error?: string;
}

// ── TUI Views ──

export type TuiView = "chat" | "agents" | "tools" | "files" | "conversations";

// ── Providers & Models ──

export interface ModelCost {
	input: number;
	output: number;
	cacheRead: number;
	cacheWrite: number;
}

export interface ModelInfo {
	id: string;
	name: string;
	reasoning: boolean;
	input: string[];
	cost: ModelCost;
	contextWindow: number;
	maxTokens: number;
}

export interface ProviderInfo {
	baseUrl: string;
	api: string;
	status: "available" | "configured" | "unavailable";
	compat: {
		supportsDeveloperRole: boolean;
		supportsReasoningEffort: boolean;
	};
	models: ModelInfo[];
}

export type ProvidersMap = Record<string, ProviderInfo>;
