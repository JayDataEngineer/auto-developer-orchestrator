/**
 * PUX Context Metrics Extension
 *
 * Shows context utilization and compaction status in the TUI footer.
 * Uses ctx.ui.setStatus() to add a status line showing:
 * - Context utilization bar and percentage
 * - Compaction events (micro/full) with token savings
 *
 * Reads context usage from ctx.getContextUsage() (pi-mono built-in)
 * and augments with Go backend metrics when available.
 */

import type { ExtensionAPI } from "../../src/core/extensions/types.js";

function formatTokens(count: number): string {
	if (count < 1000) return count.toString();
	if (count < 10000) return `${(count / 1000).toFixed(1)}k`;
	if (count < 1000000) return `${Math.round(count / 1000)}k`;
	return `${(count / 1000000).toFixed(1)}M`;
}

function utilizationBar(percent: number, width: number = 10): string {
	const filled = Math.round(percent * width);
	const empty = width - filled;
	if (percent > 0.9) return "█".repeat(filled) + "░".repeat(empty);
	if (percent > 0.7) return "▓".repeat(filled) + "░".repeat(empty);
	return "▒".repeat(filled) + "░".repeat(empty);
}

export default function registerPuxContextMetricsExtension(pi: ExtensionAPI): void {
	let lastCompactionType: string | null = null;

	// Update context metrics after each turn
	pi.on("turn_end", (_event, ctx) => {
		const usage = ctx.getContextUsage();
		if (!usage || usage.percent === null) {
			ctx.ui.setStatus("context", undefined);
			return;
		}

		const pct = usage.percent;
		const bar = utilizationBar(pct);
		const tokensStr = usage.tokens !== null ? formatTokens(usage.tokens) : "?";
		const windowStr = formatTokens(usage.contextWindow);

		let status = `${bar} ${tokensStr}/${windowStr} (${(pct * 100).toFixed(0)}%)`;
		if (lastCompactionType) {
			status += ` [last: ${lastCompactionType}]`;
		}

		ctx.ui.setStatus("context", status);
	});

	// Track compaction events
	pi.on("session_compact", (_event, ctx) => {
		lastCompactionType = "session";

		const usage = ctx.getContextUsage();
		if (usage && usage.percent !== null) {
			const tokensStr = usage.tokens !== null ? formatTokens(usage.tokens) : "?";
			const windowStr = formatTokens(usage.contextWindow);
			const bar = utilizationBar(usage.percent);
			ctx.ui.setStatus("context", `${bar} ${tokensStr}/${windowStr} (${(usage.percent * 100).toFixed(0)}%) [compacted]`);
		}
	});

	// Track Go backend compaction events (via agent_start which fires after compaction_end)
	pi.on("agent_end", (_event, ctx) => {
		// Check if Go backend sent compaction metrics
		const session = ctx as any;
		const metrics = session._lastCompactionMetrics || session.session?._lastCompactionMetrics;
		if (metrics && metrics.compactionType) {
			lastCompactionType = metrics.compactionType;

			const tokensStr = formatTokens(metrics.contextTokens || 0);
			const windowStr = formatTokens(metrics.contextSize || 0);
			const pct = metrics.contextUtil || 0;
			const bar = utilizationBar(pct);

			ctx.ui.setStatus("context", `${bar} ${tokensStr}/${windowStr} (${(pct * 100).toFixed(0)}%) [${metrics.compactionType} compact]`);
		}
	});
}
