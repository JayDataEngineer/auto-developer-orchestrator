/**
 * SSE event parser test — validates the chat panel's SSE stream parsing.
 *
 * Tests the core parsing logic extracted from chat-panel.ts handleSSEEvent.
 * This ensures the SSE contract between Go backend and web frontend stays consistent.
 */

import { describe, test, expect } from "bun:test";

// ── Types (mirror chat-panel.ts internal types) ──────────────

interface ChatMessage {
	role: "user" | "assistant";
	text: string;
	thinking?: string;
	tools?: ToolCall[];
}

interface ToolCall {
	name: string;
	args?: string;
	result?: string;
	status: "running" | "done" | "error";
}

// ── Extracted SSE handler (same logic as chat-panel.ts) ──────

function processSSEEvents(events: any[]): ChatMessage {
	const msg: ChatMessage = { role: "assistant", text: "", tools: [] };
	let currentTool: ToolCall | null = null;

	for (const event of events) {
		switch (event.type) {
			case "text_delta":
				msg.text += event.text || event.content || "";
				break;
			case "thinking_delta":
				if (!msg.thinking) msg.thinking = "";
				msg.thinking += event.text || event.content || "";
				break;
			case "tool_execution_start":
				currentTool = {
					name: event.toolName || event.name || "tool",
					args: event.args ? JSON.stringify(event.args).slice(0, 200) : undefined,
					status: "running",
				};
				msg.tools = [...(msg.tools || []), currentTool];
				break;
			case "tool_execution_end":
				if (currentTool) {
					currentTool.status = event.isError ? "error" : "done";
					currentTool = null;
				}
				break;
		}
	}

	return msg;
}

// ── SSE stream parser (same logic as chat-panel.ts send()) ──

function parseSSEStream(rawStream: string): any[] {
	const events: any[] = [];
	let buffer = "";

	const lines = (buffer + rawStream).split("\n");
	buffer = lines.pop() || "";

	for (const line of lines) {
		if (!line.startsWith("data: ")) continue;
		const data = line.slice(6).trim();
		if (data === "[DONE]") continue;
		try {
			events.push(JSON.parse(data));
		} catch {}
	}

	return events;
}

// ── Tests ────────────────────────────────────────────────────

describe("SSE event processing", () => {
	test("text_delta — accumulates text", () => {
		const msg = processSSEEvents([
			{ type: "text_delta", text: "Hello" },
			{ type: "text_delta", text: " world" },
		]);
		expect(msg.text).toBe("Hello world");
	});

	test("text_delta — falls back to content field", () => {
		const msg = processSSEEvents([
			{ type: "text_delta", content: "via content" },
		]);
		expect(msg.text).toBe("via content");
	});

	test("thinking_delta — accumulates thinking", () => {
		const msg = processSSEEvents([
			{ type: "thinking_delta", text: "Let me" },
			{ type: "thinking_delta", text: " think..." },
		]);
		expect(msg.thinking).toBe("Let me think...");
	});

	test("tool_execution_start — creates running tool", () => {
		const msg = processSSEEvents([
			{ type: "tool_execution_start", toolName: "bash", args: { cmd: "ls" } },
		]);
		expect(msg.tools).toHaveLength(1);
		expect(msg.tools![0].name).toBe("bash");
		expect(msg.tools![0].status).toBe("running");
		expect(msg.tools![0].args).toContain("ls");
	});

	test("tool_execution_start — falls back to name field", () => {
		const msg = processSSEEvents([
			{ type: "tool_execution_start", name: "delegate_to" },
		]);
		expect(msg.tools![0].name).toBe("delegate_to");
	});

	test("tool_execution_start — default name is 'tool'", () => {
		const msg = processSSEEvents([
			{ type: "tool_execution_start" },
		]);
		expect(msg.tools![0].name).toBe("tool");
	});

	test("tool_execution_end — marks tool done", () => {
		const msg = processSSEEvents([
			{ type: "tool_execution_start", toolName: "bash" },
			{ type: "tool_execution_end" },
		]);
		expect(msg.tools![0].status).toBe("done");
	});

	test("tool_execution_end — isError marks error", () => {
		const msg = processSSEEvents([
			{ type: "tool_execution_start", toolName: "bash" },
			{ type: "tool_execution_end", isError: true },
		]);
		expect(msg.tools![0].status).toBe("error");
	});

	test("multiple tools — tracks correctly", () => {
		const msg = processSSEEvents([
			{ type: "tool_execution_start", toolName: "bash" },
			{ type: "tool_execution_end" },
			{ type: "tool_execution_start", toolName: "delegate_to" },
			{ type: "tool_execution_end", isError: true },
		]);
		expect(msg.tools).toHaveLength(2);
		expect(msg.tools![0].status).toBe("done");
		expect(msg.tools![1].status).toBe("error");
	});

	test("full flow — text + thinking + tools", () => {
		const msg = processSSEEvents([
			{ type: "thinking_delta", text: "Analyzing..." },
			{ type: "text_delta", text: "I'll check " },
			{ type: "tool_execution_start", toolName: "bash", args: { cmd: "git status" } },
			{ type: "text_delta", text: "the repo." },
			{ type: "tool_execution_end" },
		]);
		expect(msg.text).toBe("I'll check the repo.");
		expect(msg.thinking).toBe("Analyzing...");
		expect(msg.tools).toHaveLength(1);
		expect(msg.tools![0].status).toBe("done");
	});

	test("compaction_end — ignored", () => {
		const msg = processSSEEvents([
			{ type: "text_delta", text: "hi" },
			{ type: "compaction_end", compactionType: "micro" },
			{ type: "text_delta", text: " there" },
		]);
		expect(msg.text).toBe("hi there");
	});
});

describe("SSE stream parser", () => {
	test("parses single data line", () => {
		const events = parseSSEStream('data: {"type":"text_delta","text":"hi"}\n\n');
		expect(events).toHaveLength(1);
		expect(events[0].type).toBe("text_delta");
		expect(events[0].text).toBe("hi");
	});

	test("skips non-data lines", () => {
		const events = parseSSEStream('event: ping\ndata: {"type":"text_delta","text":"hi"}\n\n');
		expect(events).toHaveLength(1);
	});

	test("skips [DONE] sentinel", () => {
		const events = parseSSEStream('data: {"type":"text_delta","text":"hi"}\ndata: [DONE]\n\n');
		expect(events).toHaveLength(1);
	});

	test("handles multiple events", () => {
		const stream = [
			'data: {"type":"text_delta","text":"a"}',
			'',
			'data: {"type":"text_delta","text":"b"}',
			'',
		].join("\n");
		const events = parseSSEStream(stream);
		expect(events).toHaveLength(2);
		expect(events[0].text).toBe("a");
		expect(events[1].text).toBe("b");
	});

	test("handles malformed JSON gracefully", () => {
		const events = parseSSEStream('data: not-json\ndata: {"type":"text_delta","text":"ok"}\n\n');
		// Malformed line is skipped, valid line parsed
		expect(events).toHaveLength(1);
		expect(events[0].text).toBe("ok");
	});

	test("empty stream returns no events", () => {
		const events = parseSSEStream("");
		expect(events).toHaveLength(0);
	});

	test("whitespace-only data line is skipped", () => {
		const events = parseSSEStream("data: \n\n");
		// Empty string after "data: " trimmed → tries JSON.parse("") → fails → skipped
		expect(events).toHaveLength(0);
	});
});
