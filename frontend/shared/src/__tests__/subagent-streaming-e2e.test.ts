/**
 * subagent-streaming-e2e — verify a delegate_to sub-agent streams through
 * the REAL adapter and produces the same event shape as the main
 * orchestrator (thinking → words → tools → words).
 *
 * Contract under test:
 *   1. subagent_start creates an AgentState entry in usePuxStore
 *   2. thinking_delta(agentName) → appendAgentRoundThinking
 *   3. text_delta(agentName) → appendAgentRoundText
 *   4. tool_execution_start(agentName) → appendAgentRoundToolCall
 *   5. tool_execution_end(agentName) → updateAgentRoundToolCall
 *   6. subagent_end → status update, open tools closed
 *   7. Main orchestrator's delegate_to tool-call gets args.__agentId injected
 *
 * Mutation anchors:
 *   - Comment out __agentId injection in subagent_start → __agentId test fails
 *   - Comment out appendAgentRoundToolCall → rounds[0].toolCalls is empty
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { puxChatAdapter } from "../pux-chat-adapter";
import { usePuxStore } from "../pux-store";
import { setFetch } from "../fetch-provider";

// ── Helpers (duplicated from adapter-stream-e2e to keep test self-contained) ──

function streamFromString(sse: string): ReadableStream<Uint8Array> {
	return new ReadableStream({
		start(controller) {
			controller.enqueue(new TextEncoder().encode(sse));
			controller.close();
		},
	});
}

function sse(events: Array<{ event: string; data: unknown }>): string {
	return events
		.map((e) => {
			const data = typeof e.data === "string" ? e.data : JSON.stringify(e.data);
			return `event: ${e.event}\ndata: ${data}\n\n`;
		})
		.join("");
}

function fakeFetchOk(stream: ReadableStream<Uint8Array>) {
	return vi.fn().mockResolvedValue({
		ok: true,
		status: 200,
		body: stream,
		text: () => Promise.resolve(""),
	});
}

function resetStore() {
	usePuxStore.setState({
		activeProject: "test-project",
		activeProjectPath: "/tmp/test-project",
		activeAgentId: "agent-cto",
		activeModel: "test-model",
		agents: new Map(),
		mouseOverlay: null,
		clickTrail: [],
		providerRetry: null,
		thinkingExpanded: true,
		activeWorkbenchTab: "vnc",
	});
}

let originalFetch: typeof fetch;
beforeEach(() => {
	originalFetch = globalThis.fetch;
	resetStore();
});

afterEach(() => {
	globalThis.fetch = originalFetch;
	setFetch(globalThis.fetch.bind(globalThis));
});

async function runAdapter(sseString: string) {
	setFetch(fakeFetchOk(streamFromString(sseString)) as unknown as typeof fetch);
	const snapshots: any[] = [];
	for await (const snapshot of puxChatAdapter.run({
		messages: [{ role: "user", content: "test prompt" }] as any,
		abortSignal: undefined,
	})) {
		snapshots.push(snapshot);
	}
	return snapshots;
}

// ── Test data: complete subagent stream ─────────────────────────
// Mirrors what the Go backend emits for a delegate_to that runs one
// bash tool and responds.
function fullSubagentStream(): string {
	return sse([
		// 1. Main orchestrator decides to delegate
		{ event: "tool_execution_start", data: {
			toolId: "delegate_tc1",
			toolName: "delegate_to",
			args: { role: "browser_ops", task: "click the link" },
		}},
		// 2. Sub-agent starts
		{ event: "subagent_start", data: {
			agentId: "jake_1",
			agentName: "jake",
			task: "click the link",
			transcriptId: "trans_jake_1",
		}},
		// 3. Sub-agent thinking
		{ event: "thinking_delta", data: { agentName: "jake", text: "I need to find the link first." } },
		// 4. Sub-agent text before tool
		{ event: "text_delta", data: { agentName: "jake", text: "Looking for the link." } },
		// 5. Sub-agent calls bash
		{ event: "tool_execution_start", data: {
			agentName: "jake",
			toolId: "jake_tc1",
			toolName: "bash",
			args: { command: "echo clicking" },
		}},
		// 6. Sub-agent tool completes
		{ event: "tool_execution_end", data: {
			agentName: "jake",
			toolId: "jake_tc1",
			result: "clicked",
		}},
		// 7. Sub-agent final text
		{ event: "text_delta", data: { agentName: "jake", text: " Done." } },
		// 8. Sub-agent ends
		{ event: "subagent_end", data: {
			agentId: "jake_1",
			agentName: "jake",
			status: "complete",
			result: "clicked the link",
		}},
		// 9. Main orchestrator's delegate tool completes
		{ event: "tool_execution_end", data: {
			toolId: "delegate_tc1",
			result: "Sub-agent completed: clicked the link",
		}},
		{ event: "text_delta", data: { text: "All done." } },
	]);
}

// ═══════════════════════════════════════════════════════════════
// Tests
// ═══════════════════════════════════════════════════════════════

describe("subagent streaming: AgentState creation", () => {
	it("subagent_start creates a running agent in the store", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "jake_1", agentName: "jake", task: "do thing",
			}},
			{ event: "subagent_end", data: {
				agentId: "jake_1", agentName: "jake", status: "complete",
			}},
		]));

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(1);
		const agent = agents.get("jake_1");
		expect(agent).toBeDefined();
		expect(agent?.agentName).toBe("jake");
		expect(agent?.task).toBe("do thing");
		expect(agent?.status).toBe("complete");
	});

	it("subagent_end with status:error marks agent as error", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "jake_1", agentName: "jake", task: "x",
			}},
			{ event: "subagent_end", data: {
				agentId: "jake_1", agentName: "jake",
				status: "error", error: "tool not found",
			}},
		]));

		const agent = usePuxStore.getState().agents.get("jake_1");
		expect(agent?.status).toBe("error");
		expect(agent?.error).toContain("tool not found");
	});
});

describe("subagent streaming: full think→text→tool→text sequence", () => {
	it("produces rounds[0] with thinking + toolCalls + text in correct shape", async () => {
		await runAdapter(fullSubagentStream());

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(1);
		const agent = agents.get("jake_1")!;

		// One round (single think→tool→text cycle)
		expect(agent.rounds).toHaveLength(1);
		const round = agent.rounds[0];

		// Thinking was captured
		expect(round.thinking).toBe("I need to find the link first.");

		// Tool calls array has the bash call with result + endedAt
		expect(round.toolCalls).toHaveLength(1);
		const tc = round.toolCalls[0];
		expect(tc.toolName).toBe("bash");
		expect(tc.toolCallId).toBe("jake_tc1");
		expect(tc.args).toEqual({ command: "echo clicking" });
		expect(tc.result).toBe("clicked");
		expect(tc.endedAt).toBeDefined();
		expect(tc.isError).not.toBe(true);

		// Both text deltas concatenated into round.text
		expect(round.text).toBe("Looking for the link. Done.");
	});

	it("flat toolCalls array also receives the tool call", async () => {
		await runAdapter(fullSubagentStream());
		const agent = usePuxStore.getState().agents.get("jake_1")!;
		expect(agent.toolCalls).toHaveLength(1);
		expect(agent.toolCalls[0].toolName).toBe("bash");
		expect(agent.toolCalls[0].result).toBe("clicked");
	});

	it("agent.text accumulates all text deltas", async () => {
		await runAdapter(fullSubagentStream());
		const agent = usePuxStore.getState().agents.get("jake_1")!;
		expect(agent.text).toBe("Looking for the link. Done.");
	});

	it("agent.thinkingText accumulates all thinking deltas", async () => {
		await runAdapter(fullSubagentStream());
		const agent = usePuxStore.getState().agents.get("jake_1")!;
		expect(agent.thinkingText).toBe("I need to find the link first.");
	});
});

describe("subagent streaming: main orchestrator parts", () => {
	it("main parts array contains delegate_to tool-call", async () => {
		const snapshots = await runAdapter(fullSubagentStream());
		const last = snapshots[snapshots.length - 1];
		const toolParts = last.content.filter((p: any) => p.type === "tool-call");
		const delegate = toolParts.find((p: any) => p.toolName === "delegate_to");
		expect(delegate).toBeDefined();
		expect(delegate.toolCallId).toBe("delegate_tc1");
		expect(delegate.args.role).toBe("browser_ops");
		expect(delegate.args.task).toBe("click the link");
	});

	it("delegate_to tool-call gets __agentId injected on subagent_start", async () => {
		// Adapter line 847: walks parts looking for a delegate_to without result,
		// injects args.__agentId = agentId from subagent_start.
		const snapshots = await runAdapter(fullSubagentStream());
		const last = snapshots[snapshots.length - 1];
		const delegate = last.content.find(
			(p: any) => p.type === "tool-call" && p.toolName === "delegate_to",
		);
		expect(delegate.args.__agentId).toBe("jake_1");
	});

	it("main orchestrator emits its own text after delegate returns", async () => {
		const snapshots = await runAdapter(fullSubagentStream());
		const last = snapshots[snapshots.length - 1];
		const textParts = last.content.filter((p: any) => p.type === "text");
		// Find the orchestrator's final text (not the sub-agent's — sub-agent
		// text went to the Zustand store, not the parts array)
		expect(textParts).toHaveLength(1);
		expect(textParts[0].text).toBe("All done.");
	});
});

describe("subagent streaming: workbench tab auto-switching", () => {
	it("browser_ops agent switches tab to 'vnc'", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "bops_1", agentName: "browser_ops", task: "x",
			}},
			{ event: "subagent_end", data: {
				agentId: "bops_1", agentName: "browser_ops", status: "complete",
			}},
		]));
		expect(usePuxStore.getState().activeWorkbenchTab).toBe("vnc");
	});

	it("code_ops agent switches tab to 'editor'", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "cops_1", agentName: "code_ops", task: "x",
			}},
			{ event: "subagent_end", data: {
				agentId: "cops_1", agentName: "code_ops", status: "complete",
			}},
		]));
		expect(usePuxStore.getState().activeWorkbenchTab).toBe("editor");
	});

	it("researcher agent does NOT switch tab (no mapping)", async () => {
		// Reset to a known tab first
		usePuxStore.setState({ activeWorkbenchTab: "vnc" });
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "r_1", agentName: "researcher", task: "x",
			}},
			{ event: "subagent_end", data: {
				agentId: "r_1", agentName: "researcher", status: "complete",
			}},
		]));
		// Tab unchanged
		expect(usePuxStore.getState().activeWorkbenchTab).toBe("vnc");
	});
});

describe("subagent streaming: tool execution order", () => {
	it("preserves tool-call order when sub-agent runs multiple tools", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "jake_1", agentName: "jake", task: "x",
			}},
			{ event: "tool_execution_start", data: {
				agentName: "jake", toolId: "tc_a", toolName: "bash", args: { command: "first" },
			}},
			{ event: "tool_execution_end", data: {
				agentName: "jake", toolId: "tc_a", result: "r1",
			}},
			{ event: "text_delta", data: { agentName: "jake", text: "middle text" } },
			{ event: "tool_execution_start", data: {
				agentName: "jake", toolId: "tc_b", toolName: "bash", args: { command: "second" },
			}},
			{ event: "tool_execution_end", data: {
				agentName: "jake", toolId: "tc_b", result: "r2",
			}},
			{ event: "subagent_end", data: {
				agentId: "jake_1", agentName: "jake", status: "complete",
			}},
		]));

		const agent = usePuxStore.getState().agents.get("jake_1")!;
		// Two flat tool calls in order
		expect(agent.toolCalls).toHaveLength(2);
		expect(agent.toolCalls[0].toolCallId).toBe("tc_a");
		expect(agent.toolCalls[1].toolCallId).toBe("tc_b");

		// Rounds: first tool creates round 1, then "all tools completed" + text
		// doesn't start a new round (text just appends to current). Second tool
		// goes into the same round because no thinking_delta triggered a new one.
		// So we have one round with two tool calls.
		expect(agent.rounds.length).toBeGreaterThanOrEqual(1);
		const lastRound = agent.rounds[agent.rounds.length - 1];
		expect(lastRound.toolCalls.length).toBeGreaterThanOrEqual(1);
	});

	it("thinking_delta after a completed tool starts a new round", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "jake_1", agentName: "jake", task: "x",
			}},
			// Round 1: thinking + tool
			{ event: "thinking_delta", data: { agentName: "jake", text: "round 1 thought" } },
			{ event: "tool_execution_start", data: {
				agentName: "jake", toolId: "tc1", toolName: "bash", args: {},
			}},
			{ event: "tool_execution_end", data: {
				agentName: "jake", toolId: "tc1", result: "r1",
			}},
			// Round 2: thinking again — should start new round
			{ event: "thinking_delta", data: { agentName: "jake", text: "round 2 thought" } },
			{ event: "tool_execution_start", data: {
				agentName: "jake", toolId: "tc2", toolName: "bash", args: {},
			}},
			{ event: "tool_execution_end", data: {
				agentName: "jake", toolId: "tc2", result: "r2",
			}},
			{ event: "subagent_end", data: {
				agentId: "jake_1", agentName: "jake", status: "complete",
			}},
		]));

		const agent = usePuxStore.getState().agents.get("jake_1")!;
		expect(agent.rounds).toHaveLength(2);
		expect(agent.rounds[0].thinking).toBe("round 1 thought");
		expect(agent.rounds[0].toolCalls[0].toolCallId).toBe("tc1");
		expect(agent.rounds[1].thinking).toBe("round 2 thought");
		expect(agent.rounds[1].toolCalls[0].toolCallId).toBe("tc2");
	});
});
