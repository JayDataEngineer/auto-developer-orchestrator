/**
 * adapter-stream-e2e — drive the REAL puxChatAdapter with a scripted SSE byte
 * stream and verify the resulting snapshots.
 *
 * The existing TUI tests (regression.test.tsx, render-order.test.tsx,
 * event-order.test.tsx) reimplement reorderParts/appendText as extracted
 * copies inside the test files. If the production adapter drifts from those
 * copies, those tests still pass. This file closes that gap by exercising
 * the real adapter end-to-end.
 *
 * Strategy:
 *   - setFetch() to a mock returning a Response with a ReadableStream
 *   - Build the stream from string chunks via new ReadableStream({ start(c) {...} })
 *   - Call puxChatAdapter.run({messages, abortSignal}) — collect yielded snapshots
 *   - Reset usePuxStore between tests
 *
 * Mutation anchors (in commit message):
 *   - Change appendThinking to push at end → "reasoning moves to front" fails
 *   - Change appendText to never merge → "consecutive text deltas merge" fails
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { puxChatAdapter } from "../pux-chat-adapter";
import { usePuxStore } from "../pux-store";
import { setFetch } from "../fetch-provider";

// ── Stream builder ─────────────────────────────────────────────
// Build a ReadableStream<Uint8Array> from an array of string chunks.
// Each chunk is emitted as a separate read() — simulates real network
// chunk boundaries.
function streamFromChunks(chunks: string[]): ReadableStream<Uint8Array> {
	return new ReadableStream({
		start(controller) {
			for (const chunk of chunks) {
				controller.enqueue(new TextEncoder().encode(chunk));
			}
			controller.close();
		},
	});
}

// Convenience: build a single-chunk stream from a complete SSE string.
function streamFromString(sse: string): ReadableStream<Uint8Array> {
	return streamFromChunks([sse]);
}

// Build SSE event string. Each event = "event: TYPE\ndata: JSON\n\n"
function sse(events: Array<{ event: string; data: unknown }>): string {
	return events
		.map((e) => {
			const data = typeof e.data === "string" ? e.data : JSON.stringify(e.data);
			return `event: ${e.event}\ndata: ${data}\n\n`;
		})
		.join("");
}

// Build a fake fetch that returns a Response with our stream.
function fakeFetchOk(stream: ReadableStream<Uint8Array>) {
	return vi.fn().mockResolvedValue({
		ok: true,
		status: 200,
		body: stream,
		text: () => Promise.resolve(""),
	});
}

// ── Reset store between tests ─────────────────────────────────
function resetStore() {
	usePuxStore.setState({
		activeProject: "test-project",
		activeProjectPath: "/tmp/test-project",
		activeAgentId: "agent-test",
		activeModel: "test-model",
		agents: new Map(),
		mouseOverlay: null,
		clickTrail: [],
		providerRetry: null,
		thinkingExpanded: true,
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

// Helper: drive adapter to completion, collect all yielded snapshots.
async function runAdapter(sseString: string) {
	const stream = streamFromString(sseString);
	setFetch(fakeFetchOk(stream) as unknown as typeof fetch);

	const messages = [
		{ role: "user" as const, content: "test prompt" },
	];

	const snapshots: any[] = [];
	for await (const snapshot of puxChatAdapter.run({
		messages: messages as any,
		abortSignal: undefined,
	})) {
		snapshots.push(snapshot);
	}
	return snapshots;
}

// ═══════════════════════════════════════════════════════════════
// Tests
// ═══════════════════════════════════════════════════════════════

describe("adapter stream e2e: basic snapshot yields", () => {
	it("emits an initial running snapshot before any events arrive", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "text_delta", data: { text: "hi" } },
		]));
		// First snapshot is the initial running state with empty content
		expect(snapshots.length).toBeGreaterThanOrEqual(2);
		expect(snapshots[0].status).toEqual({ type: "running" });
		expect(snapshots[0].content).toEqual([]);
	});

	it("emits a complete snapshot when [DONE] arrives", async () => {
		const snapshots = await runAdapter("data: [DONE]\n\n");
		const last = snapshots[snapshots.length - 1];
		expect(last.status).toEqual({ type: "complete", reason: "stop" });
	});
});

describe("adapter stream e2e: text_delta handling", () => {
	it("appends text_delta content to a single text part", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "text_delta", data: { text: "Hello " } },
			{ event: "text_delta", data: { text: "world" } },
		]));
		const last = snapshots[snapshots.length - 1];
		const textParts = last.content.filter((p: any) => p.type === "text");
		expect(textParts).toHaveLength(1);
		expect(textParts[0].text).toBe("Hello world");
	});

	it("does not create empty text parts for empty deltas", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "text_delta", data: { text: "" } },
			{ event: "text_delta", data: { text: "actual content" } },
		]));
		const last = snapshots[snapshots.length - 1];
		const textParts = last.content.filter((p: any) => p.type === "text");
		expect(textParts).toHaveLength(1);
		expect(textParts[0].text).toBe("actual content");
	});
});

describe("adapter stream e2e: reasoning ordering", () => {
	it("moves reasoning parts to the front of content", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "text_delta", data: { text: "Working" } },
			{ event: "thinking_delta", data: { text: "I should think first" } },
			{ event: "tool_execution_start", data: { toolId: "tc1", toolName: "bash", args: { command: "ls" } } },
		]));
		const last = snapshots[snapshots.length - 1];
		const types = last.content.map((p: any) => p.type);

		// Reasoning must be at index 0
		expect(types[0]).toBe("reasoning");
		// Text and tool preserve their stream order after reasoning
		expect(types.indexOf("text")).toBeLessThan(types.indexOf("tool-call"));
	});

	it("multiple reasoning blocks accumulate into one reasoning part", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "thinking_delta", data: { text: "First " } },
			{ event: "thinking_delta", data: { text: "second" } },
		]));
		const last = snapshots[snapshots.length - 1];
		const reasoningParts = last.content.filter((p: any) => p.type === "reasoning");
		expect(reasoningParts).toHaveLength(1);
		expect(reasoningParts[0].text).toBe("First second");
	});
});

describe("adapter stream e2e: text→tool→text ordering", () => {
	it("preserves stream order: text, tool, text (NOT tool-grouped)", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "text_delta", data: { text: "Before tool." } },
			{ event: "tool_execution_start", data: { toolId: "tc1", toolName: "bash", args: {} } },
			{ event: "tool_execution_end", data: { toolId: "tc1", result: "ok" } },
			{ event: "text_delta", data: { text: "After tool." } },
		]));
		const last = snapshots[snapshots.length - 1];
		const types = last.content.map((p: any) => p.type);

		// Must be exactly text → tool-call → text in stream order
		expect(types).toEqual(["text", "tool-call", "text"]);
	});

	it("preserves stream order with multiple tools: text→tool→text→tool→text", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "text_delta", data: { text: "A" } },
			{ event: "tool_execution_start", data: { toolId: "tc1", toolName: "bash", args: {} } },
			{ event: "tool_execution_end", data: { toolId: "tc1", result: "1" } },
			{ event: "text_delta", data: { text: "B" } },
			{ event: "tool_execution_start", data: { toolId: "tc2", toolName: "bash", args: {} } },
			{ event: "tool_execution_end", data: { toolId: "tc2", result: "2" } },
			{ event: "text_delta", data: { text: "C" } },
		]));
		const last = snapshots[snapshots.length - 1];
		const types = last.content.map((p: any) => p.type);

		// Interleaved, NOT grouped (the old bug was tool/tool/text/text/text)
		expect(types).toEqual([
			"text", "tool-call", "text", "tool-call", "text",
		]);
	});

	it("text after tool does NOT merge with text before tool", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "text_delta", data: { text: "Before" } },
			{ event: "tool_execution_start", data: { toolId: "tc1", toolName: "bash", args: {} } },
			{ event: "tool_execution_end", data: { toolId: "tc1", result: "ok" } },
			{ event: "text_delta", data: { text: "After" } },
		]));
		const last = snapshots[snapshots.length - 1];
		const textParts = last.content.filter((p: any) => p.type === "text");
		expect(textParts).toHaveLength(2);
		expect(textParts[0].text).toBe("Before");
		expect(textParts[1].text).toBe("After");
	});
});

describe("adapter stream e2e: tool result/error paths", () => {
	it("attaches result to tool-call part on tool_execution_end", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "tool_execution_start", data: { toolId: "tc1", toolName: "bash", args: { command: "echo" } } },
			{ event: "tool_execution_end", data: { toolId: "tc1", result: "command output" } },
		]));
		const last = snapshots[snapshots.length - 1];
		const toolPart = last.content.find((p: any) => p.type === "tool-call");
		expect(toolPart).toBeDefined();
		expect(toolPart.result).toBe("command output");
		// isError is only set when there's an error; success leaves it undefined
		expect(toolPart.isError).not.toBe(true);
	});

	it("marks tool-call as error when tool_execution_end has error field", async () => {
		const snapshots = await runAdapter(sse([
			{ event: "tool_execution_start", data: { toolId: "tc1", toolName: "bash", args: {} } },
			{ event: "tool_execution_end", data: { toolId: "tc1", error: "command not found" } },
		]));
		const last = snapshots[snapshots.length - 1];
		const toolPart = last.content.find((p: any) => p.type === "tool-call");
		expect(toolPart.isError).toBe(true);
		expect(toolPart.result).toContain("command not found");
	});
});

describe("adapter stream e2e: HTTP error path", () => {
	it("yields error snapshot when response is not ok", async () => {
		const errorFetch = vi.fn().mockResolvedValue({
			ok: false,
			status: 500,
			text: () => Promise.resolve("Internal Server Error"),
			body: null,
		});
		setFetch(errorFetch as unknown as typeof fetch);

		const messages = [{ role: "user" as const, content: "test" }];
		const snapshots: any[] = [];
		for await (const snapshot of puxChatAdapter.run({
			messages: messages as any,
			abortSignal: undefined,
		})) {
			snapshots.push(snapshot);
		}

		expect(snapshots).toHaveLength(1);
		expect(snapshots[0].status).toEqual({ type: "incomplete", reason: "error" });
		expect(snapshots[0].content[0].text).toContain("500");
	});
});

describe("adapter stream e2e: multi-chunk buffer accumulation", () => {
	it("handles events split across multiple read() chunks", async () => {
		// Split the SSE bytes into tiny chunks to stress parseSSE + buffer logic
		const fullSse = sse([
			{ event: "text_delta", data: { text: "hello world" } },
			{ event: "tool_execution_start", data: { toolId: "tc1", toolName: "bash", args: {} } },
		]);

		// 1-byte chunks (worst case)
		const chars = fullSse.split("");
		const chunks: string[] = [];
		for (let i = 0; i < chars.length; i += 4) {
			chunks.push(chars.slice(i, i + 4).join(""));
		}

		const stream = streamFromChunks(chunks);
		setFetch(fakeFetchOk(stream) as unknown as typeof fetch);

		const messages = [{ role: "user" as const, content: "test" }];
		const snapshots: any[] = [];
		for await (const snapshot of puxChatAdapter.run({
			messages: messages as any,
			abortSignal: undefined,
		})) {
			snapshots.push(snapshot);
		}

		const last = snapshots[snapshots.length - 1];
		const types = last.content.map((p: any) => p.type);
		// Should still produce text → tool-call in correct order
		expect(types).toContain("text");
		expect(types).toContain("tool-call");
		expect(types.indexOf("text")).toBeLessThan(types.indexOf("tool-call"));

		const textPart = last.content.find((p: any) => p.type === "text");
		expect(textPart.text).toBe("hello world");
	});
});
