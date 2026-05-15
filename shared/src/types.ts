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
}

export interface Project {
	name: string;
	path: string;
	description?: string;
	version?: string;
	hasManifest?: boolean;
}

export type WorkbenchTab = "vnc" | "editor" | "scheduler";

// ── Agent monitoring ──

export interface AgentState {
	agentId: string;
	agentName: string;
	task: string;
	status: "running" | "complete" | "error";
	startedAt: number;
	endedAt?: number;
	toolCalls: ToolCallRecord[];
	result?: string;
	error?: string;
}

export interface ToolCallRecord {
	toolName: string;
	args?: unknown;
	result?: unknown;
	isError?: boolean;
	timestamp: number;
}

// ── TUI Views ──

export type TuiView = "chat" | "agents" | "tools" | "files" | "conversations";
