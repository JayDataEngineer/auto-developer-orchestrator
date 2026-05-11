/**
 * Shared types for pux-subagents extension.
 * Tracks sub-agent state across the delegation lifecycle.
 */

/** Status of a sub-agent (employee) during or after delegation */
export type AgentStatus = "running" | "completed" | "failed" | "pending";

/** A recently executed tool by a sub-agent */
export interface RecentTool {
	tool: string;
	args: string;
	endedAt?: number;
}

/** Tracked info for a single sub-agent invocation */
export interface SubAgentInfo {
	agentName: string;
	task: string;
	status: AgentStatus;
	toolCount: number;
	currentTool?: string;
	currentToolArgs?: string;
	recentTools: RecentTool[];
	recentOutput: string[];
	lastAction: string;
	startedAt: number;
	endedAt?: number;
	error?: string;
}

/** Max recent tools/output to keep per agent */
const MAX_RECENT_TOOLS = 8;
const MAX_RECENT_OUTPUT = 6;

export function addRecentTool(info: SubAgentInfo, tool: string, args: string): void {
	info.recentTools.push({ tool, args });
	if (info.recentTools.length > MAX_RECENT_TOOLS) {
		info.recentTools.shift();
	}
}

export function addRecentOutput(info: SubAgentInfo, text: string): void {
	const lines = text.split("\n").filter((l) => l.trim());
	for (const line of lines) {
		info.recentOutput.push(line);
	}
	if (info.recentOutput.length > MAX_RECENT_OUTPUT) {
		info.recentOutput.splice(0, info.recentOutput.length - MAX_RECENT_OUTPUT);
	}
}

/**
 * Extension-wide state for all active/recent sub-agents.
 * Updated via tool_execution and subagent SSE events.
 */
export interface SubAgentState {
	/** Currently tracked agents keyed by agent name */
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
