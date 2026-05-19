/**
 * StatusBar — bottom status bar with model, agent pills, and context usage.
 *
 * Left: model name + agent pills. Right: context usage (tokens + %).
 * Agent pills show running agents: ○ marcus (3 tools · 12.3s) · ○ sarah (1 tool)
 */

import React from "react";
import { Box, Text } from "ink";
import { usePuxStore } from "@pux/shared";
import { useTerminalSize } from "../use-terminal-size.js";
import { useColors, symbols } from "../theme.js";

export function StatusBar() {
	const activeModel = usePuxStore((s) => s.activeModel);
	const lastUsage = usePuxStore((s) => s.lastUsage);
	const contextMetrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);
	const agents = usePuxStore((s) => s.agents);
	const { cols } = useTerminalSize();
	const colors = useColors();

	// Running agents for pills
	const runningAgents = [...agents.values()].filter((a) => a.status === "running");

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
	const model = activeModel || lastUsage?.model || "";
	const modelLabel = model || "no model";
	const rightStr = right ? ` ${right} ` : "";

	return (
		<Box flexDirection="column">
			{/* Agent pills row — only when agents exist */}
			{runningAgents.length > 0 && (
				<Box>
					<Text dimColor>
						{" "}
						{runningAgents.map((a, i) => {
							const duration = a.endedAt
								? `${((a.endedAt - a.startedAt) / 1000).toFixed(1)}s`
								: `${((Date.now() - a.startedAt) / 1000).toFixed(1)}s`;
							const pill = `${symbols.toolRunning} ${a.agentName} (${a.toolCalls.length} tool${a.toolCalls.length !== 1 ? "s" : ""} ${symbols.dot} ${duration})`;
							return i < runningAgents.length - 1
								? `${pill} ${symbols.dot} `
								: pill;
						}).join("")}
						{agents.size > runningAgents.length
							? ` ${symbols.dot} ${agents.size - runningAgents.length} done`
							: ""}
					</Text>
				</Box>
			)}

			{/* Main status line */}
			<Box>
				<Text dimColor>{` ${modelLabel} `}</Text>
				{runningAgents.length > 0 && (
					<Text color={colors.running} dimColor>
						{symbols.dot} Ctrl+O agents
					</Text>
				)}
				{rightStr && (
					<Text dimColor>
						{" ".repeat(Math.max(0, cols - modelLabel.length - rightStr.length - 20))}
						{rightStr}
					</Text>
				)}
			</Box>
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
