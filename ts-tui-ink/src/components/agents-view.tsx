/**
 * AgentsView — subagent conversation panel.
 *
 * Shows agents in a list. Expanded agents render conversation-style
 * blocks with left-border separation, matching OpenCode's tool rendering:
 *
 * ┃ researcher · 115.5s
 * ┃ Task: Find the weather in New York
 * ┃  └ Bash: curl wttr.in/new+york · 1.2s
 * ┃  └ Search: weather NYC · 0.8s
 * ┃ Result text here...
 *
 * Data from Zustand store + transcript fetch for completed agents.
 */

import React, { useState, useEffect, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import {
	usePuxStore,
	type AgentState,
	getToolArgPreview,
	apiUrl,
	getFetch,
} from "@pux/shared";
import { useColors, symbols } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

// ── StoredMessage shape (matches pux-history-adapter.ts) ──

interface StoredMessage {
	id: number;
	role: string;
	content: string;
	text: string;
	thinking: string;
	toolCalls: string;
	toolCallId: string;
	toolName: string;
	createdAt: string;
}

// ── Agents view ──

export function AgentsView() {
	const agents = usePuxStore((s) => s.agents);
	const { rows } = useTerminalSize();
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [expanded, setExpanded] = useState<Set<string>>(new Set());
	const colors = useColors();

	// Force re-render every second for running durations
	const [, setTick] = useState(0);
	useEffect(() => {
		const timer = setInterval(() => setTick((t) => t + 1), 1000);
		return () => clearInterval(timer);
	}, []);

	const agentList = [...agents.values()].sort((a, b) => {
		if (a.status === "running" && b.status !== "running") return -1;
		if (a.status !== "running" && b.status === "running") return 1;
		return b.startedAt - a.startedAt;
	});

	const running = agentList.filter((a) => a.status === "running").length;
	const completed = agentList.filter((a) => a.status === "complete").length;
	const failed = agentList.filter((a) => a.status === "error").length;

	useInput((input: string, key: any) => {
		if (key.escape || key.rightArrow) {
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
		if (key.return || input === " ") {
			const agent = agentList[selectedIdx];
			if (agent) {
				setExpanded((prev) => {
					const next = new Set(prev);
					if (next.has(agent.agentId)) next.delete(agent.agentId);
					else next.add(agent.agentId);
					return next;
				});
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

			<Box marginTop={1}>
				<Text color={colors.textMuted}>
					<Text bold>Up/Down</Text> navigate <Text bold>Enter</Text> expand <Text bold>Esc</Text> back
				</Text>
			</Box>
		</Box>
	);
}

// ── Agent card (collapsed + expanded) ──

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
	const activeProject = usePuxStore((s) => s.activeProject);
	const [toolResultMap, setToolResultMap] = useState<Map<string, string>>(new Map());
	const [transcriptLoaded, setTranscriptLoaded] = useState(false);

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

	// Fetch transcript for completed agents when expanded
	useEffect(() => {
		if (!isExpanded || !agent.transcriptId || !activeProject || transcriptLoaded) return;
		if (agent.status === "running") return;

		const params = new URLSearchParams({
			project: activeProject,
			agentId: agent.transcriptId,
			limit: "200",
		});

		getFetch()(apiUrl(`/api/pux/history?${params}`))
			.then((resp) => resp.ok ? resp.json() : [])
			.then((data: StoredMessage[]) => {
				if (!Array.isArray(data)) return;
				const results = new Map<string, string>();
				for (const msg of data) {
					if (msg.role === "tool" && msg.toolCallId) {
						results.set(msg.toolCallId, msg.content || "");
					}
				}
				setToolResultMap(results);
				setTranscriptLoaded(true);
			})
			.catch(() => {});
	}, [isExpanded, agent.transcriptId, agent.status, activeProject, transcriptLoaded]);

	// Collapsed: one-line summary
	if (!isExpanded) {
		return (
			<Box flexDirection="column" marginBottom={1}>
				<Box>
					<Text color={statusColor}>{statusIcon} </Text>
					<Text bold color={isSelected ? colors.brand : undefined}>
						{agent.agentName}
					</Text>
					<Text color={colors.textMuted}> {symbols.dot} {agent.toolCalls.length} tools {symbols.dot} {duration}</Text>
				</Box>
				<Text color={colors.textMuted}>
					{"  "}{agent.task.slice(0, 80)}{agent.task.length > 80 ? "..." : ""}
				</Text>
			</Box>
		);
	}

	// Expanded: bordered block with conversation flow
	return (
		<Box flexDirection="column" marginBottom={1}>
			{/* Agent header */}
			<Box>
				<Text color={statusColor}>{statusIcon} </Text>
				<Text bold color={isSelected ? colors.brand : undefined}>
					{agent.agentName}
				</Text>
				<Text color={colors.textMuted}> {symbols.dot} {duration}</Text>
			</Box>

			{/* Bordered content block */}
			<Box flexDirection="column" paddingLeft={1} borderStyle="bold" borderLeft={true} borderColor={colors.textMuted}>
				{/* Task description */}
				<Text color={colors.textMuted}>
					{agent.task.slice(0, 120)}{agent.task.length > 120 ? "..." : ""}
				</Text>

				{/* Tool calls with └ nesting */}
				{agent.toolCalls.length > 0 && (
					<Box flexDirection="column" marginTop={0}>
						{agent.toolCalls.map((tc, i) => {
							const done = !!tc.endedAt;
							const icon = tc.isError ? "✕" : done ? "●" : "○";
							const iconColor = tc.isError ? colors.error : done ? colors.success : colors.running;
							const label = getToolArgPreview(tc.toolName, tc.args as Record<string, unknown> | undefined, 50);
							const tcDuration = tc.endedAt
								? `${((tc.endedAt - tc.timestamp) / 1000).toFixed(1)}s`
								: "";
							const action = !done ? getToolAction(tc.toolName) : "";

							return (
								<Box key={i} flexDirection="column">
									<Box>
										<Text color={colors.textMuted}> └ </Text>
										<Text color={iconColor}>{icon} </Text>
										<Text bold color={colors.textMuted}>{tc.toolName}</Text>
										{label && <Text color={colors.textMuted}> {label}</Text>}
										{tcDuration && <Text color={colors.textMuted}> · {tcDuration}</Text>}
										{action && <Text color={colors.textMuted}> {action}</Text>}
									</Box>
								</Box>
							);
						})}
					</Box>
				)}

				{/* Text response */}
				{agent.text && (
					<Box flexDirection="column" marginTop={0}>
						{agent.text.split("\n").slice(0, 10).map((line, i) => (
							<Text key={i}>{line.slice(0, 120)}</Text>
						))}
						{agent.text.split("\n").length > 10 && (
							<Text color={colors.textMuted}>  ... +{agent.text.split("\n").length - 10} more lines</Text>
						)}
					</Box>
				)}

				{/* Final result (if different from text) */}
				{agent.result && agent.result !== agent.text && agent.status === "complete" && (
					<Box flexDirection="column" marginTop={0}>
						{agent.result.split("\n").slice(0, 5).map((line, i) => (
							<Text key={i} color={colors.textMuted}>  {line.slice(0, 120)}</Text>
						))}
						{agent.result.split("\n").length > 5 && (
							<Text color={colors.textMuted}>  ... +{agent.result.split("\n").length - 5} more lines</Text>
						)}
					</Box>
				)}

				{/* Error */}
				{agent.error && (
					<Box marginTop={0}>
						<Text color={colors.error}>  {agent.error.slice(0, 150)}</Text>
					</Box>
				)}
			</Box>
		</Box>
	);
}

// ── Tool action text (shown while running) ──

function getToolAction(name: string): string {
	const actions: Record<string, string> = {
		bash: "Running...",
		research: "Searching...",
		search: "Searching...",
		scrape: "Fetching...",
		extract: "Extracting...",
		file_read: "Reading...",
		file_write: "Writing...",
		file_edit: "Editing...",
		file_grep: "Searching...",
		file_glob: "Finding...",
		delegate_to: "Delegating...",
		delegate_async: "Delegating...",
		memory: "Storing...",
		skill: "Loading...",
	};
	return actions[name] || "Working...";
}
