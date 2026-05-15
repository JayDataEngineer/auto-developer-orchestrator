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
		p.type === "reasoning"
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
	const argPreview = getArgPreview(toolName, args as Record<string, unknown> | undefined);

	// Format result preview
	const resultPreview = isDone ? formatResult(result) : null;

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
			{resultPreview && !isError && (
				<Box paddingLeft={2} flexDirection="column">
					<Text color="gray">{BLOCKQUOTE_BAR} </Text>
					{resultPreview.map((line, i) => (
						<Text key={i} dimColor>
							{"  "}{BLOCKQUOTE_BAR} {line}
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

/** Get a compact arg preview for common tools */
function getArgPreview(toolName: string, args?: Record<string, unknown>): string {
	if (!args) return "";
	const entries = Object.entries(args);
	if (entries.length === 0) return "";

	// For bash/shell tools, show the command
	if (["bash", "shell", "run_command"].includes(toolName)) {
		const cmd = (args.command as string) || (args.cmd as string) || "";
		if (cmd) return cmd.length > 60 ? cmd.slice(0, 57) + "..." : cmd;
	}

	// For delegate tools, show the agent name
	if (["delegate_to", "delegate_async"].includes(toolName)) {
		return (args.agent as string) || "";
	}

	// For file tools, show the path
	if (["file_read", "file_write", "file_edit"].includes(toolName)) {
		const path = (args.path as string) || (args.file_path as string) || "";
		if (path) return path.length > 60 ? path.slice(0, 57) + "..." : path;
	}

	// Generic: show first arg value
	const firstVal = entries[0]?.[1];
	if (firstVal && typeof firstVal === "string") {
		return firstVal.length > 60 ? firstVal.slice(0, 57) + "..." : firstVal;
	}

	// Fallback: count
	return entries.length <= 2
		? entries.map(([k, v]) => {
			const val = typeof v === "string" ? v.slice(0, 30) : JSON.stringify(v)?.slice(0, 30);
			return `${k}: ${val}`;
		}).join(", ")
		: `${entries.length} args`;
}

/** Format result into preview lines */
function formatResult(result: unknown): string[] | null {
	if (result === undefined || result === null) return null;

	let text: string;
	if (typeof result === "string") {
		text = result;
	} else if (typeof result === "object") {
		const obj = result as Record<string, unknown>;
		// Extract output from tool result objects like {"output": "..."}
		text = (obj.output as string) || (obj.text as string) || (obj.result as string) || "";
		if (!text) text = JSON.stringify(result);
	} else {
		text = JSON.stringify(result);
	}

	if (!text || text.trim().length === 0) return null;

	// Clean up \r\n to \n
	text = text.replace(/\r\n/g, "\n").replace(/\r/g, "\n");

	const lines = text.split("\n").filter((l: string) => l.trim());
	if (lines.length === 0) return null;

	// Show max 3 lines, with "+N more" indicator
	if (lines.length <= 3) return lines;
	return [...lines.slice(0, 3), `... +${lines.length - 3} more lines`];
}
