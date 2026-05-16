/**
 * AssistantMessage — renders assistant messages.
 *
 * Renders text, reasoning (collapsed), and tool calls with args + result preview.
 * Reasoning is shown via ReasoningAccordion only (no duplicate from parts loop).
 * Tool calls show: ● toolName(args) when running, ● toolName(args) ⎿ result when done.
 */

import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import { useAuiState } from "@assistant-ui/react-ink";
import { formatToolResult, getToolArgPreview } from "@pux/shared";
import { ReasoningAccordion } from "./reasoning-accordion.js";
import { BranchPicker } from "./branch-picker.js";
import { colors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

export function AssistantMessage() {
	const msg = useAuiState((s) => s.message);
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
								<ToolCallDisplay
									toolName={part.toolName}
									args={part.args}
									argsText={part.argsText}
									result={part.result}
									isError={part.isError}
								/>
							</Box>
						);
					case "reasoning":
						// Handled by ReasoningAccordion above — skip duplicate
						return null;
					case "text":
						if (!part.text?.trim()) return null;
						return (
							<Box key={i} flexDirection="column" paddingLeft={1}>
								{part.text.split("\n").map((line: string, j: number) => (
									<Text key={j}>{line || " "}</Text>
								))}
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
					default:
						return null;
				}
			})}

			{/* Branch picker for forked messages */}
			<BranchPicker />
		</Box>
	);
}

// ── Tool call display — shows args and result ──

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
	const isDone = result !== undefined;
	const isRunning = !isDone && !isError;

	// Extract a useful arg preview
	const argPreview = getToolArgPreview(toolName, args as Record<string, unknown> | undefined);

	// Format result preview
	const resultPreview = isDone ? formatToolResult(result, 3) : [];

	// Max width for result lines to prevent terminal wrapping garble
	const maxResultWidth = 90;

	return (
		<Box flexDirection="column">
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
			</Box>
			{resultPreview.length > 0 && !isError && (
				<Box paddingLeft={2} flexDirection="column">
					<Text color="gray">{BLOCKQUOTE_BAR} </Text>
					{resultPreview.map((line, i) => (
						<Text key={i} dimColor>
							{"  "}{BLOCKQUOTE_BAR} {line.length > maxResultWidth ? line.slice(0, maxResultWidth - 3) + "..." : line}
						</Text>
					))}
				</Box>
			)}
			{isError && (
				<Box paddingLeft={2}>
					<Text color={colors.error}>  {symbols.cross} failed</Text>
				</Box>
			)}
		</Box>
	);
}

