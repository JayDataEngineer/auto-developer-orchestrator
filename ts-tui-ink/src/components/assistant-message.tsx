/**
 * AssistantMessage — renders assistant messages.
 *
 * Renders text, reasoning (collapsed), and tool calls.
 * Reasoning via ReasoningAccordion only (no duplicate from parts loop).
 * Tool calls: single line with proper truncation to prevent mid-word wrapping.
 */

import React, { useState, useEffect, useMemo } from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import { useAuiState } from "@assistant-ui/react-ink";
import { getToolArgPreview } from "@pux/shared";
import { usePuxStore } from "@pux/shared";
import { ReasoningAccordion } from "./reasoning-accordion.js";
import { BranchPicker } from "./branch-picker.js";
import { MarkdownText } from "./markdown-text.js";
import { TerminalImage } from "./terminal-image.js";
import { useColors, symbols, BLOCKQUOTE_BAR, BLACK_CIRCLE } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

// Truncate at word boundary — never cuts mid-word
function trunc(s: string, max: number): string {
	if (s.length <= max) return s;
	const cut = s.slice(0, max - 1);
	const lastSpace = cut.lastIndexOf(" ");
	// If no space found or too close to start, hard truncate
	if (lastSpace < max * 0.5) return cut + "…";
	return cut.slice(0, lastSpace) + "…";
}

export function AssistantMessage() {
	const parts = useAuiState((s) => s.message.parts);
	const isRunning = useAuiState((s) => s.message.status?.type === "running");
	const colors = useColors();
	const { cols } = useTerminalSize();
	// Explicit width: paddingX(1) from outer Box = 2 chars total
	const textWidth = cols - 2;
	const hasContent = parts && parts.some((p: any) =>
		(p.type === "text" && p.text?.trim()) ||
		p.type === "tool-call" ||
		p.type === "reasoning" ||
		p.type === "source"
	);

	// Elapsed time counter for waiting spinner
	const [elapsed, setElapsed] = useState(0);
	useEffect(() => {
		if (!isRunning || hasContent) return;
		setElapsed(0);
		const timer = setInterval(() => setElapsed((t) => t + 1), 1000);
		return () => clearInterval(timer);
	}, [isRunning, hasContent]);

	// Show spinner while running with no visible content yet.
	// Use "waiting..." — not "thinking" — because the model hasn't
	// produced any output yet (still connecting / processing prompt).
	if (isRunning && !hasContent) {
		const timeStr = elapsed < 60 ? `${elapsed}s` : `${Math.floor(elapsed / 60)}m ${elapsed % 60}s`;
		return (
			<Box marginTop={1} paddingX={1} gap={1}>
				<Text color={colors.assistant}><Spinner type="dots" /></Text>
				<Text color={colors.textDim}>waiting... {timeStr}</Text>
			</Box>
		);
	}

	if (!parts || parts.length === 0) return null;

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1} width={textWidth}>
			{/* Reasoning accordion — handles all reasoning display */}
			<ReasoningAccordion />

			{/* Parts: tool calls, text. Skip reasoning (handled by accordion above) */}
			{parts.map((part: any, i: number) => {
				switch (part.type) {
					case "tool-call":
						return (
							<Box key={part.toolCallId || i} flexDirection="column">
								{isDelegateTool(part.toolName) ? (
									<DelegateToolCallDisplay
										toolName={part.toolName}
										args={part.args}
										result={part.result}
										isError={part.isError}
									/>
								) : (
									<ToolCallDisplay
										toolName={part.toolName}
										args={part.args}
										argsText={part.argsText}
										result={part.result}
										isError={part.isError}
									/>
								)}
							</Box>
						);
					case "reasoning":
						// Handled by ReasoningAccordion above — skip duplicate
						return null;
					case "text":
						if (!part.text?.trim()) return null;
						return (
							<MarkdownText key={i} text={part.text} color={colors.text} />
						);
					case "image":
						return (
							<Box key={i} marginTop={1} paddingLeft={1}>
								<TerminalImage
									image={part.image}
									filename={part.filename}
								/>
							</Box>
						);
					case "source":
						return (
							<Box key={part.id || i} paddingLeft={1}>
								<Text color="gray">{BLOCKQUOTE_BAR} </Text>
								<Text color="blue">
									{part.url
										? part.title
											? `${part.title} — ${part.url}`
											: part.url
										: part.title || "source"}
								</Text>
							</Box>
						);
					case "file":
						return (
							<Box key={i} paddingLeft={1}>
								<Text dimColor>{BLOCKQUOTE_BAR} file: {part.name || "(unnamed)"}</Text>
							</Box>
						);
					default:
						return null;
				}
			})}

			{/* Branch picker for forked messages */}
			<BranchPicker />
		</Box>
	);
}

// ── Tool call helpers ──

function isDelegateTool(name: string): boolean {
	return name === "delegate_to" || name === "delegate_async";
}

// ── Delegate tool display — sub-agent progress with width-aware truncation ──

function DelegateToolCallDisplay({
	toolName,
	args,
	result,
	isError,
}: {
	toolName: string;
	args?: unknown;
	result?: unknown;
	isError?: boolean;
}) {
	const colors = useColors();
	const { cols } = useTerminalSize();
	const isDone = result !== undefined;
	const isRunning = !isDone && !isError;

	const role = (args as any)?.role || (args as any)?.instructions || "agent";
	const task = (args as any)?.task || (args as any)?.prompt || "";
	const injectedId = (args as any)?.__agentId as string | undefined;

	// Look up sub-agent details from Zustand store.
	// Priority: exact ID match > name+task match > running fallback
	const agents = usePuxStore((s) => s.agents);
	const agentState = useMemo(() => {
		// Best: match by injected agentId (handles concurrent same-role agents)
		if (injectedId) {
			const byId = agents.get(injectedId);
			if (byId) return byId;
		}
		const candidates = [...agents.values()].filter(
			(a) => a.agentName === role,
		);
		if (candidates.length === 0) return undefined;
		if (candidates.length === 1) return candidates[0];
		const byTask = candidates.find(
			(a) => task.startsWith(a.task) || a.task.startsWith(task),
		);
		return byTask ?? candidates.find((a) => a.status === "running") ?? candidates[0];
	}, [agents, role, task, injectedId]);

	const toolCalls = agentState?.toolCalls ?? [];
	const subToolCount = toolCalls.length;
	const thinkingText = agentState?.thinkingText;
	const agentText = agentState?.text;

	// Duration
	const duration = agentState
		? agentState.endedAt
			? `${((agentState.endedAt - agentState.startedAt) / 1000).toFixed(1)}s`
			: `${((Date.now() - agentState.startedAt) / 1000).toFixed(1)}s`
		: "";

	// Tick every second while running
	const [, setTick] = useState(0);
	useEffect(() => {
		if (!isRunning) return;
		const timer = setInterval(() => setTick((t) => t + 1), 1000);
		return () => clearInterval(timer);
	}, [isRunning]);

	// Width budget for tool call lines: indent(2) + "└ "(3) + symbol(1) + " "(1) = 7
	const toolIndent = 7;
	const maxArgLen = Math.max(15, cols - toolIndent - 20);
	const label = `${toolName} → ${role}`;
	const headerOverhead = 6;
	const maxTaskLen = Math.max(20, cols - headerOverhead - label.length - 10);
	const taskPreview = trunc(task, Math.min(maxTaskLen, 50));

	// Collapsed when done — single line with summary
	if (isDone) {
		const doneSuffix = subToolCount > 0
			? ` done · ${subToolCount} tool${subToolCount !== 1 ? "s" : ""} · ${duration}`
			: " done";
		return (
			<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
				<Text wrap="truncate-end">
					<Text color={isError ? colors.error : colors.success}>{BLACK_CIRCLE} </Text>
					<Text bold color={colors.brand}>{label}</Text>
					{taskPreview && <Text color="gray"> {taskPreview}</Text>}
					<Text color="gray">{doneSuffix}</Text>
				</Text>
				{/* Agent output preview when done — first 3 lines */}
				{agentText && agentText.trim() && (
					<Box paddingLeft={4}>
						<Text dimColor color="gray">
							{agentText.trim().split("\n").slice(0, 3).map((line, i, arr) =>
								`${BLOCKQUOTE_BAR} ${trunc(line, cols - 6)}${i < arr.length - 1 ? "\n" : ""}`
							).join("")}
							{agentText.trim().split("\n").length > 3 ? `\n${BLOCKQUOTE_BAR} ...` : ""}
						</Text>
					</Box>
				)}
			</Box>
		);
	}

	// Running: show nested tool snippets
	const maxShow = 5;
	const visibleTools = toolCalls.length > maxShow
		? toolCalls.slice(-maxShow)
		: toolCalls;
	const hiddenCount = toolCalls.length - visibleTools.length;

	return (
		<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
			<Text wrap="truncate-end">
				<Text color={colors.running}>
					{isRunning ? <Spinner type="dots" /> : BLACK_CIRCLE}{" "}
				</Text>
				<Text bold color={colors.brand}>{label}</Text>
				{taskPreview && <Text color="gray"> {taskPreview}</Text>}
				{subToolCount > 0 && (
					<Text color="gray"> · {subToolCount} tool{subToolCount !== 1 ? "s" : ""}</Text>
				)}
			</Text>

			{hiddenCount > 0 && (
				<Text dimColor color="gray">
					{"  └ "}{symbols.dot} {hiddenCount} earlier
				</Text>
			)}

			{visibleTools.map((tc, i) => {
				const isActive = !tc.endedAt;
				const isLast = i === visibleTools.length - 1;
				const rawArg = getToolArgPreview(tc.toolName, tc.args as Record<string, unknown> | undefined, maxArgLen);
				const argPreview = trunc(rawArg, maxArgLen);
				const sym = tc.isError ? symbols.toolError : tc.endedAt ? symbols.toolDone : symbols.toolRunning;
				return (
					<Text key={`${tc.toolName}-${tc.timestamp}-${i}`} wrap="truncate-end">
						<Text dimColor color="gray">{"  └ "}</Text>
						<Text color={tc.isError ? colors.error : tc.endedAt ? colors.success : colors.running}>
							{sym}
						</Text>
						<Text> </Text>
						<Text bold color={isActive ? colors.running : undefined}>
							{tc.toolName}
						</Text>
						{argPreview && <Text color="gray"> {argPreview}</Text>}
						{isActive && isLast && isRunning && (
							<Text color={colors.running}> <Spinner type="dots" /></Text>
						)}
					</Text>
				);
			})}

			{toolCalls.length === 0 && !thinkingText && (
				<Text dimColor color="gray">{"  └ "}starting...</Text>
			)}

			{/* Show agent thinking preview while running */}
			{thinkingText && isRunning && (
				<Text dimColor color="gray">
					{"  └ "}{BLOCKQUOTE_BAR} {trunc(thinkingText.split("\n").pop() || thinkingText, cols - 8)}
				</Text>
			)}
		</Box>
	);
}

// ── Regular tool call display — single line ──

function ToolCallDisplay({
	toolName,
	args,
	result,
	isError,
}: {
	toolName: string;
	args?: unknown;
	argsText?: string;
	result?: unknown;
	isError?: boolean;
}) {
	const colors = useColors();
	const { cols } = useTerminalSize();
	const isDone = result !== undefined;
	const isRunning = !isDone && !isError;

	const rawArg = getToolArgPreview(toolName, args as Record<string, unknown> | undefined);
	// Overhead: symbol(1) + space(1) + toolname(~15) + space(1) + "done"(4) = ~22
	const maxArgLen = Math.max(10, cols - toolName.length - 24);
	const argPreview = trunc(rawArg, maxArgLen);

	const sym = isRunning ? symbols.toolRunning : isError ? symbols.toolError : symbols.toolDone;

	return (
		<Text wrap="truncate-end">
			<Text color={isError ? colors.error : isDone ? colors.success : colors.running}>{sym}</Text>
			<Text> </Text>
			<Text bold color={isRunning ? colors.running : undefined}>{toolName}</Text>
			{argPreview && <Text color={colors.textMuted}> {argPreview}</Text>}
			{isDone && !isError && <Text color={colors.textMuted}> done</Text>}
			{isError && <Text color={colors.error}> failed</Text>}
		</Text>
	);
}

