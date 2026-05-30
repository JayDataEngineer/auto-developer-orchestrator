/**
 * AssistantMessage — renders assistant messages via library pipeline.
 *
 * Uses MessagePrimitive.Parts with the children render function API.
 * Registered tool UIs (makeAssistantToolUI) are automatically resolved
 * via part.toolUI — no manual parts.map() switch/case needed.
 *
 * Reasoning parts are reordered in the adapter (reorderParts) so they
 * come first. We extract all reasoning text and render a single collapsed
 * "Thought:" line, then the parts pipeline handles the rest.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	useAuiState,
	MessagePrimitive,
	LoadingPrimitive,
} from "@assistant-ui/react-ink";
import { MarkdownText } from "./markdown-text.js";
import { TerminalImage } from "./terminal-image.js";
import { BranchPicker } from "./branch-picker.js";
import { useColors, BLOCKQUOTE_BAR } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

// ── Truncation helper ──

function trunc(s: string, max: number): string {
	if (s.length <= max) return s;
	const cut = s.slice(0, max - 1);
	const lastSpace = cut.lastIndexOf(" ");
	if (lastSpace < max * 0.5) return cut + "…";
	return cut.slice(0, lastSpace) + "…";
}

// ── Main component ──

export function AssistantMessage() {
	const colors = useColors();
	const { cols } = useTerminalSize();
	const textWidth = cols - 2;
	const isRunning = useAuiState((s) => s.message.status?.type === "running");

	// Extract all reasoning text from parts (reorderParts puts them first)
	const parts = useAuiState((s) => s.message.parts);
	const hasContent = parts && parts.some((p: any) =>
		(p.type === "text" && p.text?.trim()) ||
		p.type === "tool-call" ||
		p.type === "reasoning" ||
		p.type === "source"
	);
	const reasoningParts = parts.filter((p: any) => p.type === "reasoning" && p.text?.trim());
	const allReasoning = reasoningParts.map((p: any) => p.text).join("\n");
	const hasReasoning = allReasoning.length > 0;

	// Get the most recent (last) line of reasoning for display
	const reasoningLines = allReasoning.split("\n").filter((l: string) => l.trim());
	const lastReasoningLine = reasoningLines.length > 0
		? reasoningLines[reasoningLines.length - 1]
		: "";

	// Only show waiting spinner for THIS message (message-level running,
	// not thread-level). Shows only when running AND no content yet.
	const showSpinner = isRunning && !hasContent;

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1} width={textWidth}>
			{showSpinner && (
				<LoadingPrimitive.Root gap={1}>
					<LoadingPrimitive.Spinner variant="spinner" type="dots" />
					<LoadingPrimitive.ElapsedTime />
				</LoadingPrimitive.Root>
			)}

			{/* Collapsed reasoning — one line for all thought steps */}
			{hasReasoning && (
				<Box marginBottom={1}>
					<Text dimColor color={colors.textMuted}>
						{BLOCKQUOTE_BAR} {trunc(lastReasoningLine, cols - 4)}
					</Text>
				</Box>
			)}

			{/* Parts pipeline — children render function (preferred API) */}
			<MessagePrimitive.Parts>
				{({ part }) => {
					switch (part.type) {
						case "reasoning":
							// Skip — already rendered above as collapsed block
							return null;
						case "text": {
							if (!part.text?.trim()) return null;
							return (
								<MarkdownText
									key={part.text.slice(0, 20)}
									text={part.text}
									color={colors.text}
								/>
							);
						}
						case "tool-call": {
							// Registered tool UIs are resolved via part.toolUI.
							// Unregistered tools get a compact one-line indicator (no animated spinner).
							if (part.toolUI) return part.toolUI;
							return <CompactToolCall key={part.toolCallId} part={part} />;
						}
						case "image":
							return (
								<Box key={part.image?.slice(0, 20)} marginTop={1} paddingLeft={1}>
									<TerminalImage
										image={part.image}
										filename={(part as any).filename}
									/>
								</Box>
							);
						case "source":
							return (
								<Box key={(part as any).id || part.url} paddingLeft={1}>
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
								<Box paddingLeft={1}>
									<Text dimColor>{BLOCKQUOTE_BAR} file: {(part as any).name || "(unnamed)"}</Text>
								</Box>
							);
						default:
							return null;
					}
				}}
			</MessagePrimitive.Parts>

			{/* Branch picker for forked messages */}
			<BranchPicker />
		</Box>
	);
}

// ── Compact tool call (replaces ToolCallPrimitive.Fallback) ──
// One line per tool call, no animated spinner. Avoids frame accumulation
// when multiple tool calls run simultaneously.

function CompactToolCall({ part }: { part: any }) {
	const colors = useColors();
	const status = part.status?.type;
	const toolName = part.toolName || "tool";
	const icon = status === "running" ? "●" : status === "error" ? "✗" : "✓";
	const color = status === "running" ? colors.running : status === "error" ? "red" : colors.success;

	// Show abbreviated args when running
	let detail = "";
	if (status === "running" && part.args) {
		try {
			const args = typeof part.args === "string" ? JSON.parse(part.args) : part.args;
			const goal = args.goal || args.task || args.command || args.path || "";
			if (goal) detail = `: ${trunc(String(goal), 60)}`;
		} catch {
			// args might not be JSON
		}
	}

	return (
		<Box paddingLeft={1}>
			<Text color={color}>{icon} </Text>
			<Text color={colors.textMuted}>{toolName}{detail}</Text>
		</Box>
	);
}
