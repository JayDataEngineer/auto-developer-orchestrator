/**
 * Tests for subagent round tracking and tool call matching.
 *
 * These tests prove two critical fixes:
 * 1. Tool execution end events match the CORRECT tool by ID (not last-open)
 * 2. Thinking blocks create separate rounds (not merged into old ones)
 * 3. Subagent output text is never truncated
 */

import { describe, test, expect, beforeEach } from "vitest";
import { usePuxStore } from "@pux/shared";
import type { AgentState, ToolCallRecord } from "@pux/shared";

// Reset store before each test
beforeEach(() => {
	usePuxStore.getState().clearAgents();
	usePuxStore.getState().startNewChat();
});

// Helper to create a running agent
function addRunningAgent(agentId: string, agentName: string, task: string): void {
	usePuxStore.getState().addAgent({
		agentId,
		agentName,
		task,
		status: "running",
		startedAt: Date.now(),
		rounds: [],
		toolCalls: [],
	});
}

function getAgent(agentId: string): AgentState | undefined {
	return usePuxStore.getState().agents.get(agentId);
}

// ────────────────────────────────────────────────────────────────
// Test 1: tool_execution_end matches by toolCallId
//
// Simulates the exact SSE event sequence:
//   tool_start(research, id=A)
//   tool_start(search, id=B)   ← research still running!
//   tool_end(research, id=A)   ← must match A, not B
//   tool_end(search, id=B)
// ────────────────────────────────────────────────────────────────

describe("tool call matching by ID", () => {
	test("end event matches correct tool by toolCallId when multiple tools are open", () => {
		addRunningAgent("ag-1", "researcher", "search the web");

		// Start research tool
		usePuxStore.getState().appendAgentRoundToolCall("ag-1", {
			toolName: "research",
			toolCallId: "call-research-001",
			args: { query: "test" },
			timestamp: 1000,
		});

		// Start search tool BEFORE research ends (parallel tools)
		usePuxStore.getState().appendAgentRoundToolCall("ag-1", {
			toolName: "search",
			toolCallId: "call-search-002",
			args: { query: "test2" },
			timestamp: 2000,
		});

		// Verify both are open
		const agent = getAgent("ag-1")!;
		expect(agent.toolCalls[0].endedAt).toBeUndefined();
		expect(agent.toolCalls[1].endedAt).toBeUndefined();

		// Research ends — must match call-research-001 specifically
		usePuxStore.getState().updateAgentRoundToolCall("ag-1", {
			toolCallId: "call-research-001",
			endedAt: 3000,
			result: "research results here",
		});

		// Search ends — must match call-search-002 specifically
		usePuxStore.getState().updateAgentRoundToolCall("ag-1", {
			toolCallId: "call-search-002",
			endedAt: 4000,
			result: "search results here",
		});

		// PROVE: research got ITS result, not search's
		const final = getAgent("ag-1")!;
		expect(final.toolCalls[0].toolName).toBe("research");
		expect(final.toolCalls[0].endedAt).toBe(3000);
		expect(final.toolCalls[0].result).toBe("research results here");

		expect(final.toolCalls[1].toolName).toBe("search");
		expect(final.toolCalls[1].endedAt).toBe(4000);
		expect(final.toolCalls[1].result).toBe("search results here");
	});

	test("end event with wrong toolCallId does not corrupt other tools", () => {
		addRunningAgent("ag-2", "researcher", "test");

		usePuxStore.getState().appendAgentRoundToolCall("ag-2", {
			toolName: "scrape",
			toolCallId: "call-scrape-100",
			args: { url: "https://example.com" },
			timestamp: 1000,
		});

		// End event with a toolCallId that doesn't match any tool
		usePuxStore.getState().updateAgentRoundToolCall("ag-2", {
			toolCallId: "nonexistent-id",
			endedAt: 2000,
		});

		// The scrape tool should STILL be open (not matched by wrong ID)
		const agent = getAgent("ag-2")!;
		expect(agent.toolCalls[0].endedAt).toBeUndefined();
	});

	test("fallback to last-open tool when no toolCallId provided", () => {
		addRunningAgent("ag-3", "researcher", "test");

		usePuxStore.getState().appendAgentRoundToolCall("ag-3", {
			toolName: "bash",
			toolCallId: "call-bash-200",
			args: { command: "ls" },
			timestamp: 1000,
		});

		// No toolCallId in update — should match last open tool
		usePuxStore.getState().updateAgentRoundToolCall("ag-3", {
			endedAt: 2000,
			result: "file1.txt\nfile2.txt",
		});

		const agent = getAgent("ag-3")!;
		expect(agent.toolCalls[0].endedAt).toBe(2000);
		expect(agent.toolCalls[0].result).toBe("file1.txt\nfile2.txt");
	});
});

// ────────────────────────────────────────────────────────────────
// Test 2: Round transitions create separate thinking blocks
//
// Proves: think → tool → tool_end → think creates TWO rounds
// ────────────────────────────────────────────────────────────────

describe("round-based thinking separation", () => {
	test("think → tool → tool_end → think creates two separate rounds", () => {
		addRunningAgent("ag-4", "researcher", "test");

		// Round 1 thinking
		usePuxStore.getState().appendAgentRoundThinking("ag-4", "I need to search");
		usePuxStore.getState().appendAgentRoundThinking("ag-4", " for Texas news");

		// Round 1 tool
		usePuxStore.getState().appendAgentRoundToolCall("ag-4", {
			toolName: "research",
			toolCallId: "call-r1",
			args: { query: "Texas" },
			timestamp: 1000,
		});

		// Round 1 tool completes
		usePuxStore.getState().updateAgentRoundToolCall("ag-4", {
			toolCallId: "call-r1",
			endedAt: 2000,
			result: "news results",
		});

		// Round 2 thinking — must create NEW round
		usePuxStore.getState().appendAgentRoundThinking("ag-4", "Let me try a different search");

		const agent = getAgent("ag-4")!;
		expect(agent.rounds.length).toBe(2);

		// Round 1: "I need to search for Texas news" + research tool
		expect(agent.rounds[0].thinking).toBe("I need to search for Texas news");
		expect(agent.rounds[0].toolCalls.length).toBe(1);
		expect(agent.rounds[0].toolCalls[0].toolName).toBe("research");
		expect(agent.rounds[0].toolCalls[0].endedAt).toBe(2000);

		// Round 2: "Let me try a different search" + no tools yet
		expect(agent.rounds[1].thinking).toBe("Let me try a different search");
		expect(agent.rounds[1].toolCalls.length).toBe(0);
	});

	test("think → tool → tool_end → think → tool → tool_end creates three rounds", () => {
		addRunningAgent("ag-5", "researcher", "test");

		// Round 1
		usePuxStore.getState().appendAgentRoundThinking("ag-5", "Round 1 thinking");
		usePuxStore.getState().appendAgentRoundToolCall("ag-5", {
			toolName: "search",
			toolCallId: "c1",
			args: {},
			timestamp: 1000,
		});
		usePuxStore.getState().updateAgentRoundToolCall("ag-5", {
			toolCallId: "c1",
			endedAt: 2000,
		});

		// Round 2
		usePuxStore.getState().appendAgentRoundThinking("ag-5", "Round 2 thinking");
		usePuxStore.getState().appendAgentRoundToolCall("ag-5", {
			toolName: "scrape",
			toolCallId: "c2",
			args: {},
			timestamp: 3000,
		});
		usePuxStore.getState().updateAgentRoundToolCall("ag-5", {
			toolCallId: "c2",
			endedAt: 4000,
		});

		// Round 3
		usePuxStore.getState().appendAgentRoundThinking("ag-5", "Round 3 thinking");
		usePuxStore.getState().appendAgentRoundToolCall("ag-5", {
			toolName: "scrape",
			toolCallId: "c3",
			args: {},
			timestamp: 5000,
		});
		usePuxStore.getState().updateAgentRoundToolCall("ag-5", {
			toolCallId: "c3",
			endedAt: 6000,
		});

		const agent = getAgent("ag-5")!;
		expect(agent.rounds.length).toBe(3);

		// Each round has its own thinking
		expect(agent.rounds[0].thinking).toBe("Round 1 thinking");
		expect(agent.rounds[1].thinking).toBe("Round 2 thinking");
		expect(agent.rounds[2].thinking).toBe("Round 3 thinking");

		// Each round has exactly one tool
		expect(agent.rounds[0].toolCalls.length).toBe(1);
		expect(agent.rounds[1].toolCalls.length).toBe(1);
		expect(agent.rounds[2].toolCalls.length).toBe(1);
	});

	test("thinking without tools does NOT create separate rounds", () => {
		addRunningAgent("ag-6", "researcher", "test");

		// Continuous thinking without tools — should stay in one round
		usePuxStore.getState().appendAgentRoundThinking("ag-6", "I need to");
		usePuxStore.getState().appendAgentRoundThinking("ag-6", " search for");
		usePuxStore.getState().appendAgentRoundThinking("ag-6", " news");

		const agent = getAgent("ag-6")!;
		expect(agent.rounds.length).toBe(1);
		expect(agent.rounds[0].thinking).toBe("I need to search for news");
	});

	test("round transition only happens when ALL tools are complete", () => {
		addRunningAgent("ag-7", "researcher", "test");

		// Two tools started
		usePuxStore.getState().appendAgentRoundThinking("ag-7", "Let me search");
		usePuxStore.getState().appendAgentRoundToolCall("ag-7", {
			toolName: "search",
			toolCallId: "c1",
			args: {},
			timestamp: 1000,
		});
		usePuxStore.getState().appendAgentRoundToolCall("ag-7", {
			toolName: "scrape",
			toolCallId: "c2",
			args: {},
			timestamp: 2000,
		});

		// Only ONE tool completes
		usePuxStore.getState().updateAgentRoundToolCall("ag-7", {
			toolCallId: "c1",
			endedAt: 3000,
		});

		// New thinking — should NOT create new round (scrape still running)
		usePuxStore.getState().appendAgentRoundThinking("ag-7", "More thinking");

		const agent = getAgent("ag-7")!;
		expect(agent.rounds.length).toBe(1); // Still one round
		// Thinking should be appended, not in a new round
		expect(agent.rounds[0].thinking).toBe("Let me searchMore thinking");

		// Now complete the second tool
		usePuxStore.getState().updateAgentRoundToolCall("ag-7", {
			toolCallId: "c2",
			endedAt: 4000,
		});

		// NOW new thinking creates a new round
		usePuxStore.getState().appendAgentRoundThinking("ag-7", "New round thinking");

		const updated = getAgent("ag-7")!;
		expect(updated.rounds.length).toBe(2);
		expect(updated.rounds[0].thinking).toBe("Let me searchMore thinking");
		expect(updated.rounds[1].thinking).toBe("New round thinking");
	});
});

// ────────────────────────────────────────────────────────────────
// Test 3: Full realistic SSE sequence — research → search → scrape
//
// This simulates the EXACT sequence the user reported as broken:
//   ○ research (hollow)
//   ● search (filled)
//   ● scrape (filled)
//
// After fix: ALL three should be filled (●)
// ────────────────────────────────────────────────────────────────

describe("realistic research agent sequence", () => {
	test("research → search → scrape all get filled correctly", () => {
		addRunningAgent("ag-real", "researcher", "Search for Texas news");

		// --- Round 1: research ---
		usePuxStore.getState().appendAgentRoundThinking("ag-real", "I'll use the research tool");

		usePuxStore.getState().appendAgentRoundToolCall("ag-real", {
			toolName: "research",
			toolCallId: "call-research-A",
			args: { max_results: 5, query: "Texas news" },
			timestamp: 1000,
		});

		// research completes
		usePuxStore.getState().updateAgentRoundToolCall("ag-real", {
			toolCallId: "call-research-A",
			endedAt: 2000,
			result: "lots of research content...",
		});

		// PROVE: research has endedAt
		let agent = getAgent("ag-real")!;
		const researchTc = agent.toolCalls.find(tc => tc.toolCallId === "call-research-A")!;
		expect(researchTc.endedAt).toBe(2000);
		expect(researchTc.result).toBe("lots of research content...");

		// --- Round 2: search ---
		usePuxStore.getState().appendAgentRoundThinking("ag-real", "Let me try search instead");

		usePuxStore.getState().appendAgentRoundToolCall("ag-real", {
			toolName: "search",
			toolCallId: "call-search-B",
			args: { query: "Texas news June 2026" },
			timestamp: 3000,
		});

		// search completes
		usePuxStore.getState().updateAgentRoundToolCall("ag-real", {
			toolCallId: "call-search-B",
			endedAt: 4000,
			result: "search results...",
		});

		// PROVE: search has endedAt
		agent = getAgent("ag-real")!;
		const searchTc = agent.toolCalls.find(tc => tc.toolCallId === "call-search-B")!;
		expect(searchTc.endedAt).toBe(4000);

		// --- Round 3: scrape ---
		usePuxStore.getState().appendAgentRoundThinking("ag-real", "Let me scrape the article");

		usePuxStore.getState().appendAgentRoundToolCall("ag-real", {
			toolName: "scrape",
			toolCallId: "call-scrape-C",
			args: { url: "https://example.com/article" },
			timestamp: 5000,
		});

		// scrape completes
		usePuxStore.getState().updateAgentRoundToolCall("ag-real", {
			toolCallId: "call-scrape-C",
			endedAt: 6000,
			result: "full article content...",
		});

		// PROVE: ALL three tools have endedAt set
		agent = getAgent("ag-real")!;
		expect(agent.toolCalls.length).toBe(3);

		for (const tc of agent.toolCalls) {
			expect(tc.endedAt).toBeDefined();
			expect(tc.endedAt).toBeGreaterThan(0);
		}

		// PROVE: Each round has its own thinking
		expect(agent.rounds.length).toBe(3);
		expect(agent.rounds[0].thinking).toBe("I'll use the research tool");
		expect(agent.rounds[1].thinking).toBe("Let me try search instead");
		expect(agent.rounds[2].thinking).toBe("Let me scrape the article");

		// PROVE: Each round has exactly one tool, and it's the right one
		expect(agent.rounds[0].toolCalls[0].toolName).toBe("research");
		expect(agent.rounds[0].toolCalls[0].endedAt).toBe(2000);
		expect(agent.rounds[1].toolCalls[0].toolName).toBe("search");
		expect(agent.rounds[1].toolCalls[0].endedAt).toBe(4000);
		expect(agent.rounds[2].toolCalls[0].toolName).toBe("scrape");
		expect(agent.rounds[2].toolCalls[0].endedAt).toBe(6000);
	});

	test("research stays hollow if toolCallId doesn't match (regression guard)", () => {
		// This test proves the OLD bug: if toolCallId isn't used for matching,
		// research would stay hollow because search starts before research ends.

		addRunningAgent("ag-reg", "researcher", "test");

		// Start research
		usePuxStore.getState().appendAgentRoundToolCall("ag-reg", {
			toolName: "research",
			toolCallId: "call-RESEARCH",
			args: {},
			timestamp: 1000,
		});

		// Start search (research still running!)
		usePuxStore.getState().appendAgentRoundToolCall("ag-reg", {
			toolName: "search",
			toolCallId: "call-SEARCH",
			args: {},
			timestamp: 2000,
		});

		// Research ends — with toolCallId, must match research specifically
		usePuxStore.getState().updateAgentRoundToolCall("ag-reg", {
			toolCallId: "call-RESEARCH",
			endedAt: 3000,
		});

		const agent = getAgent("ag-reg")!;

		// PROVE: research is done, search is still open
		expect(agent.toolCalls[0].toolName).toBe("research");
		expect(agent.toolCalls[0].endedAt).toBe(3000); // FILLED
		expect(agent.toolCalls[1].toolName).toBe("search");
		expect(agent.toolCalls[1].endedAt).toBeUndefined(); // Still running
	});
});

// ────────────────────────────────────────────────────────────────
// Test 4: Subagent text is never truncated in the store
// ────────────────────────────────────────────────────────────────

describe("subagent text integrity", () => {
	test("long text output is preserved in full", () => {
		addRunningAgent("ag-text", "researcher", "test");

		// Simulate a long research report
		const longText = "A".repeat(50000);
		usePuxStore.getState().appendAgentRoundText("ag-text", longText);

		const agent = getAgent("ag-text")!;
		expect(agent.text).toBe(longText);
		expect(agent.text!.length).toBe(50000);
	});

	test("multiple text deltas are concatenated without loss", () => {
		addRunningAgent("ag-text2", "researcher", "test");

		const chunks = [
			"## Research Report\n\n",
			"### Finding 1\nThis is a detailed finding about topic A.\n\n",
			"### Finding 2\nThis is a detailed finding about topic B.\n\n",
			"### Conclusion\nThe research shows interesting results.\n",
		];

		for (const chunk of chunks) {
			usePuxStore.getState().appendAgentRoundText("ag-text2", chunk);
		}

		const agent = getAgent("ag-text2")!;
		const expected = chunks.join("");
		expect(agent.text).toBe(expected);
	});
});
