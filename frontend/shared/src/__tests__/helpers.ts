/**
 * Shared test helpers for the @pux/shared package.
 *
 * Factories + mock builders reused across store tests to avoid duplicating
 * fixture objects and vi.mock boilerplate in every test file.
 */
import type { AgentState, ToolCallRecord } from "../types";

// ── Factories ──────────────────────────────────────────────────

/** makeAgent builds a minimal AgentState, with optional field overrides. */
export function makeAgent(overrides: Partial<AgentState> = {}): AgentState {
	return {
		agentId: "agent-1",
		agentName: "Test Agent",
		task: "Do something",
		status: "running",
		startedAt: 1000,
		rounds: [],
		toolCalls: [],
		...overrides,
	};
}

/** makeToolCall builds a minimal ToolCallRecord, with optional field overrides. */
export function makeToolCall(overrides: Partial<ToolCallRecord> = {}): ToolCallRecord {
	return {
		toolName: "bash",
		args: { command: "echo hi" },
		result: "hi",
		isError: false,
		timestamp: 2000,
		...overrides,
	};
}

// NOTE: vi.mock("../fetch-provider", ...) factories cannot be shared via import
// because vitest hoists vi.mock calls above imports. Keep the inline factory in
// each test file — it is small and idiomatic. The shared value here is the
// factory/fixture builders above.
