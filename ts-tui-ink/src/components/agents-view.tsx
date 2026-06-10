/**
 * AgentsView — subagent conversation panel.
 *
 * Shows agents in a list. Expanded agents render conversation-style
 * blocks with left-border separation, matching OpenCode's tool rendering:
 *
 * ● researcher · 115.5s
 * ┃ Task: Find the weather in New York
 * ┃  └ bash: curl wttr.in/new+york · 1.2s
 * ┃  └ search: weather NYC · 0.8s
 * ┃ Result text here...
 *
 * Data from Zustand store + transcript fetch for completed agents.
 */

import React, { useState, useEffect, useCallback, useMemo } from "react";
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
import { MarkdownText as _MarkdownText } from "@assistant-ui/react-ink-markdown";
import type { Theme as MarkdansiTheme } from "markdansi";
const MarkdownText = _MarkdownText as React.FC<
	React.ComponentProps<typeof _MarkdownText> & { tableTruncate?: boolean }
>;
const mdTheme: MarkdansiTheme = {
	heading: { bold: true, color: "#ffffff" },
	strong: { bold: true, color: "#ffffff" },
	emph: { italic: true },
	inlineCode: { color: "#d77757" },
	blockCode: { color: "#b0b0b0" },
	code: { color: "#d77757" },
	link: { color: "#73daca", underline: true },
	quote: { color: "#6a737d" },
	hr: { color: "#505050" },
	listMarker: { color: "#999999" },
	tableHeader: { bold: true, color: "#ffffff" },
	tableCell: { color: "#cccccc" },
};

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

// ── Friendly names for internal tools ──

const TOOL_DISPLAY_NAMES: Record<string, string> = {
	load_spilled: "read_output",
};

// ── Word-wrap helper ──

function wrapText(text: string, maxWidth: number): string[] {
	const words = text.split(/\s+/);
	const lines: string[] = [];
	let current = "";
	for (const word of words) {
		if (current.length === 0) {
			current = word;
		} else if (current.length + 1 + word.length <= maxWidth) {
			current += " " + word;
		} else {
			lines.push(current);
			current = word;
		}
	}
	if (current) lines.push(current);
	return lines.length > 0 ? lines : [""];
}

// ── Text normalizer ──
// Collapse single newlines to spaces, fix sentence boundary gaps from streaming.

function normalizeText(text: string): string {
	return text
		.split(/\n\n+/)
		.map(p => p
			.replace(/\n/g, " ")
			.replace(/\.([A-Z])/g, ". $1")
			.replace(/ +/g, " ")
			.trim())
		.filter(p => p.length > 0)
		.join("\n\n");
}

// ── Agents view ──

export function AgentsView() {
	const agents = usePuxStore((s) => s.agents);
	const { rows, cols } = useTerminalSize();
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

	// When any agent is expanded, show it full-height (no pagination)
	const hasExpanded = expanded.size > 0;

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

	// When expanded, show only the selected agent full-height.
	// Otherwise, show a paginated list.
	const maxVisible = hasExpanded ? agentList.length : Math.max(3, rows - 6);
	const scrollOffset = hasExpanded
		? 0
		: Math.max(0, Math.min(
			selectedIdx - Math.floor(maxVisible / 2),
			agentList.length - maxVisible,
		));
	const visibleAgents = hasExpanded
		? agentList
		: agentList.slice(scrollOffset, scrollOffset + maxVisible);

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
					cols={cols}
				/>
			))}

			{!hasExpanded && agentList.length > maxVisible && (
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
	cols,
}: {
	agent: AgentState;
	isSelected: boolean;
	isExpanded: boolean;
	cols: number;
}) {
	const colors = useColors();
	const activeProject = usePuxStore((s) => s.activeProject);
	const [transcript, setTranscript] = useState<StoredMessage[]>([]);
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

	// Fetch full transcript for completed agents when expanded
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
				setTranscript(data);
				setTranscriptLoaded(true);
			})
			.catch(() => {});
	}, [isExpanded, agent.transcriptId, agent.status, activeProject, transcriptLoaded]);

	// Tool calls with friendly display names
	const visibleTools = useMemo(
		() => agent.toolCalls.map((tc) => ({
			...tc,
			toolName: TOOL_DISPLAY_NAMES[tc.toolName] || tc.toolName,
		})),
		[agent.toolCalls],
	);

	// Collapsed: one-line summary
	if (!isExpanded) {
		return (
			<Box flexDirection="column" marginBottom={1}>
				<Box>
					<Text color={statusColor}>{statusIcon} </Text>
					<Text bold color={isSelected ? colors.brand : undefined}>
						{agent.agentName}
					</Text>
					<Text color={colors.textMuted}> {symbols.dot} {visibleTools.length} tools {symbols.dot} {duration}</Text>
				</Box>
				<Text color={colors.textMuted}>
					{"  "}{agent.task.slice(0, 80)}{agent.task.length > 80 ? "..." : ""}
				</Text>
			</Box>
		);
	}

	// Width budgets
	const toolLineWidth = Math.max(20, cols - 4);
	const thinkWidth = Math.max(20, cols - 5);

	return (
		<Box flexDirection="column" marginTop={1} paddingRight={1}>
			{/* Header — agent name, status, duration */}
			<Box>
				<Text color={statusColor}>{statusIcon} </Text>
				<Text bold color={colors.brand}>{agent.agentName}</Text>
				<Text color={colors.textMuted}> {symbols.dot} {visibleTools.length} tools {symbols.dot} {duration}</Text>
			</Box>

			{/* Completed agent with transcript — multi-turn conversation */}
			{transcriptLoaded && transcript.length > 0 ? (
				<TranscriptConversation transcript={transcript} cols={cols} />
			) : (
				/* Running/recent agent — flat view: thinking → tools → text */
				<>
					{agent.thinkingText && (
						<ThinkingBlock text={agent.thinkingText} thinkWidth={thinkWidth} />
					)}
					{visibleTools.map((tc, i) => (
						<ToolCallLine key={i} tc={tc} toolLineWidth={toolLineWidth} />
					))}
					{agent.text && (
						<Box paddingLeft={2}>
							<MarkdownText text={normalizeText(agent.text)} tableTruncate={false} theme={mdTheme} />
						</Box>
					)}
				</>
			)}

			{/* Error */}
			{agent.error && (
				<Box paddingLeft={2}>
					<Text color={colors.error}>{agent.error.slice(0, 200)}</Text>
				</Box>
			)}

			{/* Completion */}
			{agent.status !== "running" && (
				<Box marginTop={1}>
					<Text color={colors.textMuted}>● Completed in {duration}</Text>
				</Box>
			)}
		</Box>
	);
}

// ── Thinking block with ▎ bars ──

function ThinkingBlock({ text, thinkWidth }: { text: string; thinkWidth: number }) {
	const colors = useColors();
	const lines: string[] = [];
	const norm = normalizeText(text);
	for (const para of norm.split("\n")) {
		if (!para.trim()) continue;
		lines.push(...wrapText(para, thinkWidth));
	}
	if (lines.length === 0) return null;
	return (
		<Box marginTop={1} marginBottom={1} paddingLeft={2} flexDirection="column">
			{lines.map((line, i) => (
				<Text key={i} color={colors.textMuted}>
					{BLOCKQUOTE_BAR} {line}
				</Text>
			))}
		</Box>
	);
}

// ── Single tool call line ──

function ToolCallLine({ tc, toolLineWidth }: { tc: any; toolLineWidth: number }) {
	const colors = useColors();
	const done = !!tc.endedAt;
	const icon = tc.isError ? "✗" : done ? "✓" : "●";
	const iconColor = tc.isError ? colors.error : done ? colors.success : colors.running;
	const label = getToolArgPreview(tc.toolName, tc.args as Record<string, unknown> | undefined, 50);
	const tcDuration = tc.endedAt
		? ` · ${((tc.endedAt - tc.timestamp) / 1000).toFixed(1)}s`
		: "";

	let line = tc.toolName;
	if (label) line += ` ${label}`;
	line += tcDuration;
	if (line.length > toolLineWidth) {
		line = line.slice(0, toolLineWidth - 1) + "…";
	}

	return (
		<Box>
			<Text color={iconColor}>{icon}</Text>
			<Text color={colors.textMuted}> {line}</Text>
		</Box>
	);
}

// ── Transcript-based multi-turn conversation ──

function TranscriptConversation({ transcript, cols }: { transcript: StoredMessage[]; cols: number }) {
	const colors = useColors();
	const thinkWidth = Math.max(20, cols - 5);
	const toolLineWidth = Math.max(20, cols - 4);

	// Build tool result lookup
	const toolResults = new Map<string, { result: string; isError: boolean }>();
	for (const msg of transcript) {
		if (msg.role === "tool" && msg.toolCallId) {
			let isError = false;
			try {
				const parsed = JSON.parse(msg.content || "");
				if (parsed.error) isError = true;
			} catch { /* not JSON */ }
			toolResults.set(msg.toolCallId, { result: msg.content || "", isError });
		}
	}

	return (
		<Box flexDirection="column" marginTop={1}>
			{transcript.map((msg) => {
				if (msg.role === "tool") return null;

				// User message — show task
				if (msg.role === "user") {
					return (
						<Box key={`msg-${msg.id}`} paddingLeft={1} marginTop={1}>
							<Text color={colors.brand} bold>{">"}</Text>
							<Text> {msg.content.slice(0, 120)}</Text>
						</Box>
					);
				}

				// Assistant message — thinking + tools + text
				const blocks: React.ReactNode[] = [];

				// Thinking
				if (msg.thinking) {
					const normThink = normalizeText(msg.thinking);
					const thinkLines: string[] = [];
					for (const para of normThink.split("\n")) {
						if (!para.trim()) continue;
						thinkLines.push(...wrapText(para, thinkWidth));
					}
					if (thinkLines.length > 0) {
						blocks.push(
							<Box key={`think-${msg.id}`} marginBottom={1} paddingLeft={2} flexDirection="column">
								{thinkLines.map((line, i) => (
									<Text key={i} color={colors.textMuted}>
										{BLOCKQUOTE_BAR} {line}
									</Text>
								))}
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
							const displayName = TOOL_DISPLAY_NAMES[tc.name || ""] || tc.name || "unknown";
							const argPreview = getToolArgPreview(displayName, tc.args, 50);
							const icon = tr ? (tr.isError ? "✗" : "✓") : "●";
							const iconColor = tr ? (tr.isError ? colors.error : colors.success) : colors.running;

							let line = displayName;
							if (argPreview) line += ` ${argPreview}`;

							if (line.length > toolLineWidth) {
								line = line.slice(0, toolLineWidth - 1) + "…";
							}

							blocks.push(
								<Box key={`tc-${msg.id}-${callId}`}>
									<Text color={iconColor}>{icon}</Text>
									<Text color={colors.textMuted}> {line}</Text>
								</Box>
							);
						}
					} catch { /* skip malformed */ }
				}

				// Text
				if (msg.text) {
					const normText = normalizeText(msg.text);
					blocks.push(
						<Box key={`text-${msg.id}`} paddingLeft={2}>
							<MarkdownText text={normText} tableTruncate={false} theme={mdTheme} />
						</Box>
					);
				}

				if (blocks.length === 0) return null;
				return (
					<Box key={`msg-${msg.id}`} flexDirection="column">
						{blocks}
					</Box>
				);
			})}
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
