/**
 * history-adapter-unit — pure-function tests for the history adapter.
 *
 * storedMessagesToThreadLikes: converts StoredMessage[] → ThreadMessageLike[]
 *   - user messages: just content
 *   - assistant messages: parts built from thinking + toolCalls + text
 *   - tool role messages: re-attached to their parent tool-call
 *
 * restoreAgentsFromHistory: rebuilds AgentState map from subAgent traces
 * persisted inside delegate_to/delegate_async tool calls.
 *
 * Mutation anchors:
 *   - Swap text/thinking assignment in restoreAgentsFromHistory → text test fails
 *   - Push text before toolCalls in storedMessagesToThreadLikes → order test fails
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import {
	storedMessagesToThreadLikes,
	restoreAgentsFromHistory,
	estimateContextFromHistory,
} from "../pux-history-adapter";
import { usePuxStore } from "../pux-store";

// ── StoredMessage factory ──────────────────────────────────────
function msg(overrides: Partial<any> = {}): any {
	return {
		id: Math.floor(Math.random() * 100000),
		project: "test-project",
		agentId: "agent-test",
		role: "assistant",
		content: "",
		text: "",
		thinking: "",
		toolCalls: "[]",
		toolCallId: "",
		toolName: "",
		createdAt: new Date("2026-06-17T12:00:00Z").toISOString(),
		...overrides,
	};
}

// Mock fetch-provider so any incidental use doesn't try a real network.
vi.mock("../fetch-provider", () => ({
	getFetch: () => vi.fn().mockResolvedValue({ ok: true, json: async () => [] }),
	setFetch: vi.fn(),
}));

beforeEach(() => {
	usePuxStore.setState({
		activeProject: "test-project",
		activeProjectPath: "/tmp/test-project",
		activeAgentId: "agent-test",
		activeModel: "test-model",
		agents: new Map(),
		thinkingExpanded: true,
	});
});

// ═══════════════════════════════════════════════════════════════
// storedMessagesToThreadLikes
// ═══════════════════════════════════════════════════════════════

describe("storedMessagesToThreadLikes", () => {
	it("converts a user message to a ThreadLike user entry", () => {
		const data = [msg({
			role: "user",
			content: "hello world",
		})];
		const result = storedMessagesToThreadLikes(data);
		expect(result).toEqual([
			{ role: "user", content: "hello world" },
		]);
	});

	it("converts an assistant message with thinking + text into parts", () => {
		const data = [msg({
			role: "assistant",
			thinking: "I should respond",
			text: "Hi there",
		})];
		const result = storedMessagesToThreadLikes(data);
		expect(result).toHaveLength(1);
		const assistant = result[0] as any;
		expect(assistant.role).toBe("assistant");
		expect(Array.isArray(assistant.content)).toBe(true);
		expect(assistant.content).toEqual([
			{ type: "reasoning", text: "I should respond" },
			{ type: "text", text: "Hi there" },
		]);
	});

	it("assistant with thinking + toolCalls + text preserves parts order: reasoning, tool-call, text", () => {
		const data = [msg({
			role: "assistant",
			thinking: "thinking here",
			toolCalls: JSON.stringify([{
				id: "tc1",
				name: "bash",
				args: { command: "ls" },
				argsText: '{\n  "command": "ls"\n}',
			}]),
			text: "Done.",
		})];
		const result = storedMessagesToThreadLikes(data);
		const assistant = result[0] as any;
		const types = assistant.content.map((p: any) => p.type);
		expect(types).toEqual(["reasoning", "tool-call", "text"]);

		// Tool call shape
		const toolPart = assistant.content.find((p: any) => p.type === "tool-call");
		expect(toolPart.toolCallId).toBe("tc1");
		expect(toolPart.toolName).toBe("bash");
		expect(toolPart.args).toEqual({ command: "ls" });
		expect(toolPart.argsText).toContain('"command": "ls"');
	});

	it("re-attaches tool result from a separate tool-role message", () => {
		const data = [
			msg({
				id: 1,
				role: "assistant",
				thinking: "",
				toolCalls: JSON.stringify([{
					id: "tc_abc",
					name: "bash",
					args: { command: "echo hi" },
				}]),
				text: "",
			}),
			msg({
				id: 2,
				role: "tool",
				toolCallId: "tc_abc",
				content: "command output here",
			}),
		];
		const result = storedMessagesToThreadLikes(data);
		// Tool-role messages are skipped at top level
		expect(result).toHaveLength(1);
		const assistant = result[0] as any;
		const toolPart = assistant.content.find((p: any) => p.type === "tool-call");
		expect(toolPart.result).toBe("command output here");
		expect(toolPart.isError).toBe(false);
	});

	it("marks tool result as error when JSON contains 'error' field", () => {
		const data = [
			msg({
				role: "assistant",
				toolCalls: JSON.stringify([{
					id: "tc1", name: "bash", args: {},
				}]),
			}),
			msg({
				role: "tool",
				toolCallId: "tc1",
				content: JSON.stringify({ error: "command failed", output: "" }),
			}),
		];
		const result = storedMessagesToThreadLikes(data);
		const assistant = result[0] as any;
		const toolPart = assistant.content.find((p: any) => p.type === "tool-call");
		expect(toolPart.isError).toBe(true);
	});

	it("skips empty assistant messages (streaming mid-save artifacts)", () => {
		const data = [
			msg({ role: "assistant", thinking: "", text: "", toolCalls: "[]" }),
			msg({ role: "assistant", text: "actual content" }),
		];
		const result = storedMessagesToThreadLikes(data);
		// Only the non-empty assistant survives
		expect(result).toHaveLength(1);
		expect((result[0] as any).content).toEqual([
			{ type: "text", text: "actual content" },
		]);
	});

	it("preserves message order across multiple messages", () => {
		const data = [
			msg({ id: 1, role: "user", content: "first" }),
			msg({ id: 2, role: "assistant", text: "reply 1" }),
			msg({ id: 3, role: "user", content: "second" }),
			msg({ id: 4, role: "assistant", text: "reply 2" }),
		];
		const result = storedMessagesToThreadLikes(data);
		expect(result.map((m: any) => m.role)).toEqual([
			"user", "assistant", "user", "assistant",
		]);
	});

	it("injects __subAgent arg when delegate tool call has subAgent field", () => {
		const subAgent = {
			name: "browser_ops",
			status: "complete",
			toolCalls: [],
			thinking: "sub thinking",
			text: "sub text",
			result: "sub result",
		};
		const data = [msg({
			role: "assistant",
			toolCalls: JSON.stringify([{
				id: "del_1",
				name: "delegate_to",
				args: { task: "do thing" },
				subAgent,
			}]),
		})];
		const result = storedMessagesToThreadLikes(data);
		const toolPart = (result[0] as any).content.find(
			(p: any) => p.type === "tool-call",
		);
		expect(toolPart.args.__subAgent).toEqual(subAgent);
		expect(toolPart.args.task).toBe("do thing"); // original args preserved
	});
});

// ═══════════════════════════════════════════════════════════════
// restoreAgentsFromHistory
// ═══════════════════════════════════════════════════════════════

describe("restoreAgentsFromHistory", () => {
	it("reconstructs an AgentState from a delegate_to with subAgent trace", () => {
		const createdAt = "2026-06-17T12:00:00Z";
		const data = [msg({
			role: "assistant",
			createdAt,
			toolCalls: JSON.stringify([{
				id: "del_1",
				name: "delegate_to",
				args: { task: "browse the page" },
				subAgent: {
					name: "browser_ops",
					status: "complete",
					toolCalls: [{
						name: "find_element",
						args: { text: "Submit" },
						result: "clicked",
					}],
					thinking: "where is the button",
					text: "I clicked it",
					result: "success",
				},
			}]),
		})];

		restoreAgentsFromHistory(data);

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(1);

		// agentId is constructed as hist_<toolcall_id>
		const agent = agents.get("hist_del_1")!;
		expect(agent).toBeDefined();
		expect(agent.agentName).toBe("browser_ops");
		expect(agent.task).toBe("browse the page");
		expect(agent.status).toBe("complete");
		expect(agent.result).toBe("success");

		// rounds[0] shape
		expect(agent.rounds).toHaveLength(1);
		const round = agent.rounds[0];
		expect(round.thinking).toBe("where is the button");
		expect(round.text).toBe("I clicked it");
		expect(round.toolCalls).toHaveLength(1);
		expect(round.toolCalls[0].toolName).toBe("find_element");
		expect(round.toolCalls[0].result).toBe("clicked");
		expect(round.toolCalls[0].endedAt).toBeDefined();
	});

	it("marks agent as error when subAgent.status is 'error'", () => {
		const data = [msg({
			role: "assistant",
			toolCalls: JSON.stringify([{
				id: "del_1",
				name: "delegate_to",
				args: {},
				subAgent: {
					name: "browser_ops",
					status: "error",
					toolCalls: [],
					error: "tool not found",
				},
			}]),
		})];

		restoreAgentsFromHistory(data);

		const agent = usePuxStore.getState().agents.get("hist_del_1")!;
		expect(agent.status).toBe("error");
		expect(agent.error).toBe("tool not found");
	});

	it("skips delegate_async results that lack subAgent field", () => {
		const data = [msg({
			role: "assistant",
			toolCalls: JSON.stringify([{
				id: "del_1",
				name: "delegate_to",
				args: {},
				// no subAgent field
			}]),
		})];

		restoreAgentsFromHistory(data);

		expect(usePuxStore.getState().agents.size).toBe(0);
	});

	it("does not overwrite a live agent already in the store", () => {
		// Pre-populate with a live (running) agent at the same hist_del_1 ID
		usePuxStore.getState().addAgent({
			agentId: "hist_del_1",
			agentName: "browser_ops",
			task: "live task",
			status: "running",
			startedAt: Date.now(),
			rounds: [{ thinking: "live thinking", toolCalls: [] }],
			toolCalls: [],
		});

		const data = [msg({
			role: "assistant",
			toolCalls: JSON.stringify([{
				id: "del_1",
				name: "delegate_to",
				args: {},
				subAgent: {
					name: "browser_ops",
					status: "complete",
					toolCalls: [],
					thinking: "history thinking",
				},
			}]),
		})];

		restoreAgentsFromHistory(data);

		const agent = usePuxStore.getState().agents.get("hist_del_1")!;
		// Live agent's state is preserved
		expect(agent.status).toBe("running");
		expect(agent.task).toBe("live task");
		expect(agent.rounds[0].thinking).toBe("live thinking");
	});

	it("handles multiple delegate tool calls in one assistant message", () => {
		const data = [msg({
			role: "assistant",
			toolCalls: JSON.stringify([
				{
					id: "del_1",
					name: "delegate_async",
					args: {},
					subAgent: { name: "browser_ops", status: "complete", toolCalls: [], thinking: "t1", text: "x1" },
				},
				{
					id: "del_2",
					name: "delegate_async",
					args: {},
					subAgent: { name: "code_ops", status: "complete", toolCalls: [], thinking: "t2", text: "x2" },
				},
			]),
		})];

		restoreAgentsFromHistory(data);

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(2);
		expect(agents.get("hist_del_1")!.agentName).toBe("browser_ops");
		expect(agents.get("hist_del_2")!.agentName).toBe("code_ops");
	});

	it("survives malformed toolCalls JSON (no crash)", () => {
		const data = [msg({
			role: "assistant",
			toolCalls: "this is not valid JSON",
		})];

		expect(() => restoreAgentsFromHistory(data)).not.toThrow();
		expect(usePuxStore.getState().agents.size).toBe(0);
	});
});

// ═══════════════════════════════════════════════════════════════
// estimateContextFromHistory
// ═══════════════════════════════════════════════════════════════

describe("estimateContextFromHistory", () => {
	beforeEach(() => {
		// Prime the store with a known model + context window
		usePuxStore.setState({
			activeModel: "test-model",
			modelList: [{ id: "test-model", name: "Test", contextWindow: 8000 }],
		});
	});

	it("estimates tokens from total chars at 0.3 ratio", () => {
		// 100 chars × 0.3 = 30 tokens
		const data = [msg({
			role: "user",
			content: "x".repeat(100),
		})];
		estimateContextFromHistory(data);
		const metrics = usePuxStore.getState().contextMetrics;
		expect(metrics).toBeDefined();
		expect(metrics?.contextTokens).toBe(30);
	});

	it("sums content + text + thinking + toolCalls chars", () => {
		// 50 + 50 + 50 + (length of toolCalls JSON, not parsed)
		const toolCallsJson = JSON.stringify([{
			id: "tc1", name: "bash", args: { command: "x".repeat(50) },
		}]);
		const data = [msg({
			role: "assistant",
			text: "y".repeat(50),
			thinking: "z".repeat(50),
			toolCalls: toolCallsJson,
		})];
		estimateContextFromHistory(data);
		const metrics = usePuxStore.getState().contextMetrics!;
		// 50 text + 50 thinking + toolCalls.length chars
		const expectedTokens = Math.round((50 + 50 + toolCallsJson.length) * 0.3);
		expect(metrics.contextTokens).toBe(expectedTokens);
	});

	it("computes contextUtil as tokens / contextWindow", () => {
		// 1000 chars × 0.3 = 300 tokens, window 8000 → 300/8000 = 0.0375
		const data = [msg({ role: "user", content: "a".repeat(1000) })];
		estimateContextFromHistory(data);
		const metrics = usePuxStore.getState().contextMetrics!;
		expect(metrics.contextSize).toBe(8000);
		expect(metrics.contextUtil).toBeCloseTo(300 / 8000, 5);
	});

	it("skips setting metrics when no model in modelList has contextWindow", () => {
		usePuxStore.setState({
			activeModel: "unknown-model",
			modelList: [{ id: "test-model", name: "Test", contextWindow: 8000 }],
		});
		// Clear any prior metrics
		usePuxStore.setState({ contextMetrics: undefined });

		const data = [msg({ role: "user", content: "abc" })];
		estimateContextFromHistory(data);

		expect(usePuxStore.getState().contextMetrics).toBeUndefined();
	});

	it("no-ops when data is empty", () => {
		usePuxStore.setState({ contextMetrics: undefined });
		estimateContextFromHistory([]);
		expect(usePuxStore.getState().contextMetrics).toBeUndefined();
	});

	it("no-ops when estimated tokens is 0", () => {
		usePuxStore.setState({ contextMetrics: undefined });
		// All empty fields → 0 chars → 0 tokens → no set
		const data = [msg({ role: "assistant", text: "", thinking: "", toolCalls: "[]" })];
		estimateContextFromHistory(data);
		expect(usePuxStore.getState().contextMetrics).toBeUndefined();
	});
});
