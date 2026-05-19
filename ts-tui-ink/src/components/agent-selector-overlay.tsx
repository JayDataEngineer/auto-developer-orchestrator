/**
 * AgentSelectorOverlay — quick picker for zooming into a subagent.
 *
 * Triggered by Ctrl+O. Shows a compact list of agents (running first),
 * arrows to navigate, Enter to zoom, Esc to close.
 * Fits the OpenCode model: visual indicator + single key to inspect.
 */

import React, { useState, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore, getToolArgPreview } from "@pux/shared";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

export function AgentSelectorOverlay() {
	const agents = usePuxStore((s) => s.agents);
	const setZoomedAgent = usePuxStore((s) => s.setZoomedAgent);
	const closeAgentSelector = usePuxStore((s) => s.closeAgentSelector);
	const colors = useColors();
	const { rows, cols } = useTerminalSize();
	const [selectedIdx, setSelectedIdx] = useState(0);

	// Sorted: running first, then by start time descending
	const agentList = [...agents.values()].sort((a, b) => {
		if (a.status === "running" && b.status !== "running") return -1;
		if (a.status !== "running" && b.status === "running") return 1;
		return b.startedAt - a.startedAt;
	});

	useInput(useCallback((input: string, key: any) => {
		if (key.escape) {
			closeAgentSelector();
			return;
		}
		if (agentList.length === 0) return;
		if (key.upArrow) {
			setSelectedIdx((prev) => Math.max(0, prev - 1));
			return;
		}
		if (key.downArrow) {
			setSelectedIdx((prev) => Math.min(agentList.length - 1, prev + 1));
			return;
		}
		if (key.return) {
			const agent = agentList[selectedIdx];
			if (agent) {
				closeAgentSelector();
				setZoomedAgent(agent.agentId);
			}
			return;
		}
	}, [agentList, selectedIdx, closeAgentSelector, setZoomedAgent]));

	// Clamp selected index
	const clampedIdx = Math.min(selectedIdx, Math.max(0, agentList.length - 1));

	if (agentList.length === 0) {
		return (
			<Box flexDirection="column" flexGrow={1} paddingX={2} paddingY={1}>
				<Text color={colors.brand} bold>Agents</Text>
				<Box marginTop={1}>
					<Text dimColor>No agents yet. Agents appear when the CTO delegates tasks.</Text>
				</Box>
				<Box marginTop={1}>
					<Text dimColor><Text bold>Esc</Text> to close</Text>
				</Box>
			</Box>
		);
	}

	// Limit visible items
	const maxVisible = rows - 8;
	const startIdx = Math.max(0, Math.min(clampedIdx - Math.floor(maxVisible / 2), agentList.length - maxVisible));
	const visibleAgents = agentList.slice(startIdx, startIdx + maxVisible);

	return (
		<Box flexDirection="column" flexGrow={1}>
			{/* Header */}
			<Box paddingX={1}>
				<Text bold color={colors.brand}>Agents</Text>
				<Text color="gray"> {symbols.dot} {agentList.length} agent{agentList.length !== 1 ? "s" : ""}</Text>
			</Box>
			<Text dimColor>{"─".repeat(Math.min(cols, 80))}</Text>

			{/* Agent list */}
			<Box flexDirection="column" flexGrow={1} paddingX={1}>
				{visibleAgents.map((agent, i) => {
					const globalIdx = startIdx + i;
					const isSelected = globalIdx === clampedIdx;

					const statusColor = agent.status === "running"
						? colors.running
						: agent.status === "error"
							? colors.error
							: colors.success;
					const statusIcon = agent.status === "running"
						? symbols.toolRunning
						: agent.status === "error"
							? symbols.toolError
							: symbols.toolDone;

					const duration = agent.endedAt
						? `${((agent.endedAt - agent.startedAt) / 1000).toFixed(1)}s`
						: `${((Date.now() - agent.startedAt) / 1000).toFixed(1)}s`;

					// Last tool preview
					const lastTool = agent.toolCalls.length > 0
						? agent.toolCalls[agent.toolCalls.length - 1]
						: null;
					const lastToolPreview = lastTool
						? `${lastTool.toolName}${lastTool.args ? ` ${getToolArgPreview(lastTool.toolName, lastTool.args as Record<string, unknown> | undefined, 30)}` : ""}`
						: "waiting...";

					return (
						<Box key={agent.agentId} flexDirection="column">
							<Box>
								<Text color={isSelected ? colors.brand : "gray"}>
									{isSelected ? ">" : " "}
								</Text>
								<Text color={statusColor}>{statusIcon} </Text>
								<Text bold={isSelected} color={isSelected ? colors.brand : undefined}>
									{agent.agentName}
								</Text>
								<Text color="gray"> {symbols.dot} </Text>
								<Text color={statusColor}>{duration}</Text>
								<Text color="gray"> {symbols.dot} </Text>
								<Text dimColor>{agent.toolCalls.length} tool{agent.toolCalls.length !== 1 ? "s" : ""}</Text>
								<Text color="gray"> {symbols.dot} </Text>
								<Text dimColor>{lastToolPreview}</Text>
							</Box>
							{isSelected && (
								<Box paddingLeft={2}>
									<Text dimColor>{BLOCKQUOTE_BAR} {agent.task.slice(0, 70)}{agent.task.length > 70 ? "..." : ""}</Text>
								</Box>
							)}
						</Box>
					);
				})}
			</Box>

			{/* Footer */}
			<Text dimColor>{"─".repeat(Math.min(cols, 80))}</Text>
			<Box paddingX={1}>
				<Text dimColor>
					<Text bold>Up/Down</Text> navigate <Text bold>Enter</Text> zoom <Text bold>Esc</Text> close
				</Text>
			</Box>
		</Box>
	);
}
