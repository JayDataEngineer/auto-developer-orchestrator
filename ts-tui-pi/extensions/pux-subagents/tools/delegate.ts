import { Container, Spacer, Text } from "@mariozechner/pi-tui";
import type { Theme } from "../../../src/modes/interactive/theme/theme.js";

// ── Status glyphs ──────────────────────────────────────────────

const GLYPH_RUNNING = "●";
const GLYPH_SUCCESS = "✓";
const GLYPH_ERROR = "✗";
const GLYPH_PENDING = "○";

function statusGlyph(status: string, theme: Theme): string {
	switch (status) {
		case "running": return theme.fg("accent", GLYPH_RUNNING);
		case "completed": return theme.fg("success", GLYPH_SUCCESS);
		case "failed": return theme.fg("error", GLYPH_ERROR);
		default: return theme.fg("muted", GLYPH_PENDING);
	}
}

/** Format duration from ms to human-readable */
function fmtDuration(ms: number): string {
	if (ms < 1000) return `${ms}ms`;
	const s = Math.floor(ms / 1000);
	if (s < 60) return `${s}s`;
	const m = Math.floor(s / 60);
	const rs = s % 60;
	return `${m}m${rs}s`;
}

/** Truncate a string preserving any ANSI codes through ellipsis */
function truncLine(s: string, max: number): string {
	if (s.length <= max) return s;
	// Simple truncation — works for plain text + ANSI
	return s.slice(0, max - 1) + "…";
}

// ── Tool call rendering ────────────────────────────────────────

interface DelegateCallArgs {
	instructions?: string;
	step?: string;
	agent_name?: string;
	role?: string;
	task?: string;
}

/**
 * Enhanced renderCall for delegate_to / delegate_async.
 * Shows agent role with accent dot + task preview + chain position.
 */
export function renderDelegateCall(
	args: DelegateCallArgs,
	theme: Theme,
	chainPosition?: { index: number; total: number },
): Container {
	const c = new Container();
	const role = args.agent_name || args.role || args.instructions || args.step || "agent";
	const task = args.task || "";
	const preview = truncLine(task, 80);

	// Chain position indicator: "2/3 · agent"
	const prefix = chainPosition
		? theme.fg("dim", `${chainPosition.index}/${chainPosition.total} `) + theme.fg("dim", "· ")
		: "";

	const dot = theme.fg("accent", GLYPH_RUNNING);
	const roleText = theme.fg("toolTitle", theme.bold(role));

	c.addChild(new Text(`${prefix}${dot} ${roleText}`, 1, 0));

	if (preview) {
		c.addChild(new Text(theme.fg("dim", `  ${preview}`), 1, 0));
	}

	return c;
}

// ── Tool result rendering ──────────────────────────────────────

interface DelegateResult {
	content: Array<{ type: string; text?: string }>;
	details?: any;
	isError: boolean;
}

/**
 * Enhanced renderResult for delegate_to / delegate_async.
 * Shows status glyph, agent name, output summary, and optional expanded view.
 */
export function renderDelegateResult(
	result: DelegateResult,
	options: { expanded: boolean },
	theme: Theme,
	meta?: { agentName?: string; duration?: number; toolCount?: number },
): Container {
	const c = new Container();

	// Extract text output
	const textContent = result.content
		.filter((block) => block.type === "text" && block.text)
		.map((block) => block.text!)
		.join("\n");

	const status = result.isError ? "failed" : "completed";
	const glyph = statusGlyph(status, theme);
	const agentLabel = meta?.agentName
		? theme.fg("toolTitle", theme.bold(meta.agentName))
		: theme.fg("toolTitle", theme.bold("delegation"));

	// Build stats line: "✓ sarah · 3 tools · 12s"
	const statParts: string[] = [];
	if (meta?.toolCount && meta.toolCount > 0) {
		statParts.push(`${meta.toolCount} tool${meta.toolCount !== 1 ? "s" : ""}`);
	}
	if (meta?.duration) {
		statParts.push(fmtDuration(meta.duration));
	}
	const stats = statParts.length > 0
		? theme.fg("dim", ` · ${statParts.join(" · ")}`)
		: "";

	c.addChild(new Text(`${glyph} ${agentLabel}${stats}`, 1, 0));

	// First meaningful line as summary
	const firstLine = textContent.split("\n").find((l) => l.trim()) ?? "";
	const summary = truncLine(firstLine, 120);
	if (summary && !options.expanded) {
		c.addChild(new Text(theme.fg("dim", `  ${summary}`), 1, 0));
	}

	// Error output
	if (result.isError && textContent) {
		const errorLines = textContent.split("\n").slice(0, 3);
		for (const line of errorLines) {
			c.addChild(new Text(theme.fg("error", `  ${truncLine(line, 120)}`), 1, 0));
		}
	}

	// Expanded view: show structured output
	if (options.expanded && textContent.length > 0) {
		c.addChild(new Spacer(1));
		const lines = textContent.split("\n").slice(0, 30);
		for (const line of lines) {
			c.addChild(new Text(theme.fg("dim", `  ${truncLine(line, 200)}`), 1, 0));
		}
		if (textContent.split("\n").length > 30) {
			c.addChild(new Text(theme.fg("dim", "  …"), 1, 0));
		}
	}

	return c;
}

/**
 * Render a chain visualization showing sequential delegations.
 * Displays: "sarah → jake → marcus" with per-agent status.
 */
export function renderChainVisualization(
	chain: Array<{ name: string; status: string }>,
	theme: Theme,
): Container {
	const c = new Container();

	const header = theme.fg("dim", theme.bold("chain"));
	const parts = chain.map((entry) => {
		const glyph = statusGlyph(entry.status, theme);
		return `${glyph} ${theme.bold(entry.name)}`;
	});
	const joined = parts.join(theme.fg("dim", " → "));

	c.addChild(new Text(`  ${header} ${joined}`, 1, 0));
	return c;
}
