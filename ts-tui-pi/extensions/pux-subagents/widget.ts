import { Container, Text } from "@mariozechner/pi-tui";
import type { Theme } from "../../../src/modes/interactive/theme/theme.js";
import type { SubAgentInfo, SubAgentState } from "../types.js";

// ── Status glyphs ──────────────────────────────────────────────

const GLYPH_RUNNING = "●";
const GLYPH_SUCCESS = "✓";
const GLYPH_ERROR = "✗";

/** Truncate string to max length */
function trunc(s: string, max: number): string {
	return s.length <= max ? s : s.slice(0, max - 1) + "…";
}

/**
 * Build the sub-agent tracker widget as a Component.
 * Shows running/completed agents below the editor.
 *
 * Layout:
 *   Running 3 agents…
 *   ├ ● sarah: Research topic X · 5 tools
 *   │   ⎿  web_search(...)
 *   ├ ● jake: Fill out form · 2 tools
 *   └ ✓ marcus: Fix bug in auth.ts · 3 tools
 */
export function renderSubAgentWidget(state: SubAgentState, theme: Theme): Container {
	const c = new Container();
	const entries = [...state.agents.values()];

	if (entries.length === 0) return c;

	const running = entries.filter((e) => e.status === "running").length;

	// Header
	if (running > 0) {
		c.addChild(
			new Text(
				theme.fg("accent", `  Running ${running} agent${running > 1 ? "s" : ""}…`),
				0, 0,
			),
		);
	} else {
		// All done briefly — show completion summary
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

		const icon = entry.status === "running"
			? theme.fg("accent", GLYPH_RUNNING)
			: entry.status === "failed"
				? theme.fg("error", GLYPH_ERROR)
				: theme.fg("success", GLYPH_SUCCESS);

		const taskPreview = trunc(entry.task, 50);
		const toolInfo = entry.toolCount > 0
			? theme.fg("dim", ` · ${entry.toolCount} tool${entry.toolCount !== 1 ? "s" : ""}`)
			: "";

		c.addChild(
			new Text(
				theme.fg("muted", prefix) +
				`${icon} ` +
				theme.bold(entry.agentName) +
				theme.fg("muted", `: ${taskPreview}`) +
				toolInfo,
				0, 0,
			),
		);

		// Sub-line: last action (only for running agents)
		if (entry.status === "running" && entry.lastAction && entry.lastAction !== "starting...") {
			const subPrefix = isLast ? "    ⎿  " : "  │ ⎿  ";
			const actionPreview = trunc(entry.lastAction, 60);
			c.addChild(
				new Text(theme.fg("dim", `${subPrefix}${actionPreview}`), 0, 0),
			);
		}
	}

	return c;
}
