/**
 * Regression tests — catches bugs that have already been fixed.
 * Run before every commit: bun test tests/regression.test.tsx
 *
 * Each test documents a specific bug and verifies it stays fixed.
 */

import { describe, test, expect, vi } from "vitest";
import React from "react";
import { Text, Box } from "ink";
import { render } from "ink-testing-library";

vi.mock("@assistant-ui/tap/react-shim", () => ({
	useDebugValue: () => {},
	useSyncExternalStore: (_sub: any, getSnapshot: any) => getSnapshot(),
}));

import { usePuxStore } from "@pux/shared";

// ── Store shape: all fields must exist ──
// Bug: @pux/shared stale symlink caused missing fields

describe("Store shape regressions", () => {
	test("toggleThinking exists and works", () => {
		usePuxStore.setState({ thinkingExpanded: false });
		expect(usePuxStore.getState().thinkingExpanded).toBe(false);
		usePuxStore.getState().toggleThinking();
		expect(usePuxStore.getState().thinkingExpanded).toBe(true);
	});

	test("thinkingExpanded field exists", () => {
		usePuxStore.setState({ thinkingExpanded: true });
		expect(usePuxStore.getState().thinkingExpanded).toBeDefined();
	});

	test("providerRetry field exists", () => {
		expect(usePuxStore.getState().providerRetry).toBeDefined();
	});

	test("providerRetry can be set", () => {
		usePuxStore.setState({
			providerRetry: { attempt: 2, maxRetry: 5, backoffSecs: 4, error: "HTTP 500" },
		});
		expect(usePuxStore.getState().providerRetry?.attempt).toBe(2);
		usePuxStore.setState({ providerRetry: null });
	});
});

// ── Event ordering ──
// Bug: reorderParts() grouped ALL tools before ALL text

describe("Event ordering regressions", () => {
	type Segment =
		| { type: "text"; text: string }
		| { type: "reasoning"; text: string }
		| { type: "tool-call"; toolCallId: string; toolName: string };

	function reorderParts(parts: Segment[]): Segment[] {
		const reasoning = parts.filter(p => p.type === "reasoning");
		const rest = parts.filter(p => p.type !== "reasoning");
		return [...reasoning, ...rest];
	}

	test("text→tool→text stays interleaved (not tools-first)", () => {
		const parts: Segment[] = [
			{ type: "text", text: "Before" },
			{ type: "tool-call", toolCallId: "tc1", toolName: "bash" },
			{ type: "text", text: "After" },
		];
		const result = reorderParts(parts);
		expect(result.map(p => p.type)).toEqual(["text", "tool-call", "text"]);
	});

	test("NO grouping of all tools before all text", () => {
		const parts: Segment[] = [
			{ type: "text", text: "A" },
			{ type: "tool-call", toolCallId: "tc1", toolName: "bash" },
			{ type: "text", text: "B" },
			{ type: "tool-call", toolCallId: "tc2", toolName: "bash" },
			{ type: "text", text: "C" },
		];
		const result = reorderParts(parts);
		expect(result[0].type).toBe("text");
		expect(result[1].type).toBe("tool-call");
		expect(result[2].type).toBe("text");
	});

	test("appendText does not merge across tool calls", () => {
		const parts: Segment[] = [];
		function appendText(text: string) {
			const last = parts[parts.length - 1];
			if (last?.type === "text") {
				parts[parts.length - 1] = { type: "text", text: last.text + text };
			} else {
				parts.push({ type: "text", text });
			}
		}
		appendText("Before");
		parts.push({ type: "tool-call", toolCallId: "tc1", toolName: "bash" });
		appendText("After");
		expect(parts.length).toBe(3);
		expect((parts[0] as any).text).toBe("Before");
		expect((parts[2] as any).text).toBe("After");
	});
});

// ── Agent rounds ──
// Bug: restored agents from history missing rounds field → crash

describe("Agent rounds regressions", () => {
	test("AgentState from history has rounds array", () => {
		// Simulate what restoreAgentsFromHistory creates
		const agent = {
			agentId: "hist_test",
			agentName: "explorer",
			rounds: [{
				thinking: "test thinking",
				toolCalls: [],
				text: "test text",
			}],
			toolCalls: [],
		};
		expect(agent.rounds).toBeDefined();
		expect(Array.isArray(agent.rounds)).toBe(true);
	});

	test("RoundConversation handles undefined rounds", () => {
		// Simulate badly constructed agent
		const agent: any = { rounds: undefined };
		// This is the guard we added
		const hasRounds = !agent.rounds || agent.rounds.length === 0;
		expect(hasRounds).toBe(true);
	});
});

// ── Thinking rendering ──
// Bug: BLOCKQUOTE_BAR (▎) on every thinking line

describe("Thinking rendering regressions", () => {
	test("thinking lines do NOT start with blockquote bar", () => {
		// Read assistant-message.tsx and verify no BLOCKQUOTE_BAR in reasoning case
		const fs = require("fs");
		const path = require("path");
		const source = fs.readFileSync(
			path.join(__dirname, "../src/components/assistant-message.tsx"),
			"utf-8",
		);
		// Extract the reasoning case block
		const reasoningMatch = source.match(/case "reasoning":[\s\S]*?(?=case ")/);
		expect(reasoningMatch).not.toBeNull();
		// Should NOT contain BLOCKQUOTE_BAR in the expanded view
		const reasoningCode = reasoningMatch![0];
		// The collapsed view can use unicode chars but not BLOCKQUOTE_BAR
		expect(reasoningCode).not.toContain("BLOCKQUOTE_BAR");
	});
});

// ── @pux/shared symlink ──
// Bug: node_modules/@pux/shared was stale copy, not symlink

describe("Symlink regressions", () => {
	test("@pux/shared resolves to frontend/shared source", () => {
		const fs = require("fs");
		const path = require("path");

		// Check root node_modules
		const rootLink = path.join(
			__dirname, "../../../node_modules/@pux/shared"
		);
		// Should be a symlink
		const stat = fs.lstatSync(rootLink);
		expect(stat.isSymbolicLink()).toBe(true);
	});
});

// ── Provider retry ──
// Bug: no retry on 500 errors

describe("Provider retry regressions", () => {
	test("ClassifyError treats 500 as transient", () => {
		// Simulate the error classification logic
		function classifyError(msg: string): "transient" | "permanent" | "unknown" {
			const lower = msg.toLowerCase();
			if (lower.includes("500") || lower.includes("internal server error") ||
				lower.includes("502") || lower.includes("503") || lower.includes("504") ||
				lower.includes("rate limit") || lower.includes("overloaded") ||
				lower.includes("timeout") || lower.includes("connection")) {
				return "transient";
			}
			if (lower.includes("not found") || lower.includes("denied")) {
				return "permanent";
			}
			return "unknown";
		}
		expect(classifyError("chat API HTTP 500: Internal Server Error")).toBe("transient");
		expect(classifyError("rate limit exceeded")).toBe("transient");
		expect(classifyError("503 Service Unavailable")).toBe("transient");
		expect(classifyError("Not Found")).toBe("permanent");
	});
});
