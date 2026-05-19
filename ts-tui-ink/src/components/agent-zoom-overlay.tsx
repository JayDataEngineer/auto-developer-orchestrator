/**
 * AgentZoomOverlay — full-screen detail view for a single subagent.
 *
 * Triggered when zoomedAgentId is set in the Zustand store.
 * Shows complete tool call list, result preview, and error details.
 * OpenCode-style "zoom in" on an agent block.
 */

import React, { useState, useEffect, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore, getToolArgPreview } from "@pux/shared";
import { useColors, symbols, BLOCKQUOTE_BAR, BLACK_CIRCLE } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

export function AgentZoomOverlay() {
	const zoomedAgentId = usePuxStore((s) => s.zoomedAgentId);
	const agents = usePuxStore((s) => s.agents);
	const setZoomedAgent = usePuxStore((s) => s.setZoomedAgent);
	const colors = useColors();
	const { rows, cols } = useTerminalSize();

	const [scrollOffset, setScrollOffset] = useState(0);

	// Find the agent
	const agent = zoomedAgentId
		? agents.get(zoomedAgentId)
		: undefined;

	// Reset scroll when agent changes
	useEffect(() => {
		setScrollOffset(0);
	}, [zoomedAgentId]);

	// Tick every second while agent is running
	const [, setTick] = useState(0);
	useEffect(() => {
		if (!agent || agent.status !== "running") return;
		const timer = setInterval(() => setTick((t) => t + 1), 1000);
		return () => clearInterval(timer);
	}, [agent?.status]);

	// Keyboard handling
	useInput(useCallback((input: string, key: any) => {
		if (key.escape) {
			setZoomedAgent(null);
			return;
		}
		if (key.upArrow) {
			setScrollOffset((prev) => Math.max(0, prev - 3));
			return;
		}
		if (key.downArrow) {
			setScrollOffset((prev) => prev + 3);
			return;
		}
	}, [setZoomedAgent]));

	if (!agent) {
		return (
			<Box flexDirection="column" flexGrow={1} paddingX={2} paddingY={1}>
				<Text color={colors.error}>Agent not found</Text>
				<Text dimColor>Press Esc to go back</Text>
			</Box>
		);
	}

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

	const toolCalls = agent.toolCalls;
	const contentLines: React.ReactNode[] = [];

	// Build all content lines for scrolling
	// Tool calls section
	toolCalls.forEach((tc, i) => {
		const argPreview = getToolArgPreview(tc.toolName, tc.args as Record<string, unknown> | undefined, 60);
		const tcDuration = tc.endedAt
			? `${((tc.endedAt - tc.timestamp) / 1000).toFixed(1)}s`
			: "";
		contentLines.push(
			<Box key={`tc-${i}`} paddingLeft={1}>
				<Text color={tc.isError ? colors.error : tc.endedAt ? colors.success : colors.running}>
					{tc.isError ? symbols.toolError : tc.endedAt ? symbols.toolDone : symbols.toolRunning}
				</Text>
				<Text> </Text>
				<Text bold>{tc.toolName}</Text>
				{argPreview && <Text color="gray"> {argPreview}</Text>}
				{tcDuration && <Text color="gray"> {symbols.dot} {tcDuration}</Text>}
			</Box>
		);
	});

	// Result section
	const resultLines = agent.result ? agent.result.split("\n") : [];
	if (resultLines.length > 0) {
		contentLines.push(
			<Box key="result-header" marginTop={1}>
				<Text bold color={colors.brand}>Result</Text>
			</Box>
		);
		resultLines.forEach((line, i) => {
			contentLines.push(
				<Box key={`result-${i}`} paddingLeft={1}>
					<Text color="gray">{BLOCKQUOTE_BAR} </Text>
					<Text dimColor>{line}</Text>
				</Box>
			);
		});
	}

	// Error section
	if (agent.error) {
		contentLines.push(
			<Box key="error-header" marginTop={1}>
				<Text bold color={colors.error}>Error</Text>
			</Box>
		);
		contentLines.push(
			<Box key="error-body" paddingLeft={1}>
				<Text color={colors.error}>{agent.error}</Text>
			</Box>
		);
	}

	// Apply scroll offset
	const visibleContent = contentLines.slice(scrollOffset, scrollOffset + rows - 8);
	const maxScroll = Math.max(0, contentLines.length - (rows - 8));

	return (
		<Box flexDirection="column" flexGrow={1}>
			{/* Header */}
			<Box paddingX={1}>
				<Text color={statusColor}>{statusIcon} </Text>
				<Text bold color={colors.brand}>{agent.agentName}</Text>
				<Text color="gray"> {symbols.dot} </Text>
				<Text color={statusColor}>{agent.status}</Text>
				<Text color="gray"> {symbols.dot} {duration} {symbols.dot} {toolCalls.length} tool{toolCalls.length !== 1 ? "s" : ""}</Text>
			</Box>

			{/* Task */}
			<Box paddingX={1} marginBottom={1}>
				<Text dimColor>{BLOCKQUOTE_BAR} {agent.task}</Text>
			</Box>

			<Text dimColor>{"─".repeat(Math.min(cols, 80))}</Text>

			{/* Scrollable content */}
			<Box flexDirection="column" flexGrow={1} paddingX={1}>
				{visibleContent}
			</Box>

			{/* Footer */}
			<Text dimColor>{"─".repeat(Math.min(cols, 80))}</Text>
			<Box paddingX={1}>
				<Text dimColor>
					<Text bold>Esc</Text> back
					{maxScroll > 0 && <> <Text bold>Up/Down</Text> scroll ({scrollOffset}/{maxScroll})</>}
				</Text>
			</Box>
		</Box>
	);
}
