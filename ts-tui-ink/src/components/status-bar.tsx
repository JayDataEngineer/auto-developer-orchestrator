/**
 * StatusBar — bottom status bar with model and context usage.
 *
 * Left: model name. Right: context usage (tokens + %).
 */

import React from "react";
import { Box, Text, useStdout } from "ink";
import { usePuxStore } from "@pux/shared";

interface StatusBarProps {
	model: string;
	project: string;
}

export function StatusBar({ model }: StatusBarProps) {
	const contextMetrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);
	const { stdout } = useStdout();
	const cols = stdout?.columns ?? 80;

	// Build right side: context usage
	let right = "";
	if (contextMetrics) {
		const tokens = formatTokens(contextMetrics.contextTokens);
		const pct = Math.round(contextMetrics.contextUtil * 100);
		right = `${tokens} (${pct}%)`;
	}
	if (compacting) {
		right += " compacting";
	}

	// Pad to fill width
	const label = model || "no model";
	const leftStr = ` ${label} `;
	const rightStr = right ? ` ${right} ` : "";
	const contentLen = leftStr.length + rightStr.length;
	const padding = Math.max(0, cols - contentLen);
	const padded = rightStr
		? `${leftStr}${" ".repeat(padding)}${rightStr}`
		: `${leftStr}`;

	return (
		<Box>
			<Text dimColor>{padded}</Text>
		</Box>
	);
}

/** Format token count: 584321 → "584.3K", 1.2M → "1.2M" */
function formatTokens(n: number): string {
	if (n >= 1_000_000) {
		return `${(n / 1_000_000).toFixed(1)}M`;
	}
	if (n >= 1_000) {
		return `${(n / 1_000).toFixed(1)}K`;
	}
	return String(n);
}
