/**
 * PUX Subagents Extension
 *
 * Enhanced TUI rendering for sub-agent (employee) delegation.
 * Overrides pux-core's basic delegate rendering with richer visuals:
 *   - Status glyphs, tool counts, duration
 *   - Chain visualization for sequential delegations
 *   - Live sub-agent tracker widget with current tool, recent tools, output
 *
 * Render-only — the Go backend handles actual tool execution.
 */

import { Type } from "@sinclair/typebox";
import type { ExtensionAPI } from "../../src/core/extensions/types.js";
import { renderDelegateCall, renderDelegateResult } from "./tools/delegate.js";
import { renderSubAgentWidget } from "./widget.js";
import { createSubAgentState, addRecentTool, addRecentOutput, type SubAgentInfo } from "./types.js";

const WIDGET_KEY = "pux-subagents";

/** Extract a short preview of tool args for display */
function argsPreview(args: any): string {
	if (!args || typeof args !== "object") return "";
	// Pick the most useful arg for preview
	const cmd = args.command || args.path || args.query || args.task || args.key || args.action || "";
	if (typeof cmd === "string") return truncArgs(cmd, 35);
	return "";
}

function truncArgs(s: string, max: number): string {
	return s.length <= max ? s : s.slice(0, max - 1) + "…";
}

export default function registerPuxSubagentsExtension(pi: ExtensionAPI): void {
	// ── Shared state ──────────────────────────────────────────
	const state = createSubAgentState();
	let widgetCleanupTimer: ReturnType<typeof setTimeout> | undefined;

	/** Update the sub-agent tracker widget below the editor */
	function refreshWidget(ctx: any): void {
		if (state.agents.size === 0) {
			ctx.ui.setWidget(WIDGET_KEY, undefined);
			return;
		}
		ctx.ui.setWidget(WIDGET_KEY, (_tui: any, theme: any) =>
			renderSubAgentWidget(state, theme),
		);
	}

	/** Find a tracked agent by name */
	function findAgent(agentName: string): SubAgentInfo | undefined {
		for (const info of state.agents.values()) {
			if (info.agentName === agentName) return info;
		}
		return undefined;
	}

	// ── Enhanced delegate_to ───────────────────────────────────
	pi.registerTool({
		name: "delegate_to",
		label: "Delegate",
		description: "Delegate a task to an employee agent with enhanced rendering",
		parameters: Type.Object({
			instructions: Type.Optional(Type.String()),
			step: Type.Optional(Type.String()),
			agent_name: Type.Optional(Type.String()),
			role: Type.Optional(Type.String()),
			task: Type.Optional(Type.String()),
		}),
		execute: async () => ({ content: [] }),
		renderCall: (args, theme) => {
			const typedArgs = args as any;
			const chainPos = state.chain.length > 1
				? { index: state.chain.indexOf(typedArgs.agent_name || "") + 1, total: state.chain.length }
				: undefined;
			return renderDelegateCall(typedArgs, theme, chainPos);
		},
		renderResult: (result, options, theme) => {
			// Try to find agent info from state
			let info: SubAgentInfo | undefined;
			for (const agent of state.agents.values()) {
				if (agent.endedAt) { info = agent; break; }
			}
			return renderDelegateResult(
				result as any,
				options as any,
				theme,
				{
					agentName: info?.agentName,
					duration: info?.endedAt && info?.startedAt ? info.endedAt - info.startedAt : undefined,
					toolCount: info?.toolCount,
				},
			);
		},
	});

	// ── Enhanced delegate_async ────────────────────────────────
	pi.registerTool({
		name: "delegate_async",
		label: "Delegate Async",
		description: "Delegate a task asynchronously with enhanced rendering",
		parameters: Type.Object({
			instructions: Type.Optional(Type.String()),
			step: Type.Optional(Type.String()),
			agent_name: Type.Optional(Type.String()),
			role: Type.Optional(Type.String()),
			task: Type.Optional(Type.String()),
		}),
		execute: async () => ({ content: [] }),
		renderCall: (args, theme) => renderDelegateCall(args as any, theme),
		renderResult: (result, options, theme) =>
			renderDelegateResult(result as any, options as any, theme),
	});

	// ── Event hooks for sub-agent tracking ─────────────────────

	// 1) Track delegate_to/delegate_async calls — register the agent
	pi.on("tool_execution_start" as any, (event: any, ctx: any) => {
		if (event.toolName !== "delegate_to" && event.toolName !== "delegate_async") return;
		if ((event as any).agentName) return; // This is a sub-agent's own tool, not the parent delegate

		const agentName = event.args?.agent_name || event.args?.role || "agent";
		const task = event.args?.task || "";

		state.agents.set(event.toolCallId || agentName, {
			agentName,
			task,
			status: "running",
			toolCount: 0,
			recentTools: [],
			recentOutput: [],
			lastAction: "starting...",
			startedAt: Date.now(),
		});

		if (!state.chain.includes(agentName)) {
			state.chain.push(agentName);
		}

		refreshWidget(ctx);
	});

	// 2) Track sub-agent's own tool calls (events with agentName)
	pi.on("tool_execution_start" as any, (event: any, ctx: any) => {
		const agentName = (event as any).agentName;
		if (!agentName) return;
		if (event.toolName === "delegate_to" || event.toolName === "delegate_async") return;

		const info = findAgent(agentName);
		if (!info) return;

		info.toolCount++;
		info.currentTool = event.toolName;
		info.currentToolArgs = argsPreview(event.args);
		info.lastAction = `${event.toolName}(${info.currentToolArgs || "..."})`;

		refreshWidget(ctx);
	});

	// 3) Track sub-agent tool completions
	pi.on("tool_execution_end" as any, (event: any, ctx: any) => {
		const agentName = (event as any).agentName;
		if (!agentName) {
			// This might be the delegate_to tool itself ending
			if (event.toolName === "delegate_to" || event.toolName === "delegate_async") {
				// Find agent by toolCallId
				const entry = state.agents.get(event.toolCallId);
				if (entry) {
					entry.status = event.isError ? "failed" : "completed";
					entry.endedAt = Date.now();
					entry.error = event.isError ? "error" : undefined;
					entry.currentTool = undefined;
					entry.currentToolArgs = undefined;
					if (event.isError) state.failed++;
					else state.completed++;
					refreshWidget(ctx);

					// Auto-cleanup after delay
					clearTimeout(widgetCleanupTimer);
					widgetCleanupTimer = setTimeout(() => {
						state.agents.delete(event.toolCallId);
						if (state.agents.size === 0) {
							state.chain = [];
							state.completed = 0;
							state.failed = 0;
							ctx.ui.setWidget(WIDGET_KEY, undefined);
						} else {
							refreshWidget(ctx);
						}
					}, 3000);
				}
			}
			return;
		}

		// Sub-agent tool completion
		const info = findAgent(agentName);
		if (!info) return;

		if (info.currentTool) {
			addRecentTool(info, info.currentTool, info.currentToolArgs || "");
		}
		info.currentTool = undefined;
		info.currentToolArgs = undefined;

		// Capture output from the tool result for the widget
		if (event.result?.content) {
			const text = event.result.content
				.filter((b: any) => b.type === "text" && b.text)
				.map((b: any) => b.text)
				.join("\n");
			if (text) addRecentOutput(info, text);
		}

		refreshWidget(ctx);
	});

	// Reset state on new agent turn
	pi.on("agent_start" as any, (_event: any, _ctx: any) => {
		state.agents.clear();
		state.chain = [];
		state.completed = 0;
		state.failed = 0;
		clearTimeout(widgetCleanupTimer);
	});
}
