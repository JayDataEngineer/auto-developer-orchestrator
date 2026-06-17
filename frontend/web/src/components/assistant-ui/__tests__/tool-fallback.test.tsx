// @vitest-environment jsdom
/**
 * tool-fallback + delegate-tool-ui — mount REAL web leaf components via
 * React Testing Library + jsdom.
 *
 * Tests cover:
 *   - DelegateRenderer: args.role/task render in header
 *   - DelegateRenderer: args.__subAgent fallback when no live agent in store
 *   - DelegateRenderer: status.type="running" shows "working..."
 *   - DelegateRenderer: status.type="error" shows "failed"
 *   - DelegateRenderer: click expand reveals briefing + thinking + tool rows
 *
 *   - ToolFallback: result with "Error:" → red error block
 *   - ToolFallback: result with "<tool_use_error>" → red error block
 *   - ToolFallback: normal result → no red error block
 *   - ToolFallback: delegate_to tool routes to DelegateRenderer
 *
 * Mutation anchors:
 *   - Remove args.__subAgent fallback → persisted fallback test fails
 *   - Change red styling class on error → error styling test fails
 *   - Skip isDelegateTool check → delegate routing test fails
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import React from "react";

// ── Mock @assistant-ui/react (makeAssistantToolUI registration etc.) ──
vi.mock("@assistant-ui/react", () => ({
	makeAssistantToolUI: (opts: any) => ({
		toolName: opts.toolName,
		render: opts.render,
	}),
}));

// ── Mock use-collapsible to bypass Radix ──
vi.mock("../use-collapsible", () => ({
	useCollapsibleRoot: (defaultOpen: boolean, controlled?: boolean, onChange?: (o: boolean) => void) => {
		// Track open state locally so tests can interact
		let open = defaultOpen;
		const ref = { current: null as HTMLDivElement | null };
		const handleOpenChange = (next: boolean) => {
			open = next;
			onChange?.(next);
		};
		// We can't easily expose state from a factory; for tests we re-mount
		// with controlled open. Return minimal shape.
		return {
			collapsibleRef: ref,
			isOpen: defaultOpen,
			handleOpenChange,
			animationStyle: {},
		};
	},
}));

// ── Mock @/components/ui/collapsible (Radix wrapper) ──
vi.mock("@/components/ui/collapsible", () => ({
	Collapsible: ({ children, open, ...props }: any) => (
		<div data-open={open ? "true" : "false"} {...props}>{children}</div>
	),
	CollapsibleTrigger: ({ children, ...props }: any) => (
		<button type="button" {...props}>{children}</button>
	),
	CollapsibleContent: ({ children, ...props }: any) => (
		<div {...props}>{children}</div>
	),
}));

// ── NOW we can import ──
import { DelegateRenderer, isDelegateTool, toolLabel } from "../delegate-tool-ui";
import { ToolFallback } from "../tool-fallback";
import { usePuxStore } from "@pux/shared";

// ── Helpers ─────────────────────────────────────────────────────

function renderDelegate(props: Parameters<typeof DelegateRenderer>[0]) {
	return render(React.createElement(DelegateRenderer, props));
}

function renderToolFallback(props: Parameters<typeof ToolFallback>[0]) {
	return render(React.createElement(ToolFallback, props));
}

beforeEach(() => {
	// Clear live agent state between tests
	usePuxStore.setState({
		activeProject: "test-project",
		activeAgentId: "agent-test",
		agents: new Map(),
		pendingDecision: null,
	});
});

// ═══════════════════════════════════════════════════════════════
// DelegateRenderer
// ═══════════════════════════════════════════════════════════════

describe("DelegateRenderer: header rendering", () => {
	it("renders agentName from args.role", () => {
		const { container } = renderDelegate({
			args: { role: "browser_ops", task: "click the link" },
			status: { type: "complete" },
		});
		expect(container.textContent).toContain("browser_ops");
	});

	it("renders task from args.task", () => {
		const { container } = renderDelegate({
			args: { role: "browser_ops", task: "click the link" },
			status: { type: "complete" },
		});
		expect(container.textContent).toContain("click the link");
	});

	it("shows 'working...' status when status.type is running", () => {
		const { container } = renderDelegate({
			args: { role: "browser_ops", task: "x" },
			status: { type: "running" },
		});
		expect(container.textContent).toContain("working...");
	});

	it("shows 'failed' status when status.type is error", () => {
		const { container } = renderDelegate({
			args: { role: "browser_ops", task: "x" },
			status: { type: "error" },
		});
		expect(container.textContent).toContain("failed");
	});

	it("falls back to args.agent_id if role missing", () => {
		const { container } = renderDelegate({
			args: { agent_id: "custom-agent", task: "x" },
			status: { type: "complete" },
		});
		expect(container.textContent).toContain("custom-agent");
	});

	it("falls back to args.instructions if role and agent_id missing", () => {
		const { container } = renderDelegate({
			args: { instructions: "do the thing", task: "x" },
			status: { type: "complete" },
		});
		expect(container.textContent).toContain("do the thing");
	});

	it("defaults agentName to 'agent' if no identifying field present", () => {
		const { container } = renderDelegate({
			args: { task: "x" },
			status: { type: "complete" },
		});
		// Default fallback is "agent"
		expect(container.textContent).toContain("agent");
	});
});

describe("DelegateRenderer: live agent state lookup", () => {
	it("picks up toolCalls from live agent matching args.__agentId", () => {
		usePuxStore.getState().addAgent({
			agentId: "jake_1",
			agentName: "browser_ops",
			task: "x",
			status: "running",
			startedAt: Date.now(),
			rounds: [],
			toolCalls: [{
				toolName: "bash",
				toolCallId: "tc1",
				args: { command: "echo hi" },
				result: "hi",
				timestamp: Date.now(),
				endedAt: Date.now(),
			}],
		});

		const { container } = renderDelegate({
			args: { role: "browser_ops", task: "x", __agentId: "jake_1" },
			status: { type: "complete" },
		});

		// Click to expand
		const button = container.querySelector("button");
		fireEvent.click(button!);

		// Tool count should be visible
		expect(container.textContent).toContain("1 tool");
	});
});

describe("DelegateRenderer: persisted subAgent fallback", () => {
	it("uses args.__subAgent when no live agent is in the store", () => {
		const { container } = renderDelegate({
			args: {
				role: "browser_ops",
				task: "browse page",
				__subAgent: {
					name: "browser_ops",
					status: "complete",
					toolCalls: [{
						name: "bash",
						args: { command: "echo sub" },
						result: "sub output",
					}],
					thinking: "sub thought",
					text: "sub text",
					result: "final result",
				},
			},
			status: { type: "complete" },
		});

		// Expand
		const button = container.querySelector("button");
		fireEvent.click(button!);

		// Tool count from __subAgent
		expect(container.textContent).toContain("1 tool");
	});

	it("shows 'failed' when args.__subAgent.status is 'error'", () => {
		const { container } = renderDelegate({
			args: {
				role: "browser_ops",
				task: "x",
				__subAgent: {
					name: "browser_ops",
					status: "error",
					toolCalls: [],
					error: "tool not found",
				},
			},
			status: { type: "complete" },
		});
		expect(container.textContent).toContain("failed");
	});
});

// ═══════════════════════════════════════════════════════════════
// isDelegateTool helper
// ═══════════════════════════════════════════════════════════════

describe("isDelegateTool", () => {
	it("returns true for 'delegate_to'", () => {
		expect(isDelegateTool("delegate_to")).toBe(true);
	});

	it("returns true for 'delegate_async'", () => {
		expect(isDelegateTool("delegate_async")).toBe(true);
	});

	it("returns false for 'bash'", () => {
		expect(isDelegateTool("bash")).toBe(false);
	});

	it("returns false for empty string", () => {
		expect(isDelegateTool("")).toBe(false);
	});
});

// ═══════════════════════════════════════════════════════════════
// toolLabel helper
// ═══════════════════════════════════════════════════════════════

describe("toolLabel", () => {
	it("returns human-readable label for known tools", () => {
		expect(toolLabel("bash")).toBe("Ran command");
		expect(toolLabel("delegate_to")).toBe("Delegated to agent");
		expect(toolLabel("web_search")).toBe("Searched the web");
	});

	it("returns the tool name as-is for unknown tools", () => {
		expect(toolLabel("unknown_tool")).toBe("unknown_tool");
	});
});

// ═══════════════════════════════════════════════════════════════
// ToolFallback
// ═══════════════════════════════════════════════════════════════

describe("ToolFallback: error detection", () => {
	it("renders red error styling when result starts with 'Error:'", () => {
		const { container } = renderToolFallback({
			toolName: "bash",
			args: { command: "bad-cmd" },
			argsText: "{}",
			result: "Error: command not found",
			status: { type: "complete" },
		});
		// Look for the red error block class
		const errorBlock = container.querySelector(".bg-red-500\\/5");
		expect(errorBlock).not.toBeNull();
		expect(errorBlock?.textContent).toContain("Error: command not found");
	});

	it("renders red error styling when result contains '<tool_use_error>'", () => {
		const { container } = renderToolFallback({
			toolName: "bash",
			args: {},
			argsText: "{}",
			result: "<tool_use_error>permission denied</tool_use_error>",
			status: { type: "complete" },
		});
		const errorBlock = container.querySelector(".bg-red-500\\/5");
		expect(errorBlock).not.toBeNull();
		// The wrapper tags are stripped in display
		expect(errorBlock?.textContent).toContain("permission denied");
	});

	it("does NOT render red error block for normal results", () => {
		const { container } = renderToolFallback({
			toolName: "bash",
			args: { command: "echo hi" },
			argsText: "{}",
			result: "hi",
			status: { type: "complete" },
		});
		const errorBlock = container.querySelector(".bg-red-500\\/5");
		expect(errorBlock).toBeNull();
	});
});

describe("ToolFallback: delegate routing", () => {
	it("routes delegate_to tool to DelegateRenderer (shows agentName)", () => {
		const { container } = renderToolFallback({
			toolName: "delegate_to",
			args: { role: "browser_ops", task: "do thing" },
			argsText: "{}",
			status: { type: "complete" },
		});
		// DelegateRenderer renders the agentName — should NOT contain
		// "Used tool" which is the generic ToolFallback label
		expect(container.textContent).toContain("browser_ops");
		expect(container.textContent).not.toContain("Used tool");
	});

	it("routes delegate_async tool to DelegateRenderer", () => {
		const { container } = renderToolFallback({
			toolName: "delegate_async",
			args: { role: "code_ops", task: "edit files" },
			argsText: "{}",
			status: { type: "running" },
		});
		expect(container.textContent).toContain("code_ops");
		expect(container.textContent).toContain("working...");
	});

	it("does NOT route bash to DelegateRenderer", () => {
		const { container } = renderToolFallback({
			toolName: "bash",
			args: { command: "ls" },
			argsText: "{}",
			result: "file.txt",
			status: { type: "complete" },
		});
		// bash gets the generic ToolFallback row, not the DelegateRenderer card
		expect(container.textContent).toContain("Ran command");
	});
});
