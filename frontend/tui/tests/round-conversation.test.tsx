// @vitest-environment jsdom
/**
 * round-conversation — mount REAL RoundConversation via React Testing Library.
 *
 * Placed in frontend/shared (not frontend/tui) because the bun:test runner
 * in TUI can't resolve the @assistant-ui/react-ink-markdown import chain
 * (pre-existing tap version conflict — see components.test.tsx which is
 * also broken with the same SyntaxError). Vitest's vi.mock intercepts the
 * import properly, so we can mount the real component here.
 *
 * RoundConversation renders each round separately:
 *   thinking (▼ Thought block) → tool calls (✓ lines) → text (markdown)
 *
 * Tests verify:
 *   - Multi-round case preserves order: t1 < tool < text1 < t2 < tool2 < text2
 *   - Empty rounds array falls back to flat toolCalls
 *   - Round with empty thinking → no ▼ Thought header
 *   - Round with empty toolCalls → no tool line
 *   - Round with empty text → no text block
 *
 * Mutation anchors:
 *   - Swap thinking/toolCalls render order in RoundConversation → order test fails
 *   - Remove fallback to flat toolCalls → empty-rounds test fails
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import React from "react";
import { render } from "@testing-library/react";

// ── Mock @assistant-ui/react-ink-markdown before importing agents-view ──
// The real MarkdownText pulls in @assistant-ui/store → @assistant-ui/tap
// chain that has a version conflict in this workspace.
vi.mock("@assistant-ui/react-ink-markdown", () => ({
	MarkdownText: ({ text }: { text: string }) => {
		return React.createElement("div", { "data-testid": "md" }, text);
	},
}));

// ── Mock TUI theme — agents-view pulls useColors/symbols from ../theme.js ──
// ThemeProvider context isn't mounted in this isolated test, so we feed a
// stable color/symbol shape directly.
vi.mock("../../../tui/src/theme.js", () => ({
	useColors: () => ({
		brand: "#ff0000",
		textMuted: "#888",
		running: "#ffff00",
		success: "#00ff00",
		error: "#ff0000",
	}),
	symbols: {
		dot: "·",
		toolRunning: "●",
		toolDone: "✓",
		toolError: "✗",
	},
	BLOCKQUOTE_BAR: "▎",
	ThemeProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// ── Mock @pux/shared to feed minimal store data ──
vi.mock("@pux/shared", async () => {
	const actual: any = await vi.importActual("@pux/shared");
	return {
		...actual,
		getToolArgPreview: (toolName: string, args: any, maxLen?: number) => {
			if (!args) return "";
			if (toolName === "bash") return String(args.command || "").slice(0, maxLen || 50);
			if (toolName === "file_edit") return String(args.path || "").slice(0, maxLen || 50);
			return "";
		},
	};
});

// ── NOW we can import the component under test ──
import { RoundConversation } from "../../../tui/src/components/agents-view.js";
import type { AgentState } from "@pux/shared";

// ── AgentState factory ──────────────────────────────────────────
function makeAgent(overrides: Partial<AgentState> = {}): AgentState {
	return {
		agentId: "test-agent",
		agentName: "browser_ops",
		task: "do thing",
		status: "complete",
		startedAt: Date.now() - 5000,
		endedAt: Date.now(),
		rounds: [],
		toolCalls: [],
		...overrides,
	} as AgentState;
}

// ── Helper ──────────────────────────────────────────────────────

function renderAgent(agent: AgentState, cols = 80) {
	return render(React.createElement(RoundConversation, { agent, cols }));
}

// ═══════════════════════════════════════════════════════════════
// Tests
// ═══════════════════════════════════════════════════════════════

describe("RoundConversation: multi-round rendering", () => {
	it("renders thinking → tool → text in order across two rounds", () => {
		const agent = makeAgent({
			rounds: [
				{
					thinking: "round one thought",
					toolCalls: [{
						toolName: "bash",
						toolCallId: "tc1",
						args: { command: "echo first" },
						result: "first output",
						timestamp: Date.now(),
						endedAt: Date.now() + 100,
					}],
					text: "round one text",
				},
				{
					thinking: "round two thought",
					toolCalls: [{
						toolName: "file_edit",
						toolCallId: "tc2",
						args: { path: "/tmp/foo" },
						result: "edited",
						timestamp: Date.now(),
						endedAt: Date.now() + 100,
					}],
					text: "round two text",
				},
			],
		});

		const { container } = renderAgent(agent);
		const html = container.innerHTML;

		// Each marker must appear, and in this order
		const i1 = html.indexOf("round one thought");
		const i2 = html.indexOf("bash");
		const i3 = html.indexOf("round one text");
		const i4 = html.indexOf("round two thought");
		const i5 = html.indexOf("file_edit");
		const i6 = html.indexOf("round two text");

		expect(i1).toBeGreaterThanOrEqual(0);
		expect(i2).toBeGreaterThan(i1);
		expect(i3).toBeGreaterThan(i2);
		expect(i4).toBeGreaterThan(i3);
		expect(i5).toBeGreaterThan(i4);
		expect(i6).toBeGreaterThan(i5);
	});

	it("emits ▼ Thought header for each round with thinking", () => {
		const agent = makeAgent({
			rounds: [
				{ thinking: "t1", toolCalls: [], text: "" },
				{ thinking: "t2", toolCalls: [], text: "" },
			],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		// Ink renders unicode ▼ (▼). Look for it twice.
		const matches = html.match(/▼/g) ?? [];
		expect(matches.length).toBe(2);
	});
});

describe("RoundConversation: fallback to flat toolCalls", () => {
	it("renders flat toolCalls when rounds is empty", () => {
		const agent = makeAgent({
			rounds: [],
			toolCalls: [
				{
					toolName: "bash",
					toolCallId: "f_tc1",
					args: { command: "ls" },
					result: "file.txt",
					timestamp: Date.now(),
					endedAt: Date.now(),
				},
				{
					toolName: "file_edit",
					toolCallId: "f_tc2",
					args: { path: "/tmp/x" },
					result: "ok",
					timestamp: Date.now(),
					endedAt: Date.now(),
				},
			],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		expect(html).toContain("bash");
		expect(html).toContain("file_edit");
		// No thinking header in flat mode
		expect(html).not.toContain("▼");
	});

	it("renders nothing when both rounds and toolCalls are empty", () => {
		const agent = makeAgent({ rounds: [], toolCalls: [] });
		const { container } = renderAgent(agent);
		expect(container.textContent?.trim().length).toBe(0);
	});
});

describe("RoundConversation: edge cases per round", () => {
	it("round with empty thinking omits ▼ Thought header", () => {
		const agent = makeAgent({
			rounds: [{
				thinking: "",
				toolCalls: [{
					toolName: "bash",
					toolCallId: "x",
					args: {},
					timestamp: Date.now(),
					endedAt: Date.now(),
				}],
				text: "did the thing",
			}],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		expect(html).not.toContain("▼");
		expect(html).toContain("bash");
		expect(html).toContain("did the thing");
	});

	it("round with only thinking (no tools, no text)", () => {
		const agent = makeAgent({
			rounds: [{ thinking: "just thinking", toolCalls: [], text: "" }],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		expect(html).toContain("just thinking");
		expect(html).not.toContain("bash");
	});

	it("round with only text (no thinking, no tools)", () => {
		const agent = makeAgent({
			rounds: [{ thinking: "", toolCalls: [], text: "final answer" }],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		expect(html).toContain("final answer");
		expect(html).not.toContain("▼");
	});

	it("round with only tools (no thinking, no text)", () => {
		const agent = makeAgent({
			rounds: [{
				thinking: "",
				toolCalls: [{
					toolName: "bash",
					toolCallId: "tc",
					args: { command: "ls" },
					timestamp: Date.now(),
					endedAt: Date.now(),
				}],
				text: "",
			}],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		expect(html).toContain("bash");
		expect(html).not.toContain("▼");
	});

	it("whitespace-only thinking is treated as empty", () => {
		const agent = makeAgent({
			rounds: [{
				thinking: "   \n  \t  ",
				toolCalls: [],
				text: "real text",
			}],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		// .trim() check inside RoundConversation should suppress this
		expect(html).not.toContain("▼");
		expect(html).toContain("real text");
	});
});

describe("RoundConversation: tool rendering details", () => {
	it("tool with endedAt shows ✓ (done) icon", () => {
		const agent = makeAgent({
			rounds: [{
				thinking: "",
				toolCalls: [{
					toolName: "bash",
					toolCallId: "tc",
					args: { command: "ls" },
					timestamp: Date.now(),
					endedAt: Date.now(),
				}],
				text: "",
			}],
		});
		const { container } = renderAgent(agent);
		expect(container.innerHTML).toContain("✓");
	});

	it("tool without endedAt shows ● (running) icon", () => {
		const agent = makeAgent({
			rounds: [{
				thinking: "",
				toolCalls: [{
					toolName: "bash",
					toolCallId: "tc",
					args: { command: "ls" },
					timestamp: Date.now(),
					// no endedAt → still running
				}],
				text: "",
			}],
		});
		const { container } = renderAgent(agent);
		expect(container.innerHTML).toContain("●");
		expect(container.innerHTML).not.toContain("✓");
	});

	it("tool with isError=true shows ✗ icon", () => {
		const agent = makeAgent({
			rounds: [{
				thinking: "",
				toolCalls: [{
					toolName: "bash",
					toolCallId: "tc",
					args: { command: "bad-cmd" },
					timestamp: Date.now(),
					endedAt: Date.now(),
					isError: true,
				}],
				text: "",
			}],
		});
		const { container } = renderAgent(agent);
		expect(container.innerHTML).toContain("✗");
	});

	it("tool arg preview is rendered next to tool name", () => {
		const agent = makeAgent({
			rounds: [{
				thinking: "",
				toolCalls: [{
					toolName: "bash",
					toolCallId: "tc",
					args: { command: "echo hello world" },
					timestamp: Date.now(),
					endedAt: Date.now(),
				}],
				text: "",
			}],
		});
		const { container } = renderAgent(agent);
		// getToolArgPreview returns the command for bash
		expect(container.innerHTML).toContain("echo hello world");
	});
});

describe("RoundConversation: text overflow handling", () => {
	it("truncates very long round text with ellipsis", () => {
		const longText = "x".repeat(3000);
		const agent = makeAgent({
			rounds: [{ thinking: "", toolCalls: [], text: longText }],
		});
		const { container } = renderAgent(agent);
		const html = container.innerHTML;
		expect(html).toContain("...");
		// Should not contain the full 3000-char string
		expect(html.length).toBeLessThan(longText.length + 200);
	});

	it("preserves normal-length text as-is", () => {
		const text = "This is a normal response.";
		const agent = makeAgent({
			rounds: [{ thinking: "", toolCalls: [], text }],
		});
		const { container } = renderAgent(agent);
		expect(container.innerHTML).toContain(text);
		expect(container.innerHTML).not.toContain("...");
	});
});
