/**
 * AgentZoomOverlay — full-screen detail view for a single subagent.
 *
 * Triggered when zoomedAgentId is set in the Zustand store.
 * Shows complete tool call list, result preview, and error details.
 * Press 't' to toggle transcript view (full chat history from backend).
 * OpenCode-style "zoom in" on an agent block.
 */

import React, { useState, useEffect, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore, getToolArgPreview, apiUrl, getFetch } from "@pux/shared";
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

// ── Main overlay ──

export function AgentZoomOverlay() {
	const zoomedAgentId = usePuxStore((s) => s.zoomedAgentId);
	const agents = usePuxStore((s) => s.agents);
	const setZoomedAgent = usePuxStore((s) => s.setZoomedAgent);
	const activeProject = usePuxStore((s) => s.activeProject);
	const colors = useColors();
	const { rows, cols } = useTerminalSize();

	const [scrollOffset, setScrollOffset] = useState(0);
	const [view, setView] = useState<"summary" | "transcript">("summary");
	const [messages, setMessages] = useState<StoredMessage[]>([]);
	const [loading, setLoading] = useState(false);
	const [fetchError, setFetchError] = useState("");

	// Find the agent
	const agent = zoomedAgentId
		? agents.get(zoomedAgentId)
		: undefined;

	// Reset state when agent changes
	useEffect(() => {
		setScrollOffset(0);
		setView("summary");
		setMessages([]);
		setFetchError("");
	}, [zoomedAgentId]);

	// Fetch transcript when switching to transcript view
	useEffect(() => {
		if (view !== "transcript" || !agent?.transcriptId || !activeProject) return;
		if (messages.length > 0) return; // already loaded

		setLoading(true);
		setFetchError("");

		const params = new URLSearchParams({
			project: activeProject,
			agentId: agent.transcriptId,
			limit: "200",
		});

		getFetch()(apiUrl(`/api/pux/history?${params}`))
			.then((resp) => {
				if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
				return resp.json();
			})
			.then((data: StoredMessage[]) => {
				setMessages(Array.isArray(data) ? data : []);
			})
			.catch((err) => {
				setFetchError(err.message || "Failed to load transcript");
			})
			.finally(() => setLoading(false));
	}, [view, agent?.transcriptId, activeProject, messages.length]);

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
		if (input === "t" || input === "T") {
			if (agent?.transcriptId) {
				setView((v) => v === "summary" ? "transcript" : "summary");
				setScrollOffset(0);
			}
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
	}, [setZoomedAgent, agent?.transcriptId]));

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

	const headerHeight = 4; // header + task + separator + footer
	const contentHeight = Math.max(1, rows - headerHeight);

	// Build content lines based on current view
	const contentLines: React.ReactNode[] = view === "summary"
		? buildSummaryLines(agent, colors)
		: buildTranscriptLines(messages, loading, fetchError, colors);

	// Apply scroll offset
	const visibleContent = contentLines.slice(scrollOffset, scrollOffset + contentHeight);
	const maxScroll = Math.max(0, contentLines.length - contentHeight);

	return (
		<Box flexDirection="column" flexGrow={1}>
			{/* Header */}
			<Box paddingX={1}>
				<Text color={statusColor}>{statusIcon} </Text>
				<Text bold color={colors.brand}>{agent.agentName}</Text>
				<Text color="gray"> {symbols.dot} </Text>
				<Text color={statusColor}>{agent.status}</Text>
				<Text color="gray"> {symbols.dot} {duration} {symbols.dot} {agent.toolCalls.length} tool{agent.toolCalls.length !== 1 ? "s" : ""}</Text>
				{view === "transcript" && <Text color={colors.brand}> {symbols.dot} transcript</Text>}
			</Box>

			{/* Task */}
			<Box paddingX={1} marginBottom={0}>
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
					{agent.transcriptId && <> <Text bold>{symbols.dot}</Text> <Text bold>t</Text> {view === "summary" ? "transcript" : "summary"}</>}
					{maxScroll > 0 && <> <Text bold>{symbols.dot}</Text> <Text bold>Up/Down</Text> scroll ({scrollOffset}/{maxScroll})</>}
				</Text>
			</Box>
		</Box>
	);
}

// ── Summary view (tool calls + result) ──

function buildSummaryLines(agent: ReturnType<typeof usePuxStore.getState>["agents"] extends Map<string, infer V> ? V : never, colors: ReturnType<typeof useColors>): React.ReactNode[] {
	const lines: React.ReactNode[] = [];

	agent.toolCalls.forEach((tc, i) => {
		const argPreview = getToolArgPreview(tc.toolName, tc.args as Record<string, unknown> | undefined, 60);
		const tcDuration = tc.endedAt
			? `${((tc.endedAt - tc.timestamp) / 1000).toFixed(1)}s`
			: "";
		lines.push(
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

	const resultLines = agent.result ? agent.result.split("\n") : [];
	if (resultLines.length > 0) {
		lines.push(
			<Box key="result-header" marginTop={1}>
				<Text bold color={colors.brand}>Result</Text>
			</Box>
		);
		resultLines.forEach((line, i) => {
			lines.push(
				<Box key={`result-${i}`} paddingLeft={1}>
					<Text color="gray">{BLOCKQUOTE_BAR} </Text>
					<Text dimColor>{line}</Text>
				</Box>
			);
		});
	}

	if (agent.error) {
		lines.push(
			<Box key="error-header" marginTop={1}>
				<Text bold color={colors.error}>Error</Text>
			</Box>
		);
		lines.push(
			<Box key="error-body" paddingLeft={1}>
				<Text color={colors.error}>{agent.error}</Text>
			</Box>
		);
	}

	return lines;
}

// ── Transcript view (full chat history from backend) ──

function buildTranscriptLines(
	messages: StoredMessage[],
	loading: boolean,
	fetchError: string,
	colors: ReturnType<typeof useColors>,
): React.ReactNode[] {
	const lines: React.ReactNode[] = [];

	if (loading) {
		lines.push(
			<Box key="loading" paddingY={1}>
				<Text color={colors.running}>Loading transcript...</Text>
			</Box>
		);
		return lines;
	}

	if (fetchError) {
		lines.push(
			<Box key="error" paddingY={1}>
				<Text color={colors.error}>Failed to load: {fetchError}</Text>
			</Box>
		);
		return lines;
	}

	if (messages.length === 0) {
		lines.push(
			<Box key="empty" paddingY={1}>
				<Text dimColor>No transcript available</Text>
			</Box>
		);
		return lines;
	}

	// Build tool result lookup
	const toolResults = new Map<string, { result: string; isError: boolean }>();
	for (const msg of messages) {
		if (msg.role === "tool" && msg.toolCallId) {
			let isError = false;
			try {
				const parsed = JSON.parse(msg.content || "");
				if (parsed.error) isError = true;
			} catch { /* not JSON */ }
			toolResults.set(msg.toolCallId, { result: msg.content || "", isError });
		}
	}

	// Render messages in order
	for (const msg of messages) {
		if (msg.role === "tool") continue; // tool results are inlined below

		if (msg.role === "user") {
			lines.push(
				<Box key={`msg-${msg.id}`} marginTop={1} paddingLeft={1}>
					<Text color={colors.brand} bold>{">"}</Text>
					<Text> {msg.content}</Text>
				</Box>
			);
			continue;
		}

		// Assistant message
		// Thinking
		if (msg.thinking) {
			const thinkLines = msg.thinking.split("\n");
			lines.push(
				<Box key={`think-h-${msg.id}`} paddingLeft={1} marginTop={0}>
					<Text dimColor italic>thinking...</Text>
				</Box>
			);
			thinkLines.slice(0, 6).forEach((line, i) => {
				lines.push(
					<Box key={`think-${msg.id}-${i}`} paddingLeft={2}>
						<Text dimColor>{BLOCKQUOTE_BAR} {line.slice(0, 80)}</Text>
					</Box>
				);
			});
			if (thinkLines.length > 6) {
				lines.push(
					<Box key={`think-more-${msg.id}`} paddingLeft={2}>
						<Text dimColor>{BLOCKQUOTE_BAR} ... ({thinkLines.length - 6} more lines)</Text>
					</Box>
				);
			}
		}

		// Tool calls
		if (msg.toolCalls && msg.toolCalls !== "[]") {
			try {
				const calls: { id?: string; name?: string; args?: Record<string, unknown>; argsText?: string }[] = JSON.parse(msg.toolCalls);
				for (const tc of calls) {
					const callId = tc.id || "";
					const tr = toolResults.get(callId);
					const argPreview = getToolArgPreview(tc.name || "unknown", tc.args, 50);

					lines.push(
						<Box key={`tc-${msg.id}-${callId}`} paddingLeft={1} marginTop={0}>
							<Text color={tr ? (tr.isError ? colors.error : colors.success) : colors.running}>
								{tr ? (tr.isError ? symbols.toolError : symbols.toolDone) : symbols.toolRunning}
							</Text>
							<Text> </Text>
							<Text bold>{tc.name || "unknown"}</Text>
							{argPreview && <Text color="gray"> {argPreview}</Text>}
						</Box>
					);

					// Show tool result preview (first 3 lines)
					if (tr) {
						const resultPreview = tr.result.split("\n").slice(0, 3);
						resultPreview.forEach((line, i) => {
							lines.push(
								<Box key={`tr-${msg.id}-${callId}-${i}`} paddingLeft={3}>
									<Text color="gray">{BLOCKQUOTE_BAR} </Text>
									<Text dimColor>{line.slice(0, 100)}</Text>
								</Box>
							);
						});
					}
				}
			} catch { /* skip malformed */ }
		}

		// Text
		if (msg.text) {
			const textLines = msg.text.split("\n");
			textLines.slice(0, 20).forEach((line, i) => {
				lines.push(
					<Box key={`text-${msg.id}-${i}`} paddingLeft={1}>
						<Text>{line}</Text>
					</Box>
				);
			});
			if (textLines.length > 20) {
				lines.push(
					<Box key={`text-more-${msg.id}`} paddingLeft={1}>
						<Text dimColor>... ({textLines.length - 20} more lines)</Text>
					</Box>
				);
			}
		}
	}

	return lines;
}
