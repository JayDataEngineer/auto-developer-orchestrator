/**
 * StatusBar — bottom status bar with model, project, agent pills, and context usage.
 *
 * Layout: [model] · [project] · [indicators] ... [context]
 * Agent pills show running agents: ○ marcus (3 tools · 12.3s)
 */

import React, { useState, useEffect } from "react";
import { Box, Text } from "ink";
import { usePuxStore } from "@pux/shared";
import { useTerminalSize } from "../use-terminal-size.js";
import { useColors, symbols } from "../theme.js";

export function StatusBar() {
	const activeModel = usePuxStore((s) => s.activeModel);
	const modelList = usePuxStore((s) => s.modelList);
	const lastUsage = usePuxStore((s) => s.lastUsage);
	const contextMetrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);
	const agents = usePuxStore((s) => s.agents);
	const backgroundTasks = usePuxStore((s) => s.backgroundTasks);
	const foregroundTaskId = usePuxStore((s) => s.foregroundTaskId);
	const projectName = usePuxStore((s) => s.activeProject);
	const { cols } = useTerminalSize();
	const colors = useColors();

	// Tick for agent duration updates
	const [, setTick] = useState(0);
	useEffect(() => {
		const timer = setInterval(() => setTick((t) => t + 1), 1000);
		return () => clearInterval(timer);
	}, []);

	// Running agents for pills
	const runningAgents = agents ? [...agents.values()].filter((a) => a.status === "running") : [];

	// Background task counts
	const bgTasks = backgroundTasks ? [...backgroundTasks.values()] : [];
	const bgRunning = bgTasks.filter((t) => t.status === "running" || t.status === "backgrounded").length;
	const bgCompleted = bgTasks.filter((t) => t.status === "completed" || t.status === "failed").length;

	// Build right side: context usage
	let contextStr = "";
	let contextPct = 0;
	if (contextMetrics) {
		contextPct = Math.round(contextMetrics.contextUtil * 100);
		const tokens = formatTokens(contextMetrics.contextTokens);
		contextStr = compacting ? `compacting` : `${tokens} ${contextPct}%`;
	} else if (compacting) {
		contextStr = "compacting";
	}

	// Clean model name: strip provider prefix, use display name if available
	const rawModel = activeModel || lastUsage?.model || "";
	const modelEntry = modelList?.find((m: any) => m.id === rawModel);
	const modelLabel = modelEntry?.name
		? modelEntry.name.replace(/\s*\(local\)\s*$/, "")
		: rawModel
			? cleanModelName(rawModel)
			: "no model";

	// Build left segments
	const leftParts: { text: string; color?: string; bold?: boolean }[] = [];
	leftParts.push({ text: modelLabel, color: colors.brand });
	if (projectName) {
		leftParts.push({ text: symbols.dot });
		// Truncate long project names
		const displayName = projectName.length > 20 ? projectName.slice(0, 17) + "..." : projectName;
		leftParts.push({ text: displayName });
	}
	if (foregroundTaskId) {
		leftParts.push({ text: symbols.dot });
		leftParts.push({ text: "Ctrl+B background", color: "yellow" });
	}
	if (bgRunning > 0) {
		leftParts.push({ text: symbols.dot });
		leftParts.push({ text: `${bgRunning} bg`, color: "yellow" });
	}
	if (bgCompleted > 0) {
		leftParts.push({ text: symbols.dot });
		leftParts.push({ text: `${bgCompleted} done`, color: "green" });
	}

	// Calculate left text length for padding
	// Each part: text + space separator (except first)
	const leftText = leftParts.map((p, i) => i === 0 ? p.text : ` ${p.text}`).join("");
	const leftLen = leftText.length;

	// Build right side as a single string for accurate width calculation
	const barFilled = contextMetrics ? Math.max(0, Math.round((contextPct / 100) * 10)) : 0;
	const barEmpty = contextMetrics ? Math.max(0, 10 - barFilled) : 0;
	const rightText = contextMetrics && !compacting
		? ` ${"█".repeat(barFilled)}${"░".repeat(barEmpty)} ${contextStr} `
		: compacting
			? ` ${contextStr} `
			: "";
	const rightLen = rightText.length;

	// Padding fills the gap; if negative, clamp to 1 (content may overlap but won't crash)
	const padLen = Math.max(1, cols - 1 - leftLen - rightLen);

	return (
		<Box flexDirection="column">
			{/* Agent pills row — only when agents exist */}
			{runningAgents.length > 0 && (
				<Box paddingX={1}>
					<Text color={colors.running}>
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
				{/* Left side */}
				{leftParts.map((p, i) => (
					<Text key={i} color={p.color} bold={p.bold}>
						{i === 0 ? ` ${p.text}` : ` ${p.text}`}
					</Text>
				))}
				{/* Spacer */}
				<Text>{" ".repeat(padLen)}</Text>
				{/* Right side: context bar */}
				{contextMetrics && !compacting && (
					<Text>
						<Text color={contextPct > 75 ? "red" : contextPct > 50 ? "yellow" : colors.success}>
							{rightText.slice(0, barFilled + 1)}
						</Text>
						<Text color="blackBright">
							{rightText.slice(barFilled + 1, barFilled + 1 + barEmpty)}
						</Text>
						<Text color={contextPct > 75 ? "red" : "gray"}>
							{rightText.slice(barFilled + 1 + barEmpty)}
						</Text>
					</Text>
				)}
				{compacting && (
					<Text color="yellow">{rightText}</Text>
				)}
			</Box>
		</Box>
	);
}

/** Clean model name: strip provider prefix, shorten common names */
function cleanModelName(raw: string): string {
	// Remove openrouter/org prefix: "deepseek/deepseek-v4-flash" → "deepseek-v4-flash"
	let name = raw.replace(/^[a-z][-a-z0-9]*\//, "");
	// Remove -it, -instruct suffixes
	name = name.replace(/-it$/, "").replace(/-instruct$/, "");
	// Shorten common patterns
	name = name.replace(/^gemma-4-26b.*$/, "Gemma 4 27B");
	name = name.replace(/^qwen[^-]*-?(\d+)/, "Qwen $1");
	name = name.replace(/^deepseek-v(\d+)(.*)/, "DeepSeek V$1$2");
	// Capitalize first letter
	return name.charAt(0).toUpperCase() + name.slice(1);
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
