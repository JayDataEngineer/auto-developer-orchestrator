/**
 * PUX Subagents Extension
 *
 * Enhanced TUI rendering for sub-agent (employee) delegation.
 * Overrides pux-core's basic delegate rendering with richer visuals:
 *   - Status glyphs, tool counts, duration
 *   - Chain visualization for sequential delegations
 *   - Live sub-agent tracker widget below the editor
 *
 * Render-only — the Go backend handles actual tool execution.
 */

import { Type } from "@sinclair/typebox";
import type { ExtensionAPI } from "../../src/core/extensions/types.js";
import { renderDelegateCall, renderDelegateResult, renderChainVisualization } from "./tools/delegate.js";
import { renderSubAgentWidget } from "./widget.js";
import { createSubAgentState, type SubAgentState, type SubAgentInfo } from "./types.js";

const WIDGET_KEY = "pux-subagents";

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
			const agentName = (result as any)?._meta?.agentName;
			const info = agentName ? state.agents.get(agentName) : undefined;
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

	// Track when a sub-agent starts (from SSE subagent_start event)
	pi.on("tool_execution_start" as any, (event: any, ctx: any) => {
		// Only track delegate_to / delegate_async calls
		if (event.toolName !== "delegate_to" && event.toolName !== "delegate_async") return;

		const agentName = event.args?.agent_name || event.args?.role || "agent";
		const task = event.args?.task || "";

		// Add to state
		state.agents.set(event.toolCallId || agentName, {
			agentName,
			task,
			status: "running",
			toolCount: 0,
			lastAction: "starting...",
			startedAt: Date.now(),
		});

		// Track chain order
		if (!state.chain.includes(agentName)) {
			state.chain.push(agentName);
		}

		refreshWidget(ctx);
	});

	// Track tool calls from within sub-agents (agentName field on SSE events)
	pi.on("tool_execution_start" as any, (event: any, ctx: any) => {
		if (!event.agentName) return;
		// Find the sub-agent entry (could be keyed by name or toolCallId)
		for (const [key, info] of state.agents) {
			if (info.agentName === event.agentName) {
				info.toolCount++;
				info.lastAction = `${event.toolName}(...)`;
				refreshWidget(ctx);
				break;
			}
		}
	});

	// Track when a sub-agent finishes
	pi.on("tool_execution_end" as any, (event: any, ctx: any) => {
		if (event.toolName !== "delegate_to" && event.toolName !== "delegate_async") return;

		const agentName = event.args?.agent_name || event.args?.role || "agent";
		const key = event.toolCallId || agentName;
		const entry = state.agents.get(key);

		if (entry) {
			entry.status = event.isError ? "failed" : "completed";
			entry.endedAt = Date.now();
			entry.error = event.isError ? "error" : undefined;
			if (event.isError) state.failed++;
			else state.completed++;
		}

		refreshWidget(ctx);

		// Auto-cleanup after delay
		clearTimeout(widgetCleanupTimer);
		widgetCleanupTimer = setTimeout(() => {
			state.agents.delete(key);
			if (state.agents.size === 0) {
				state.chain = [];
				state.completed = 0;
				state.failed = 0;
				ctx.ui.setWidget(WIDGET_KEY, undefined);
			} else {
				refreshWidget(ctx);
			}
		}, 3000);
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
