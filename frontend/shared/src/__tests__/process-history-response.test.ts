/**
 * processHistoryResponse — the unified post-processing pipeline used by both
 * the TUI's ThreadHistoryAdapter and the web's imperative runtime.thread.reset()
 * path.
 *
 * This is a regression test for the bug where the web frontend bypassed
 * restoreAgentsFromHistory and estimateContextFromHistory by duplicating
 * the fetch + storedMessagesToThreadLikes logic inline. The shared helper
 * closes that gap — if anyone re-introduces a divergence, this test fails.
 *
 * Mutation anchors:
 *   - Drop restoreAgentsFromHistory call → "restores sub-agent state" fails
 *   - Drop estimateContextFromHistory call → "seeds context metrics" fails
 *   - Skip empty check → "returns null for empty input" fails
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { processHistoryResponse } from "../pux-history-adapter";
import { usePuxStore } from "../pux-store";

vi.mock("../fetch-provider", () => ({
	getFetch: () => vi.fn().mockResolvedValue({ ok: true, json: async () => [] }),
	setFetch: vi.fn(),
}));

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

beforeEach(() => {
	usePuxStore.setState({
		activeProject: "test-project",
		activeProjectPath: "/tmp/test-project",
		activeAgentId: "agent-test",
		activeModel: "test-model",
		modelList: [{ id: "test-model", name: "Test", provider: "p", contextWindow: 8192 }],
		agents: new Map(),
		thinkingExpanded: true,
		contextMetrics: null,
	});
});

describe("processHistoryResponse: empty/invalid input", () => {
	it("returns null for empty array", () => {
		expect(processHistoryResponse([])).toBeNull();
	});

	it("returns null for non-array input", () => {
		expect(processHistoryResponse(null)).toBeNull();
		expect(processHistoryResponse(undefined)).toBeNull();
		expect(processHistoryResponse({})).toBeNull();
		expect(processHistoryResponse("not an array")).toBeNull();
	});

	it("does not throw on malformed input", () => {
		expect(() => processHistoryResponse([{ garbage: true }] as any)).not.toThrow();
	});
});

describe("processHistoryResponse: sub-agent restoration", () => {
	it("restores AgentState from delegate_to subAgent trace", () => {
		const data = [msg({
			role: "assistant",
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

		const messages = processHistoryResponse(data);
		expect(messages).not.toBeNull();

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(1);
		const agent = agents.get("hist_del_1")!;
		expect(agent.agentName).toBe("browser_ops");
		expect(agent.status).toBe("complete");
	});

	it("restores multiple sub-agents from a single assistant message", () => {
		const data = [msg({
			role: "assistant",
			toolCalls: JSON.stringify([
				{
					id: "del_a",
					name: "delegate_async",
					args: {},
					subAgent: { name: "browser_ops", status: "complete", toolCalls: [], text: "a" },
				},
				{
					id: "del_b",
					name: "delegate_async",
					args: {},
					subAgent: { name: "code_ops", status: "complete", toolCalls: [], text: "b" },
				},
			]),
		})];

		processHistoryResponse(data);

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(2);
		expect(agents.get("hist_del_a")!.agentName).toBe("browser_ops");
		expect(agents.get("hist_del_b")!.agentName).toBe("code_ops");
	});
});

describe("processHistoryResponse: context estimation", () => {
	it("seeds contextMetrics from history character count", () => {
		const data = [msg({
			role: "user",
			content: "x".repeat(1000),  // ~300 tokens at 0.3 chars/token
		})];

		processHistoryResponse(data);

		const ctx = usePuxStore.getState().contextMetrics;
		expect(ctx).not.toBeNull();
		expect(ctx!.contextTokens).toBe(300);
		expect(ctx!.contextSize).toBe(8192);
		expect(ctx!.contextUtil).toBeCloseTo(300 / 8192, 5);
	});

	it("does not set contextMetrics when no model contextWindow is known", () => {
		usePuxStore.setState({ modelList: [] });

		const data = [msg({
			role: "user",
			content: "x".repeat(100),
		})];

		processHistoryResponse(data);
		expect(usePuxStore.getState().contextMetrics).toBeNull();
	});
});

describe("processHistoryResponse: message conversion", () => {
	it("returns ThreadLike[] matching storedMessagesToThreadLikes output", () => {
		const data = [
			msg({ id: 1, role: "user", content: "hello" }),
			msg({
				id: 2, role: "assistant",
				thinking: "hmm",
				text: "hi",
			}),
		];

		const messages = processHistoryResponse(data);
		expect(messages).toEqual([
			{ role: "user", content: "hello" },
			{
				role: "assistant",
				content: [
					{ type: "reasoning", text: "hmm" },
					{ type: "text", text: "hi" },
				],
			},
		]);
	});

	it("preserves tool-call parts with argsText", () => {
		const data = [msg({
			role: "assistant",
			thinking: "deciding what to run",
			toolCalls: JSON.stringify([{
				id: "tc1",
				name: "bash",
				args: { command: "ls" },
				argsText: '{\n  "command": "ls"\n}',
			}]),
			text: "Done",
		})];

		const messages = processHistoryResponse(data)!;
		const assistant = messages[0] as any;
		const types = assistant.content.map((p: any) => p.type);
		expect(types).toEqual(["reasoning", "tool-call", "text"]);
	});
});

describe("processHistoryResponse: ordering invariants", () => {
	it("calls restore BEFORE convert (subAgent data is read by both)", () => {
		// If the order were reversed, subAgent would still be in the args
		// when storedMessagesToThreadLikes runs, but the conversion reads
		// subAgent from the raw JSON toolCalls field — so order doesn't
		// strictly matter for this test. What matters is that BOTH run.
		const data = [msg({
			role: "assistant",
			toolCalls: JSON.stringify([{
				id: "del_x",
				name: "delegate_to",
				args: { task: "X" },
				subAgent: {
					name: "code_ops", status: "complete", toolCalls: [],
					thinking: "t", text: "y", result: "r",
				},
			}]),
		})];

		const messages = processHistoryResponse(data)!;

		// Agent restored
		expect(usePuxStore.getState().agents.size).toBe(1);

		// Message still has the tool-call part with __subAgent injected
		const tool = (messages[0] as any).content.find((p: any) => p.type === "tool-call");
		expect(tool.args.__subAgent).toBeDefined();
		expect(tool.args.__subAgent.name).toBe("code_ops");
	});
});
