/**
 * AssistantMessage — renders assistant messages.
 *
 * Renders text, reasoning (collapsed), and tool calls.
 * Reasoning via ReasoningAccordion only (no duplicate from parts loop).
 * Tool calls: single line — ● toolName(args) when running, ● toolName(args) done when complete.
 */

import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import { useAuiState } from "@assistant-ui/react-ink";
import { getToolArgPreview } from "@pux/shared";
import { usePuxStore } from "@pux/shared";
import { ReasoningAccordion } from "./reasoning-accordion.js";
import { BranchPicker } from "./branch-picker.js";
import { MarkdownText } from "./markdown-text.js";
import { TerminalImage } from "./terminal-image.js";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

export function AssistantMessage() {
	const msg = useAuiState((s) => s.message);
	const colors = useColors();
	const parts = msg.parts;
	const isRunning = msg.status?.type === "running";
	const hasContent = parts && parts.some((p: any) =>
		(p.type === "text" && p.text?.trim()) ||
		p.type === "tool-call" ||
		p.type === "reasoning" ||
		p.type === "source"
	);

	// Show spinner while running with no visible content yet
	if (isRunning && !hasContent) {
		return (
			<Box marginTop={1} paddingX={1} gap={1}>
				<Text color={colors.brand}><Spinner type="dots" /></Text>
				<Text dimColor>thinking...</Text>
			</Box>
		);
	}

	if (!parts || parts.length === 0) return null;

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1}>
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
							<Box key={i} flexDirection="column" paddingLeft={1}>
								<MarkdownText text={part.text} />
							</Box>
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

// ── Tool call display — compact single line ──

function isDelegateTool(name: string): boolean {
	return name === "delegate_to" || name === "delegate_async";
}

// ── Delegate tool display — shows sub-agent progress from Zustand store ──

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
	const isDone = result !== undefined;
	const isRunning = !isDone && !isError;

	const agentName = (args as any)?.agent_id || (args as any)?.agent || (args as any)?.instructions || "agent";
	const task = (args as any)?.task || (args as any)?.prompt || "";
	const taskPreview = task.length > 50 ? task.slice(0, 47) + "..." : task;

	// Look up sub-agent details from Zustand store
	const agents = usePuxStore((s) => s.agents);
	const agentState = [...agents.values()].find(
		(a) => a.agentName === agentName && a.task === task,
	);
	const subToolCount = agentState?.toolCalls.length ?? 0;
	const lastToolName = subToolCount > 0 ? agentState!.toolCalls[subToolCount - 1].toolName : null;

	return (
		<Box flexDirection="column">
			<Box>
				<Text color={isError ? colors.error : isDone ? colors.success : colors.running}>
					{isRunning ? symbols.toolRunning : isError ? symbols.toolError : symbols.toolDone}
				</Text>
				<Text> </Text>
				<Text bold color={colors.brand}>{agentName}</Text>
				{taskPreview && <Text color="gray">({taskPreview})</Text>}
				<Text color="gray">
					{isDone ? " done" : isRunning ? " working..." : ""}
				</Text>
				{subToolCount > 0 && (
					<Text color="gray"> · {subToolCount} tool{subToolCount !== 1 ? "s" : ""}</Text>
				)}
			</Box>
			{/* Show last active sub-tool when running */}
			{isRunning && lastToolName && (
				<Box paddingLeft={3}>
					<Text dimColor color={colors.running}>
						{symbols.toolRunning} {lastToolName}
					</Text>
				</Box>
			)}
		</Box>
	);
}

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
	const isDone = result !== undefined;
	const isRunning = !isDone && !isError;

	// Extract a useful arg preview
	const argPreview = getToolArgPreview(toolName, args as Record<string, unknown> | undefined);

	return (
		<Box>
			<Text color={isError ? colors.error : isDone ? colors.success : colors.running}>
				{isRunning ? symbols.toolRunning : isError ? symbols.toolError : symbols.toolDone}
			</Text>
			<Text> </Text>
			<Text bold color={isRunning ? colors.running : undefined}>
				{toolName}
			</Text>
			{argPreview && (
				<Text color="gray">({argPreview})</Text>
			)}
			{isDone && !isError && <Text color="gray"> done</Text>}
			{isError && <Text color={colors.error}> failed</Text>}
		</Box>
	);
}

