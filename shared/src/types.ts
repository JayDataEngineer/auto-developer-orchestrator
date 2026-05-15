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

export interface PendingQuestion {
	questionId: string;
	question: string;
	options: string[];
	allowFreeText: boolean;
}

export interface PendingApproval {
	requestId: string;
	title: string;
	description: string;
}

export interface PendingPlan {
	planId: string;
	name: string;
	content: string;
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
