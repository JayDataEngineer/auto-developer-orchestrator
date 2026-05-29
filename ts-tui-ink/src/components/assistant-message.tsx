/**
 * AssistantMessage — renders assistant messages using the library pipeline.
 *
 * Uses MessagePrimitive.Parts with custom components so:
 * - Reasoning is grouped (via ReasoningGroup) and shown as a collapsed block
 * - Tool calls route to registered makeAssistantToolUI renderers
 * - Unregistered tools fall back to PuxToolFallback (single-line display)
 * - The library's grouping algorithm (groupMessageParts) handles consecutive parts
 */

import React, { useState, useEffect } from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import {
	useAuiState,
	MessagePrimitive,
} from "@assistant-ui/react-ink";
import type { ToolCallMessagePartProps } from "@assistant-ui/react-ink";
import { getToolArgPreview } from "@pux/shared";
import { BranchPicker } from "./branch-picker.js";
import { MarkdownText } from "./markdown-text.js";
import { TerminalImage } from "./terminal-image.js";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

// Truncate at word boundary — never cuts mid-word
function trunc(s: string, max: number): string {
	if (s.length <= max) return s;
	const cut = s.slice(0, max - 1);
	const lastSpace = cut.lastIndexOf(" ");
	if (lastSpace < max * 0.5) return cut + "…";
	return cut.slice(0, lastSpace) + "…";
}

// ── Reasoning block — renders all reasoning parts in a range as one collapsed block ──

function ReasoningBlock({ startIndex, endIndex }: { startIndex: number; endIndex: number }) {
	const colors = useColors();
	const { cols } = useTerminalSize();
	const isRunning = useAuiState((s) => s.message.status?.type === "running");

	// Read all reasoning parts in the range and join their text
	const parts = useAuiState((s) => s.message.parts);
	const reasoningParts = parts.slice(startIndex, endIndex + 1);
	const allText = reasoningParts
		.filter((p: any) => p.type === "reasoning" && p.text?.trim())
		.map((p: any) => p.text)
		.join("\n");

	if (!allText) return null;

	const lines = allText.split("\n").filter((l) => l.trim());
	if (lines.length === 0) return null;

	const label = isRunning ? "Thinking" : "Thought";
	const maxWidth = cols - 4; // indent + bar + space + label
	const lastLine = lines[lines.length - 1];
	const preview = trunc(lastLine, maxWidth);

	return (
		<Box flexDirection="column" marginBottom={1}>
			<Text dimColor color={colors.textMuted}>
				{BLOCKQUOTE_BAR} {label}: {preview}
			</Text>
		</Box>
	);
}

// ── Context-aware part readers ──

function MarkdownTextFromContext() {
	const colors = useColors();
	const part = useAuiState((s) => s.part as any);
	if (part.type !== "text" || !part.text?.trim()) return null;
	return <MarkdownText text={part.text} color={colors.text} />;
}

function ImageFromContext() {
	const part = useAuiState((s) => s.part as any);
	if (part.type !== "image") return null;
	return (
		<Box marginTop={1} paddingLeft={1}>
			<TerminalImage image={part.image} filename={part.filename} />
		</Box>
	);
}

function SourceFromContext() {
	const part = useAuiState((s) => s.part as any);
	if (part.type !== "source") return null;
	return (
		<Box paddingLeft={1}>
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
}

function FileFromContext() {
	const part = useAuiState((s) => s.part as any);
	if (part.type !== "file") return null;
	return (
		<Box paddingLeft={1}>
			<Text dimColor>{BLOCKQUOTE_BAR} file: {part.name || "(unnamed)"}</Text>
		</Box>
	);
}

// ── Fallback for unregistered tools — single line with status ──

function PuxToolFallback({ toolName, args, result, isError, status }: ToolCallMessagePartProps) {
	const colors = useColors();
	const { cols } = useTerminalSize();
	const isDone = status?.type === "complete";
	const isRunning = status?.type === "running";

	const rawArg = getToolArgPreview(toolName, args as Record<string, unknown> | undefined);
	const maxArgLen = Math.max(10, cols - toolName.length - 24);
	const argPreview = trunc(rawArg, maxArgLen);

	const sym = isRunning ? symbols.toolRunning : isError ? symbols.toolError : symbols.toolDone;
	const errMsg = isError && typeof result === "string" ? result : "";
	const errPreview = errMsg ? trunc(errMsg, maxArgLen) : "";

	return (
		<Box flexDirection="column">
			<Text wrap="truncate-end">
				<Text color={isError ? colors.error : isDone ? colors.success : colors.running}>{sym}</Text>
				<Text> </Text>
				<Text bold color={isRunning ? colors.running : undefined}>{toolName}</Text>
				{argPreview && <Text color={colors.textMuted}> {argPreview}</Text>}
				{isDone && !isError && <Text color={colors.textMuted}> done</Text>}
				{isError && <Text color={colors.error}> failed</Text>}
			</Text>
			{errPreview && (
				<Box paddingLeft={2}>
					<Text dimColor color={colors.error}>{trunc(errMsg, cols - 4)}</Text>
				</Box>
			)}
		</Box>
	);
}

// ── Main component ──

export function AssistantMessage() {
	const parts = useAuiState((s) => s.message.parts);
	const isRunning = useAuiState((s) => s.message.status?.type === "running");
	const colors = useColors();
	const { cols } = useTerminalSize();
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

	// Show spinner while running with no visible content yet
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
			<MessagePrimitive.Parts
				components={{
					// Individual reasoning parts hidden — ReasoningBlock handles the group
					Reasoning: () => null,
					ReasoningGroup: ({ startIndex, endIndex }) => (
						<ReasoningBlock startIndex={startIndex} endIndex={endIndex} />
					),
					Text: () => <MarkdownTextFromContext />,
					Image: () => <ImageFromContext />,
					Source: () => <SourceFromContext />,
					File: () => <FileFromContext />,
					tools: { Fallback: PuxToolFallback },
					ToolGroup: ({ children }) => <>{children}</>,
				}}
			/>
			<BranchPicker />
		</Box>
	);
}
