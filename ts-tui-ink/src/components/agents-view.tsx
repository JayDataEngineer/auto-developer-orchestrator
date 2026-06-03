/**
 * AgentsView — live subagent monitoring panel.
 *
 * Shows a tree of running/completed/failed agents with:
 * - Agent name and task
 * - Status icon and duration
 * - Tool call count and result preview
 * - Collapsible detail for each agent
 *
 * Data comes from the Zustand store, populated by subagent SSE events
 * in pux-chat-adapter.ts.
 */

import React, { useState, useEffect } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore, type AgentState } from "@pux/shared";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

export function AgentsView() {
	const agents = usePuxStore((s) => s.agents);
	const setZoomedAgent = usePuxStore((s) => s.setZoomedAgent);
	const { rows } = useTerminalSize();
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [expanded, setExpanded] = useState<Set<string>>(new Set());
	const colors = useColors();

	// Force re-render every second to update running durations
	const [, setTick] = useState(0);
	useEffect(() => {
		const timer = setInterval(() => setTick((t) => t + 1), 1000);
		return () => clearInterval(timer);
	}, []);

	const agentList = [...agents.values()].sort((a, b) => {
		// Running first, then by start time descending
		if (a.status === "running" && b.status !== "running") return -1;
		if (a.status !== "running" && b.status === "running") return 1;
		return b.startedAt - a.startedAt;
	});

	const running = agentList.filter((a) => a.status === "running").length;
	const completed = agentList.filter((a) => a.status === "complete").length;
	const failed = agentList.filter((a) => a.status === "error").length;

	// Keyboard navigation
	useInput((input: string, key: any) => {
		if (key.escape) {
			usePuxStore.getState().setTuiView("chat");
			return;
		}
		if (agentList.length === 0) return;
		if (key.upArrow) {
			setSelectedIdx(Math.max(0, selectedIdx - 1));
			return;
		}
		if (key.downArrow) {
			setSelectedIdx(Math.min(agentList.length - 1, selectedIdx + 1));
			return;
		}
		if (input === " ") {
			const agent = agentList[selectedIdx];
			if (agent) {
				setExpanded((prev) => {
					const next = new Set(prev);
					if (next.has(agent.agentId)) {
						next.delete(agent.agentId);
					} else {
						next.add(agent.agentId);
					}
					return next;
				});
			}
			return;
		}
		if (key.return) {
			const agent = agentList[selectedIdx];
			if (agent) {
				setZoomedAgent(agent.agentId);
			}
			return;
		}
	});

	if (agentList.length === 0) {
		return (
			<Box flexDirection="column" paddingX={2} paddingY={1}>
				<Text bold color={colors.brand}>Subagents</Text>
				<Box marginTop={1}>
					<Text color={colors.textMuted}>No agents yet. Agents appear when the CTO delegates tasks.</Text>
				</Box>
				<Box marginTop={1}>
					<Text color={colors.textMuted}><Text bold>Esc</Text> back to chat</Text>
				</Box>
			</Box>
		);
	}

	// Limit visible agents to viewport with scroll window
	const maxVisible = Math.max(3, rows - 6);
	const scrollOffset = Math.max(0, Math.min(
		selectedIdx - Math.floor(maxVisible / 2),
		agentList.length - maxVisible,
	));
	const visibleAgents = agentList.slice(scrollOffset, scrollOffset + maxVisible);

	return (
		<Box flexDirection="column" paddingX={1}>
			{/* Header */}
			<Box marginBottom={1}>
				<Text bold color={colors.brand}>Subagents</Text>
				<Text color={colors.textMuted}> {symbols.dot} </Text>
				{running > 0 && <Text color={colors.running}>{running} running </Text>}
				{completed > 0 && <Text color={colors.success}>{completed} done </Text>}
				{failed > 0 && <Text color={colors.error}>{failed} failed </Text>}
			</Box>

			{/* Agent list */}
			{visibleAgents.map((agent, i) => (
				<AgentCard
					key={agent.agentId}
					agent={agent}
					isSelected={(i + scrollOffset) === selectedIdx}
					isExpanded={expanded.has(agent.agentId)}
				/>
			))}

			{agentList.length > maxVisible && (
				<Text color={colors.textMuted}>
					... {scrollOffset + 1}–{scrollOffset + visibleAgents.length} of {agentList.length}
				</Text>
			)}

			{/* Controls hint */}
			<Box marginTop={1}>
				<Text dimColor>
					<Text bold>Up/Down</Text> navigate <Text bold>Enter</Text> zoom <Text bold>Space</Text> expand <Text bold>Esc</Text> back
				</Text>
			</Box>
		</Box>
	);
}

function AgentCard({
	agent,
	isSelected,
	isExpanded,
}: {
	agent: AgentState;
	isSelected: boolean;
	isExpanded: boolean;
}) {
	const colors = useColors();
	const statusIcon = agent.status === "running"
		? symbols.toolRunning
		: agent.status === "error"
			? symbols.toolError
			: symbols.toolDone;
	const statusColor = agent.status === "running"
		? colors.running
		: agent.status === "error"
			? colors.error
			: colors.success;

	const duration = agent.endedAt
		? `${((agent.endedAt - agent.startedAt) / 1000).toFixed(1)}s`
		: `${((Date.now() - agent.startedAt) / 1000).toFixed(1)}s`;

	return (
		<Box flexDirection="column" marginBottom={1}>
			<Box>
				<Text color={statusColor}>{statusIcon} </Text>
				<Text bold color={isSelected ? colors.brand : undefined}>
					{agent.agentName}
				</Text>
				<Text color={colors.textMuted}> {symbols.dot} </Text>
				<Text color={statusColor}>{duration}</Text>
				<Text color={colors.textMuted}> {symbols.dot} </Text>
				<Text dimColor>{agent.toolCalls.length} tools</Text>
			</Box>

			{/* Task preview */}
			<Text color={colors.textMuted}>
				{"  "}{BLOCKQUOTE_BAR} {agent.task.slice(0, 80)}
				{agent.task.length > 80 ? "..." : ""}
			</Text>

			{/* Expanded detail */}
			{isExpanded && (
				<Box flexDirection="column" paddingLeft={3} marginTop={0}>
					{/* Tool calls */}
					{agent.toolCalls.length > 0 && (
						<Box flexDirection="column">
							{agent.toolCalls.slice(0, 10).map((tc, i) => {
								const done = !!tc.endedAt;
								const icon = tc.isError ? "✕" : done ? "●" : "○";
								const iconColor = tc.isError ? colors.error : done ? colors.success : colors.running;
								const label = toolLabel(tc);
								return (
									<Box key={i}>
										<Text color={iconColor}>  {icon} </Text>
										<Text color={colors.textMuted}>{tc.toolName}</Text>
										{label && <Text color={colors.textMuted}> {label}</Text>}
										{tc.endedAt && (
											<Text color={colors.textMuted}> · {((tc.endedAt - tc.timestamp) / 1000).toFixed(1)}s</Text>
										)}
									</Box>
								);
							})}
							{agent.toolCalls.length > 10 && (
								<Text color={colors.textMuted}>
									{"  "}... +{agent.toolCalls.length - 10} more
								</Text>
							)}
						</Box>
					)}

					{/* Result */}
					{agent.result && agent.status === "complete" && (
						<Box flexDirection="column" marginTop={0}>
							<Text dimColor bold>Result:</Text>
							{agent.result.split("\n").slice(0, 5).map((line: string, i: number) => (
								<Text key={i} dimColor>
									{"  "}{BLOCKQUOTE_BAR} {line.slice(0, 100)}
								</Text>
							))}
							{agent.result.split("\n").length > 5 && (
								<Text color={colors.textMuted}>
									{"  "}... +{agent.result.split("\n").length - 5} more lines
								</Text>
							)}
						</Box>
					)}

					{/* Error */}
					{agent.error && (
						<Box marginTop={0}>
							<Text color={colors.error}>
								{symbols.cross} {agent.error.slice(0, 100)}
							</Text>
						</Box>
					)}
				</Box>
			)}
		</Box>
	);
}

/** Extract a short label from tool call args — URL, query, file path, etc. */
function toolLabel(tc: AgentState["toolCalls"][number]): string {
	const args = tc.args as Record<string, unknown> | undefined;
	if (!args || typeof args !== "object") return "";
	// URL-based tools
	const url = args.url as string | undefined;
	if (url) return url.replace(/^https?:\/\//, "").slice(0, 50);
	// Search/research tools
	const query = (args.query || args.search_query || args.keywords) as string | undefined;
	if (query) return query.slice(0, 60);
	// File-based tools
	const path = (args.path || args.file_path || args.filename) as string | undefined;
	if (path) return path.replace(/^\/sandbox\/workspace\//, "").slice(0, 60);
	// Bash commands
	const cmd = (args.command || args.cmd) as string | undefined;
	if (cmd) return cmd.slice(0, 60);
	// Generic: first string arg
	const first = Object.values(args).find((v) => typeof v === "string") as string | undefined;
	if (first) return first.slice(0, 60);
	return "";
}