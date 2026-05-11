import { Container, Text } from "@mariozechner/pi-tui";
import type { Theme } from "../../../src/modes/interactive/theme/theme.js";
import type { SubAgentState } from "./types.js";

// ── Status glyphs ──────────────────────────────────────────────

const GLYPH_RUNNING = "●";
const GLYPH_SUCCESS = "✓";
const GLYPH_ERROR = "✗";

/** Truncate string to max length */
function trunc(s: string, max: number): string {
	return s.length <= max ? s : s.slice(0, max - 1) + "…";
}

/** Format duration from ms */
function fmtDuration(ms: number): string {
	if (ms < 1000) return `${ms}ms`;
	const s = Math.floor(ms / 1000);
	if (s < 60) return `${s}s`;
	return `${Math.floor(s / 60)}m${s % 60}s`;
}

/**
 * Build the sub-agent tracker widget as a Component.
 * Shows running/completed agents with live tool tracking.
 *
 * Layout (running agent):
 *   Running 2 agents…
 *   ├ ● sarah: Research topic X · 5 tools · 12s
 *   │   ⎿  web_search("query...")
 *   │   Recent: file_read → bash → file_write
 *   └ ○ jake: Fill out form
 *
 * Layout (completed):
 *   2 agents done
 *   └ ✓ marcus: Fix auth bug · 8 tools · 34s
 */
export function renderSubAgentWidget(state: SubAgentState, theme: Theme): Container {
	const c = new Container();
	const entries = [...state.agents.values()];

	if (entries.length === 0) return c;

	const running = entries.filter((e) => e.status === "running").length;
	const now = Date.now();

	// Header
	if (running > 0) {
		c.addChild(
			new Text(
				theme.fg("accent", `  Running ${running} agent${running > 1 ? "s" : ""}…`),
				0, 0,
			),
		);
	} else {
		const total = entries.length;
		const failed = entries.filter((e) => e.status === "failed").length;
		const label = failed > 0
			? theme.fg("warning", `  ${total} agent${total > 1 ? "s" : ""} done (${failed} failed)`)
			: theme.fg("success", `  ${total} agent${total > 1 ? "s" : ""} done`);
		c.addChild(new Text(label, 0, 0));
	}

	// Agent entries
	for (let i = 0; i < entries.length; i++) {
		const entry = entries[i];
		const isLast = i === entries.length - 1;
		const prefix = isLast ? "  └ " : "  ├ ";
		const subPrefix = isLast ? "    " : "  │ ";
		const branch = isLast ? "  " : "│ ";
		const elapsed = entry.status === "running" && entry.startedAt
			? fmtDuration(now - entry.startedAt)
			: entry.endedAt && entry.startedAt
				? fmtDuration(entry.endedAt - entry.startedAt)
				: "";

		const icon = entry.status === "running"
			? theme.fg("accent", GLYPH_RUNNING)
			: entry.status === "failed"
				? theme.fg("error", GLYPH_ERROR)
				: theme.fg("success", GLYPH_SUCCESS);

		const taskPreview = trunc(entry.task, 45);
		const toolInfo = entry.toolCount > 0
			? theme.fg("dim", ` · ${entry.toolCount} tool${entry.toolCount !== 1 ? "s" : ""}`)
			: "";
		const timeInfo = elapsed
			? theme.fg("dim", ` · ${elapsed}`)
			: "";

		c.addChild(
			new Text(
				theme.fg("muted", prefix) +
				`${icon} ` +
				theme.bold(entry.agentName) +
				theme.fg("muted", `: ${taskPreview}`) +
				toolInfo +
				timeInfo,
				0, 0,
			),
		);

		// Live: current tool being executed
		if (entry.status === "running" && entry.currentTool) {
			const argsPreview = entry.currentToolArgs
				? trunc(`(${entry.currentToolArgs})`, 40)
				: "";
			c.addChild(
				new Text(
					theme.fg("dim", `${subPrefix}⎿  ${entry.currentTool}${argsPreview ? " " + argsPreview : ""}`),
					0, 0,
				),
			);
		}

		// Recent tools summary (last 4, shown as arrow-joined)
		if (entry.recentTools.length > 0) {
			const recent = entry.recentTools.slice(-4).map((t) => t.tool);
			const toolLine = recent.join(theme.fg("dim", " → "));
			c.addChild(
				new Text(
					theme.fg("dim", `${subPrefix}  ${toolLine}`),
					0, 0,
				),
			);
		}

		// Recent output (last 2 lines, if any)
		if (entry.status === "running" && entry.recentOutput.length > 0) {
			const outputLines = entry.recentOutput.slice(-2);
			for (const line of outputLines) {
				c.addChild(
					new Text(
						theme.fg("dim", `${subPrefix}  ${trunc(line, 60)}`),
						0, 0,
					),
				);
			}
		}
	}

	return c;
}
