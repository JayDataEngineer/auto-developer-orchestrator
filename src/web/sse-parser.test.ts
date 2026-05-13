/**
 * ChatState tests — shared message accumulator used by both TUI and web.
 *
 * Tests the event→message model conversion that PuxAgentSession emits.
 * Both TUI InteractiveMode and web chat-panel consume these same events.
 */

import { describe, test, expect } from "vitest";
import { ChatState } from "../../ts-tui-pi/src/core/chat-state.js";

function emit(state: ChatState, event: any): void {
	state.handleEvent(event);
}

function userStart(text: string): any {
	return { type: "message_start", message: { role: "user", content: [{ type: "text", text }] } };
}

function assistantStart(): any {
	return { type: "message_start", message: { role: "assistant", content: [] } };
}

function messageUpdate(content: any[]): any {
	return { type: "message_update", message: { role: "assistant", content } };
}

function messageEnd(content: any[], extra?: Record<string, any>): any {
	return { type: "message_end", message: { role: "assistant", content, stopReason: "stop", ...extra } };
}

function toolExecStart(id: string, name: string, args?: any): any {
	return { type: "tool_execution_start", toolCallId: id, toolName: name, args };
}

function toolExecEnd(id: string, isError?: boolean, result?: any): any {
	return { type: "tool_execution_end", toolCallId: id, isError: !!isError, result };
}

function agentEnd(): any {
	return { type: "agent_end", messages: [] };
}

// ── Tests ────────────────────────────────────────────────────

describe("ChatState (shared TUI + web accumulator)", () => {
	test("user message added to messages", () => {
		const s = new ChatState();
		emit(s, userStart("hello"));
		emit(s, agentEnd());
		expect(s.messages).toHaveLength(1);
		expect(s.messages[0].role).toBe("user");
		expect(s.messages[0].text).toBe("hello");
	});

	test("assistant start creates streaming message", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		expect(s.messages).toHaveLength(1);
		expect(s.messages[0].role).toBe("assistant");
		expect(s.streaming).toBe(true);
	});

	test("message_update accumulates text", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageUpdate([
			{ type: "text", text: "Hello world" },
		]));
		expect(s.messages[0].text).toBe("Hello world");
	});

	test("message_update accumulates thinking", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageUpdate([
			{ type: "thinking", thinking: "Let me analyze..." },
		]));
		expect(s.messages[0].thinking).toBe("Let me analyze...");
	});

	test("message_update tracks tool calls", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageUpdate([
			{ type: "toolCall", id: "t1", name: "bash", arguments: { cmd: "ls" } },
		]));
		expect(s.messages[0].tools).toHaveLength(1);
		expect(s.messages[0].tools[0].name).toBe("bash");
		expect(s.messages[0].tools[0].status).toBe("running");
	});

	test("tool_execution_end marks tool done", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageUpdate([
			{ type: "toolCall", id: "t1", name: "bash", arguments: {} },
		]));
		emit(s, toolExecEnd("t1"));
		expect(s.messages[0].tools[0].status).toBe("done");
	});

	test("tool_execution_end marks tool error", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageUpdate([
			{ type: "toolCall", id: "t1", name: "bash", arguments: {} },
		]));
		emit(s, toolExecEnd("t1", true));
		expect(s.messages[0].tools[0].status).toBe("error");
	});

	test("tool_execution_start adds sub-agent tool", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, toolExecStart("t2", "delegate_to", { agent: "jake" }));
		expect(s.messages[0].tools).toHaveLength(1);
		expect(s.messages[0].tools[0].name).toBe("delegate_to");
	});

	test("message_end finalizes text", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageUpdate([{ type: "text", text: "partial" }]));
		emit(s, messageEnd([{ type: "text", text: "final answer" }]));
		expect(s.messages[0].text).toBe("final answer");
	});

	test("message_end captures error", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageEnd([{ type: "text", text: "" }], { stopReason: "error", errorMessage: "Backend 500" }));
		expect(s.messages[0].errorMessage).toBe("Backend 500");
	});

	test("agent_end sets streaming false", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		expect(s.streaming).toBe(true);
		emit(s, messageEnd([{ type: "text", text: "done" }]));
		emit(s, agentEnd());
		expect(s.streaming).toBe(false);
	});

	test("full conversation flow", () => {
		const s = new ChatState();

		// User sends message
		emit(s, userStart("list files"));
		// Assistant starts
		emit(s, assistantStart());
		// Tool call
		emit(s, messageUpdate([
			{ type: "toolCall", id: "t1", name: "bash", arguments: { cmd: "ls" } },
		]));
		// Tool result
		emit(s, toolExecEnd("t1", false, { content: [{ type: "text", text: "file1.txt" }] }));
		// Final answer
		emit(s, messageEnd([{ type: "text", text: "Here are the files: file1.txt" }]));
		emit(s, agentEnd());

		expect(s.messages).toHaveLength(2);
		expect(s.messages[0].role).toBe("user");
		expect(s.messages[0].text).toBe("list files");
		expect(s.messages[1].role).toBe("assistant");
		expect(s.messages[1].text).toBe("Here are the files: file1.txt");
		expect(s.messages[1].tools).toHaveLength(1);
		expect(s.messages[1].tools[0].status).toBe("done");
		expect(s.streaming).toBe(false);
	});

	test("multiple tool calls in sequence", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		emit(s, messageUpdate([{ type: "toolCall", id: "t1", name: "bash", arguments: { cmd: "ls" } }]));
		emit(s, messageUpdate([
			{ type: "toolCall", id: "t1", name: "bash", arguments: { cmd: "ls" } },
			{ type: "toolCall", id: "t2", name: "bash", arguments: { cmd: "pwd" } },
		]));
		emit(s, toolExecEnd("t1"));
		emit(s, toolExecEnd("t2"));
		emit(s, messageEnd([]));
		emit(s, agentEnd());

		expect(s.messages[0].tools).toHaveLength(2);
		expect(s.messages[0].tools[0].status).toBe("done");
		expect(s.messages[0].tools[1].status).toBe("done");
	});

	test("deduplicates tool calls by id", () => {
		const s = new ChatState();
		emit(s, assistantStart());
		// Same tool id sent twice (message_update sends accumulated state)
		emit(s, messageUpdate([{ type: "toolCall", id: "t1", name: "bash", arguments: { cmd: "ls" } }]));
		emit(s, messageUpdate([{ type: "toolCall", id: "t1", name: "bash", arguments: { cmd: "ls -la" } }]));
		expect(s.messages[0].tools).toHaveLength(1);
	});
});
