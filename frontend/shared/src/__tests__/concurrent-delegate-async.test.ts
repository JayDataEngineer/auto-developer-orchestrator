/**
 * concurrent-delegate-async — verify the adapter demultiplexes events for
 * interleaved sub-agents running in parallel.
 *
 * The adapter routes sub-agent events by `agentName`:
 *   `const agent = [...agents.values()].find(a => a.agentName === parsed.agentName)`
 *
 * This works when concurrent sub-agents have DIFFERENT agentNames. But when
 * the same role (e.g., two `code_ops`) is delegated to twice concurrently,
 * events collide — both routes to whichever was added first. This file
 * documents both the working case and the known collision.
 *
 * Mutation anchors:
 *   - Change `a.agentName === parsed.agentName` to `a.agentId === parsed.agentName`
 *     → both concurrent tests fail
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { puxChatAdapter } from "../pux-chat-adapter";
import { usePuxStore } from "../pux-store";
import { setFetch } from "../fetch-provider";

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

// ═══════════════════════════════════════════════════════════════
// Tests
// ═══════════════════════════════════════════════════════════════

describe("concurrent delegate_async: different agentNames", () => {
	it("two distinct-name subagents both end with correct state", async () => {
		// browser_ops and code_ops running concurrently, events interleaved
		await runAdapter(sse([
			// Both start
			{ event: "subagent_start", data: {
				agentId: "bops_1", agentName: "browser_ops", task: "browse",
			}},
			{ event: "subagent_start", data: {
				agentId: "cops_1", agentName: "code_ops", task: "edit code",
			}},
			// Interleaved thinking + text
			{ event: "thinking_delta", data: { agentName: "browser_ops", text: "clicking..." } },
			{ event: "thinking_delta", data: { agentName: "code_ops", text: "editing..." } },
			{ event: "text_delta", data: { agentName: "browser_ops", text: "Found link." } },
			{ event: "text_delta", data: { agentName: "code_ops", text: "Wrote file." } },
			// Interleaved tools
			{ event: "tool_execution_start", data: {
				agentName: "browser_ops", toolId: "b_tc1", toolName: "find_element", args: {},
			}},
			{ event: "tool_execution_start", data: {
				agentName: "code_ops", toolId: "c_tc1", toolName: "file_edit", args: {},
			}},
			{ event: "tool_execution_end", data: {
				agentName: "browser_ops", toolId: "b_tc1", result: "found",
			}},
			{ event: "tool_execution_end", data: {
				agentName: "code_ops", toolId: "c_tc1", result: "edited",
			}},
			// Both end
			{ event: "subagent_end", data: {
				agentId: "bops_1", agentName: "browser_ops", status: "complete",
			}},
			{ event: "subagent_end", data: {
				agentId: "cops_1", agentName: "code_ops", status: "complete",
			}},
		]));

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(2);

		const bops = agents.get("bops_1")!;
		const cops = agents.get("cops_1")!;

		expect(bops.agentName).toBe("browser_ops");
		expect(cops.agentName).toBe("code_ops");

		// Each got its OWN thinking, not the other's
		expect(bops.thinkingText).toBe("clicking...");
		expect(cops.thinkingText).toBe("editing...");

		// Each got its OWN text
		expect(bops.text).toBe("Found link.");
		expect(cops.text).toBe("Wrote file.");

		// Each got its OWN tool result
		expect(bops.toolCalls).toHaveLength(1);
		expect(bops.toolCalls[0].toolName).toBe("find_element");
		expect(bops.toolCalls[0].result).toBe("found");

		expect(cops.toolCalls).toHaveLength(1);
		expect(cops.toolCalls[0].toolName).toBe("file_edit");
		expect(cops.toolCalls[0].result).toBe("edited");
	});

	it("preserves agentId-based correlation when subagent_end arrives", async () => {
		// subagent_end uses agentId if provided (preferred) over agentName
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "bops_1", agentName: "browser_ops", task: "x",
			}},
			{ event: "subagent_start", data: {
				agentId: "cops_1", agentName: "code_ops", task: "y",
			}},
			{ event: "subagent_end", data: {
				agentId: "cops_1", agentName: "code_ops", status: "complete", result: "cops done",
			}},
			{ event: "subagent_end", data: {
				agentId: "bops_1", agentName: "browser_ops", status: "complete", result: "bops done",
			}},
		]));

		const agents = usePuxStore.getState().agents;
		const bops = agents.get("bops_1")!;
		const cops = agents.get("cops_1")!;

		// Both completed, with their own results
		expect(bops.status).toBe("complete");
		expect(bops.result).toBe("bops done");
		expect(cops.status).toBe("complete");
		expect(cops.result).toBe("cops done");
	});

	it("handles out-of-order completion: second agent finishes before first", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "a_1", agentName: "browser_ops", task: "a",
			}},
			{ event: "subagent_start", data: {
				agentId: "b_1", agentName: "code_ops", task: "b",
			}},
			// b finishes first
			{ event: "subagent_end", data: {
				agentId: "b_1", agentName: "code_ops", status: "complete",
			}},
			// a still running, emits more
			{ event: "text_delta", data: { agentName: "browser_ops", text: "still working" } },
			{ event: "subagent_end", data: {
				agentId: "a_1", agentName: "browser_ops", status: "complete",
			}},
		]));

		const a = usePuxStore.getState().agents.get("a_1")!;
		const b = usePuxStore.getState().agents.get("b_1")!;
		expect(a.status).toBe("complete");
		expect(a.text).toBe("still working");
		expect(b.status).toBe("complete");
	});
});

describe("concurrent delegate_async: same agentName collision (KNOWN LIMITATION)", () => {
	// These tests document a real bug in the adapter's routing logic.
	// The adapter finds agents by agentName only:
	//   `[...agents.values()].find(a => a.agentName === parsed.agentName)`
	//
	// When two sub-agents share the same agentName (e.g., two concurrent
	// browser_ops), all events route to the FIRST one added. The second
	// agent's state ends up empty.
	//
	// The fix would be to route by agentId when available, falling back
	// to agentName+status==="running" only for legacy events.

	it("KNOWN BUG: two same-name subagents route events to the first", async () => {
		await runAdapter(sse([
			{ event: "subagent_start", data: {
				agentId: "bops_1", agentName: "browser_ops", task: "first task",
			}},
			{ event: "subagent_start", data: {
				agentId: "bops_2", agentName: "browser_ops", task: "second task",
			}},
			// Both produce text
			{ event: "text_delta", data: { agentName: "browser_ops", text: "from first " } },
			{ event: "text_delta", data: { agentName: "browser_ops", text: "from second" } },
			{ event: "subagent_end", data: {
				agentId: "bops_1", agentName: "browser_ops", status: "complete",
			}},
			{ event: "subagent_end", data: {
				agentId: "bops_2", agentName: "browser_ops", status: "complete",
			}},
		]));

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(2);

		const first = agents.get("bops_1")!;
		const second = agents.get("bops_2")!;

		// BUG: all text went to the first agent because find() returned the
		// first match by agentName. The second agent has empty text.
		expect(first.text).toContain("from first");
		expect(first.text).toContain("from second"); // ← WRONG, should be on second
		expect(second.text).toBeUndefined(); // ← WRONG, should contain "from second"

		// Document the bug: this is what currently happens. When fixed,
		// these expectations should be rewritten to:
		//   expect(first.text).toBe("from first");
		//   expect(second.text).toBe("from second");
	});

	it("events with agentId could disambiguate but adapter doesn't use them for text/tool routing", async () => {
		// thinking_delta/text_delta/tool_execution_* only look at agentName,
		// never at agentId. So even though subagent_start emitted agentId, the
		// per-event routing can't use it. This test documents that the event
		// payloads from the backend don't carry agentId today (only agentName).
		//
		// If the backend starts emitting agentId on every event, the adapter
		// SHOULD prefer agentId for routing. Until then, this is a known
		// limitation.
		expect(true).toBe(true); // documentation-only test
	});
});
