/**
 * StatusBar — bottom status bar with model, tokens, timing, and view.
 *
 * Left: model name + timing. Right: token usage (in/out), context %, active agents.
 * Uses both usePuxStore (for backend metrics) and useAuiState (for runtime timing).
 */

import React from "react";
import { Box, Text, useStdout } from "ink";
import { usePuxStore } from "@pux/shared";
import { useAuiState } from "@assistant-ui/react-ink";
import { symbols } from "../theme.js";

interface StatusBarProps {
	model: string;
	project: string;
}

export function StatusBar({ model }: StatusBarProps) {
	const lastUsage = usePuxStore((s) => s.lastUsage);
	const contextMetrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);
	const agents = usePuxStore((s) => s.agents);
	const activeView = usePuxStore((s) => s.activeTuiView);
	const showProviders = usePuxStore((s) => s.showProvidersOverlay);
	const { stdout } = useStdout();
	const cols = stdout?.columns ?? 80;

	// Build left side: model name + view indicator
	const viewLabels: Record<string, string> = {
		chat: "Chat",
		agents: "Agents",
		tools: "Tools",
		files: "Files",
		conversations: "History",
	};
	const left = showProviders
		? `${model} ${symbols.dot} Providers`
		: `${model} ${symbols.dot} ${viewLabels[activeView] || "Chat"}`;

	// Build right side: token usage + timing + agents
	let right = "";
	if (lastUsage) {
		const inK = lastUsage.input > 1000 ? `${(lastUsage.input / 1000).toFixed(1)}k` : String(lastUsage.input);
		const outK = lastUsage.output > 1000 ? `${(lastUsage.output / 1000).toFixed(1)}k` : String(lastUsage.output);
		right = `in:${inK} out:${outK}`;
	}
	if (contextMetrics) {
		const pct = Math.round(contextMetrics.contextUtil * 100);
		right += ` ${symbols.dot} ctx:${pct}%`;
	}
	if (compacting) {
		right += ` ${symbols.dot} compacting`;
	}

	// Running agent count
	const runningAgents = [...agents.values()].filter((a) => a.status === "running").length;
	if (runningAgents > 0) {
		right += ` ${symbols.dot} ${runningAgents} agents`;
	}

	// Pad to fill width
	const content = ` ${left} ${right} `;
	const totalLen = stripAnsiLen(content);
	const padding = Math.max(0, cols - totalLen);
	const padded = right
		? ` ${left}${" ".repeat(padding)}${right} `
		: ` ${left} `;

	return (
		<Box>
			<Text dimColor>{padded}</Text>
		</Box>
	);
}

/** Get visible length of string (excluding ANSI codes) */
function stripAnsiLen(str: string): number {
	// eslint-disable-next-line no-control-regex
	return str.replace(/\x1b\[[0-9;]*m/g, "").length;
}
