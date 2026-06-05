/**
 * AgentsView — subagent conversation panel.
 *
 * Shows agents as conversation cards. Collapsed: status + task preview.
 * Expanded: thinking, tool calls with args/results, text response, error.
 *
 * Data comes from the Zustand store, populated by subagent SSE events
 * in pux-chat-adapter.ts. Completed agents fetch full transcript for
 * tool results via /api/pux/history.
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
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";
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

// ── Agents view (list + conversation) ──

export function AgentsView() {
	const agents = usePuxStore((s) => s.agents);
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

	// Limit visible agents to viewport
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

			{/* Agent cards */}
			{visibleAgents.map((agent, i) => (
				<AgentConversationCard
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
				<Text color={colors.textMuted}>
					<Text bold>Up/Down</Text> navigate <Text bold>Enter/Space</Text> expand <Text bold>Esc/Right</Text> back
				</Text>
			</Box>
		</Box>
	);
}

// ── Agent conversation card ──

function AgentConversationCard({
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

	// Transcript data for completed agents
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

	// Fetch transcript when expanded for completed agents
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

	return (
		<Box flexDirection="column" marginBottom={1}>
			{/* Header line — always visible */}
			<Box>
				<Text color={statusColor}>{statusIcon} </Text>
				<Text bold color={isSelected ? colors.brand : undefined}>
					{agent.agentName}
				</Text>
				<Text color={colors.textMuted}> {symbols.dot} {agent.toolCalls.length} tools {symbols.dot} {duration}</Text>
			</Box>

			{/* Collapsed: task preview only */}
			{!isExpanded && (
				<Text color={colors.textMuted}>
					{"  "}{BLOCKQUOTE_BAR} {agent.task.slice(0, 80)}
					{agent.task.length > 80 ? "..." : ""}
				</Text>
			)}

			{/* Expanded: conversation blocks */}
			{isExpanded && (
				<Box flexDirection="column" paddingLeft={2}>
					{/* Task */}
					<Text color={colors.textMuted}>
						{BLOCKQUOTE_BAR} {agent.task.slice(0, 120)}
						{agent.task.length > 120 ? "..." : ""}
					</Text>

					{/* Thinking block */}
					{agent.thinkingText && (
						<ThinkingBlock text={agent.thinkingText} />
					)}

					{/* Tool calls */}
					{agent.toolCalls.map((tc, i) => {
						const done = !!tc.endedAt;
						const icon = tc.isError ? "✕" : done ? "●" : "○";
						const iconColor = tc.isError ? colors.error : done ? colors.success : colors.running;
						const label = getToolArgPreview(tc.toolName, tc.args as Record<string, unknown> | undefined, 50);
						const tcDuration = tc.endedAt
							? `${((tc.endedAt - tc.timestamp) / 1000).toFixed(1)}s`
							: "";

						// Tool result: from real-time data or transcript
						const result = tc.result as string | undefined;
						const transcriptResult = (tc as any).toolCallId
							? toolResultMap.get((tc as any).toolCallId as string)
							: undefined;
						const resultText = typeof result === "string" ? result : transcriptResult;

						return (
							<Box key={i} flexDirection="column">
								<Box>
									<Text color={iconColor}>  {icon} </Text>
									<Text color={colors.textMuted}>{tc.toolName}</Text>
									{label && <Text color={colors.textMuted}> {label}</Text>}
									{tcDuration && <Text color={colors.textMuted}> · {tcDuration}</Text>}
								</Box>
								{/* Tool result preview */}
								{resultText && (
									<ResultPreview text={resultText} />
								)}
							</Box>
						);
					})}

					{/* Text response */}
					{agent.text && (
						<Text color={colors.text}>
							{agent.text.split("\n").slice(0, 8).map((line, i) => (
								<Text key={i}>
									{i > 0 ? "\n" : ""}{line.slice(0, 120)}
								</Text>
							))}
							{agent.text.split("\n").length > 8 && (
								<Text color={colors.textMuted}>{"\n"}  ... +{agent.text.split("\n").length - 8} more lines</Text>
							)}
						</Text>
					)}

					{/* Final result (if different from text) */}
					{agent.result && agent.result !== agent.text && agent.status === "complete" && (
						<Box flexDirection="column">
							{agent.result.split("\n").slice(0, 5).map((line, i) => (
								<Text key={i} color={colors.textMuted}>
									{"  "}{BLOCKQUOTE_BAR} {line.slice(0, 120)}
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
								{symbols.cross} {agent.error.slice(0, 150)}
							</Text>
						</Box>
					)}
				</Box>
			)}
		</Box>
	);
}

// ── Thinking block (collapsed preview) ──

function ThinkingBlock({ text }: { text: string }) {
	const colors = useColors();
	const lines = text.split("\n").filter((l) => l.trim());
	if (lines.length === 0) return null;

	return (
		<Box flexDirection="column">
			{lines.slice(0, 3).map((line, i) => (
				<Text key={i} color={colors.textMuted}>
					{BLOCKQUOTE_BAR} {line.slice(0, 120)}
				</Text>
			))}
			{lines.length > 3 && (
				<Text color={colors.textMuted}>
					{BLOCKQUOTE_BAR} ... {lines.length - 3} more lines
				</Text>
			)}
		</Box>
	);
}

// ── Tool result preview (first 2 lines) ──

function ResultPreview({ text }: { text: string }) {
	const colors = useColors();
	const lines = text.split("\n").filter((l) => l.trim());
	if (lines.length === 0) return null;

	return (
		<Box flexDirection="column" paddingLeft={3}>
			{lines.slice(0, 2).map((line, i) => (
				<Text key={i} color={colors.textMuted}>
					{BLOCKQUOTE_BAR} {line.slice(0, 100)}
				</Text>
			))}
			{lines.length > 2 && (
				<Text color={colors.textMuted}>
					{"  "}{BLOCKQUOTE_BAR} ... {lines.length - 2} more
				</Text>
			)}
		</Box>
	);
}
