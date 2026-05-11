/**
 * Shared types for pux-subagents extension.
 * Tracks sub-agent state across the delegation lifecycle.
 */

/** Status of a sub-agent (employee) during or after delegation */
export type AgentStatus = "running" | "completed" | "failed" | "pending";

/** Tracked info for a single sub-agent invocation */
export interface SubAgentInfo {
	agentName: string;
	task: string;
	status: AgentStatus;
	toolCount: number;
	lastAction: string;
	startedAt: number;
	endedAt?: number;
	error?: string;
}

/**
 * Extension-wide state for all active/recent sub-agents.
 * Updated via tool_execution and subagent SSE events.
 */
export interface SubAgentState {
	/** Currently tracked agents keyed by agent name (or toolCallId for parallel) */
	agents: Map<string, SubAgentInfo>;
	/** Ordered list of agent names for chain visualization */
	chain: string[];
	/** Total agents completed in this turn */
	completed: number;
	/** Total agents that failed in this turn */
	failed: number;
}

export function createSubAgentState(): SubAgentState {
	return {
		agents: new Map(),
		chain: [],
		completed: 0,
		failed: 0,
	};
}
