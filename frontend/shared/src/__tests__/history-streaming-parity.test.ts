/**
 * history-streaming-parity — the killer test.
 *
 * Goal: prove that a session loaded from history reproduces the SAME agent
 * state that live streaming would have produced for the equivalent input.
 *
 * Method:
 *   1. Drive REAL puxChatAdapter with a scripted SSE stream → snapshot A
 *   2. Reset usePuxStore
 *   3. Build StoredMessage[] that mirrors what the Go backend would have
 *      persisted for that same stream
 *   4. Mock fetch to return that array
 *   5. Call createPuxHistoryAdapter().load()
 *   6. Snapshot B
 *   7. Deep-equal specific fields of A and B
 *
 * Documents EXACTLY which fields survive the round-trip and which don't.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { puxChatAdapter } from "../pux-chat-adapter";
import { createPuxHistoryAdapter } from "../pux-history-adapter";
import { usePuxStore } from "../pux-store";
import { setFetch, getFetch } from "../fetch-provider";

// ── SSE helpers ──────────────────────────────────────────────────

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

function fakeFetchJson(body: any) {
	return vi.fn().mockImplementation((url: string) => {
		// History endpoint returns array
		if (typeof url === "string" && url.includes("/api/pux/history")) {
			return Promise.resolve({
				ok: true,
				status: 200,
				json: async () => body,
			});
		}
		// Other endpoints (providers, defaults, etc.) return empty defaults
		return Promise.resolve({
			ok: true,
			status: 200,
			json: async () => (Array.isArray(body) ? [] : {}),
		});
	});
}

// ── Stream that drives the live adapter ──────────────────────────
// Mirrors what the Go backend emits for a delegate_to that runs one
// bash tool and responds.
function fullSubagentStream(): string {
	return sse([
		{ event: "tool_execution_start", data: {
			toolId: "delegate_tc1",
			toolName: "delegate_to",
			args: { role: "browser_ops", task: "click the link" },
		}},
		{ event: "subagent_start", data: {
			agentId: "jake_1",
			agentName: "jake",
			task: "click the link",
		}},
		{ event: "thinking_delta", data: { agentName: "jake", text: "I need to find the link first." } },
		{ event: "text_delta", data: { agentName: "jake", text: "Looking for the link." } },
		{ event: "tool_execution_start", data: {
			agentName: "jake", toolId: "jake_tc1", toolName: "bash",
			args: { command: "echo clicking" },
		}},
		{ event: "tool_execution_end", data: {
			agentName: "jake", toolId: "jake_tc1", result: "clicked",
		}},
		{ event: "text_delta", data: { agentName: "jake", text: " Done." } },
		{ event: "subagent_end", data: {
			agentId: "jake_1", agentName: "jake", status: "complete",
			result: "clicked the link",
		}},
		{ event: "tool_execution_end", data: {
			toolId: "delegate_tc1", result: "Sub-agent completed: clicked the link",
		}},
		{ event: "text_delta", data: { text: "All done." } },
	]);
}

// ── StoredMessage array mirroring the same stream ────────────────
// This is what the Go backend's SQLite/Postgres would persist:
//   - 1 user message
//   - 1 assistant message with toolCalls JSON containing delegate_to + subAgent
//   - 1 tool-role message for the delegate's tool result
function buildStoredHistory(): any[] {
	return [
		// 1. User prompt
		{
			id: 1,
			project: "test-project",
			agentId: "agent-cto",
			role: "user",
			content: "test prompt",
			text: "",
			thinking: "",
			toolCalls: "[]",
			toolCallId: "",
			toolName: "",
			createdAt: "2026-06-17T12:00:00Z",
		},
		// 2. Assistant message carrying the delegate_to + subAgent trace
		{
			id: 2,
			project: "test-project",
			agentId: "agent-cto",
			role: "assistant",
			content: "",
			text: "All done.",  // main orchestrator's text
			thinking: "",       // no thinking on main orchestrator in this stream
			toolCalls: JSON.stringify([{
				id: "delegate_tc1",
				name: "delegate_to",
				args: { role: "browser_ops", task: "click the link" },
				subAgent: {
					name: "jake",
					status: "complete",
					toolCalls: [{
						id: "jake_tc1",
						name: "bash",
						args: { command: "echo clicking" },
						result: "clicked",
					}],
					thinking: "I need to find the link first.",
					text: "Looking for the link. Done.",
					result: "clicked the link",
				},
			}]),
			toolCallId: "",
			toolName: "",
			createdAt: "2026-06-17T12:00:01Z",
		},
		// 3. Tool-role message for delegate's result
		{
			id: 3,
			project: "test-project",
			agentId: "agent-cto",
			role: "tool",
			content: "Sub-agent completed: clicked the link",
			text: "",
			thinking: "",
			toolCalls: "[]",
			toolCallId: "delegate_tc1",
			toolName: "delegate_to",
			createdAt: "2026-06-17T12:00:02Z",
		},
	];
}

// ── Test setup ───────────────────────────────────────────────────

let originalFetch: typeof fetch;

beforeEach(() => {
	originalFetch = globalThis.fetch;
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

async function loadHistory(stored: any[]) {
	setFetch(fakeFetchJson(stored) as unknown as typeof fetch);
	const adapter = createPuxHistoryAdapter();
	return await adapter.load();
}

// ═══════════════════════════════════════════════════════════════
// Parity tests
// ═══════════════════════════════════════════════════════════════

describe("history-streaming parity: AgentState fields", () => {
	it("sub-agent agentName matches across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;
		expect(live.agentName).toBe("jake");

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.agentName).toBe(live.agentName);
	});

	it("sub-agent status matches across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.status).toBe(live.status);
	});

	it("rounds[0].thinking matches across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.rounds[0].thinking).toBe(live.rounds[0].thinking);
	});

	it("rounds[0].text matches across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.rounds[0].text).toBe(live.rounds[0].text);
	});

	it("rounds[0].toolCalls[0].toolName matches across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.rounds[0].toolCalls[0].toolName)
			.toBe(live.rounds[0].toolCalls[0].toolName);
	});

	it("rounds[0].toolCalls[0].result matches across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.rounds[0].toolCalls[0].result)
			.toBe(live.rounds[0].toolCalls[0].result);
	});

	it("rounds[0].toolCalls[0].args match across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.rounds[0].toolCalls[0].args)
			.toEqual(live.rounds[0].toolCalls[0].args);
	});

	it("agent.task matches across round-trip", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.task).toBe(live.task);
	});
});

describe("history-streaming parity: documented differences", () => {
	// These tests assert fields that DIFFER between live and restored state.
	// They are NOT bugs — they are intentional differences in how
	// streaming vs. persistence represent timestamps and identifiers.

	it("agentId differs: streaming uses 'jake_1', history uses 'hist_delegate_tc1'", async () => {
		await runAdapter(fullSubagentStream());
		const liveId = "jake_1";

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restoredId = "hist_delegate_tc1";
		expect(usePuxStore.getState().agents.has(liveId)).toBe(false);
		expect(usePuxStore.getState().agents.has(restoredId)).toBe(true);
	});

	it("both agentIds are valid keys present in their respective stores", async () => {
		await runAdapter(fullSubagentStream());
		expect(usePuxStore.getState().agents.has("jake_1")).toBe(true);

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		expect(usePuxStore.getState().agents.has("hist_delegate_tc1")).toBe(true);
	});

	it("number of rounds matches (1)", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;
		expect(live.rounds).toHaveLength(1);

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.rounds).toHaveLength(live.rounds.length);
	});

	it("number of tool calls in rounds[0] matches (1)", async () => {
		await runAdapter(fullSubagentStream());
		const live = usePuxStore.getState().agents.get("jake_1")!;

		usePuxStore.setState({ agents: new Map() });
		await loadHistory(buildStoredHistory());

		const restored = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(restored.rounds[0].toolCalls.length)
			.toBe(live.rounds[0].toolCalls.length);
	});
});

describe("history-streaming parity: top-level assistant message", () => {
	// Verifies the main orchestrator's assistant message — its text and
	// its delegate_to tool-call — also survives the round-trip.
	//
	// We bypass the ExportedMessageRepository wrapper and call
	// storedMessagesToThreadLikes directly on the same StoredMessage[]
	// that the adapter.load() path consumes internally. That gives us
	// the exact ThreadLike[] the repository would have wrapped.

	it("main orchestrator's text 'All done.' is preserved in ThreadLikes", async () => {
		const snapshots = await runAdapter(fullSubagentStream());
		const lastLive = snapshots[snapshots.length - 1];
		const liveText = lastLive.content.find((p: any) => p.type === "text");
		expect(liveText?.text).toBe("All done.");

		// Now check the same field survives storedMessagesToThreadLikes
		const { storedMessagesToThreadLikes } = await import("../pux-history-adapter");
		const threads = storedMessagesToThreadLikes(buildStoredHistory());
		const assistantThreads = threads.filter((t: any) => t.role === "assistant");

		// Flatten all text parts from all assistant messages
		const assistantTexts = assistantThreads
			.flatMap((t: any) => Array.isArray(t.content) ? t.content : [])
			.filter((p: any) => p.type === "text")
			.map((p: any) => p.text);

		expect(assistantTexts).toContain("All done.");
	});

	it("main orchestrator's delegate_to tool-call is preserved in ThreadLikes", async () => {
		const { storedMessagesToThreadLikes } = await import("../pux-history-adapter");
		const threads = storedMessagesToThreadLikes(buildStoredHistory());
		const assistantThreads = threads.filter((t: any) => t.role === "assistant");

		const toolCalls = assistantThreads
			.flatMap((t: any) => Array.isArray(t.content) ? t.content : [])
			.filter((p: any) => p.type === "tool-call");

		const delegate = toolCalls.find(
			(p: any) => p.toolName === "delegate_to" && p.toolCallId === "delegate_tc1",
		);
		expect(delegate).toBeDefined();
		expect(delegate.args.role).toBe("browser_ops");
		expect(delegate.args.task).toBe("click the link");
		// Result attached from tool-role message
		expect(delegate.result).toBe("Sub-agent completed: clicked the link");
	});

	it("main orchestrator's delegate_to carries __subAgent in args", async () => {
		const { storedMessagesToThreadLikes } = await import("../pux-history-adapter");
		const threads = storedMessagesToThreadLikes(buildStoredHistory());
		const assistantThreads = threads.filter((t: any) => t.role === "assistant");

		const delegate = assistantThreads
			.flatMap((t: any) => Array.isArray(t.content) ? t.content : [])
			.find((p: any) => p.toolName === "delegate_to");

		expect(delegate.args.__subAgent).toBeDefined();
		expect(delegate.args.__subAgent.name).toBe("jake");
		expect(delegate.args.__subAgent.status).toBe("complete");
	});

	it("hist_delegate_tc1 agent exists in store after load", async () => {
		await loadHistory(buildStoredHistory());
		const agent = usePuxStore.getState().agents.get("hist_delegate_tc1")!;
		expect(agent).toBeDefined();
		expect(agent.agentName).toBe("jake");
		expect(agent.task).toBe("click the link");
	});
});
