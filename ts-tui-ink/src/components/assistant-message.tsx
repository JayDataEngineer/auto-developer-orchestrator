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

import React, { useState, useEffect, useRef } from "react";
import { Box, Text } from "ink";
import {
	useAuiState,
	MessagePrimitive,
} from "@assistant-ui/react-ink";
import { MarkdownText as _MarkdownText } from "@assistant-ui/react-ink-markdown";
import type { Theme as MarkdansiTheme } from "markdansi";
// Extend to pass tableTruncate through the spread (not in library's types yet)
const MarkdownText = _MarkdownText as React.FC<
	React.ComponentProps<typeof _MarkdownText> & { tableTruncate?: boolean }
>;

// Markdansi theme matching Claude Code's palette — no dark gray
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
	const isRunning = useAuiState((s) => s.message.status?.type === "running");

	// Track elapsed time — persists after completion
	const startRef = useRef(Date.now());
	const [elapsed, setElapsed] = useState(0);
	useEffect(() => {
		if (!isRunning) return;
		const timer = setInterval(() => setElapsed(Math.floor((Date.now() - startRef.current) / 1000)), 200);
		return () => clearInterval(timer);
	}, [isRunning]);

	// Spinner animation
	const [frame, setFrame] = useState(0);
	useEffect(() => {
		if (!isRunning) return;
		const timer = setInterval(() => setFrame((f) => (f + 1) % 10), 80);
		return () => clearInterval(timer);
	}, [isRunning]);
	const spinnerChars = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"];

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

	// Split reasoning into lines for blockquote-style display
	const reasoningLines = allReasoning.split("\n").filter((l: string) => l.trim());

	// Only show initial spinner when no content yet
	const showSpinner = isRunning && !hasContent;

	// Format elapsed time string
	function fmtTime(secs: number): string {
		if (secs < 60) return `${secs}s`;
		const m = Math.floor(secs / 60);
		const s = secs % 60;
		return `${m}m ${s}s`;
	}

	return (
		<Box flexDirection="column" marginTop={1} paddingRight={1}>
			{/* Initial spinner — shown while waiting for first content */}
			{showSpinner && (
				<Box gap={1}>
					<Text color={colors.running}>{spinnerChars[frame]}</Text>
					<Text color={colors.textMuted}>({fmtTime(elapsed)})</Text>
				</Box>
			)}

			{/* Reasoning — all lines, blockquote-style */}
			{hasReasoning && (
				<Box marginBottom={1} paddingLeft={2} flexDirection="column">
					{reasoningLines.map((line, i) => (
						<Text key={i} color={colors.textMuted} wrap="wrap">
							{BLOCKQUOTE_BAR} {line}
						</Text>
					))}
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
								<Box key={part.text.slice(0, 20)} paddingLeft={2}>
									<MarkdownText
										text={part.text}
										tableTruncate={false}
										theme={mdTheme}
									/>
								</Box>
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
									<Text color={colors.textMuted}>{BLOCKQUOTE_BAR} </Text>
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
									<Text color={colors.textMuted}>{BLOCKQUOTE_BAR} file: {(part as any).name || "(unnamed)"}</Text>
								</Box>
							);
						default:
							return null;
					}
				}}
			</MessagePrimitive.Parts>

			{/* Branch picker for forked messages */}
			<BranchPicker />

			{/* Completion time — shown after message finishes */}
			{!isRunning && hasContent && (
				<Box marginTop={1}>
					<Text color={colors.textMuted}>● Completed in {fmtTime(elapsed || Math.floor((Date.now() - startRef.current) / 1000))}</Text>
				</Box>
			)}
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
		<Box>
			<Text color={color}>{icon} </Text>
			<Text color={colors.textMuted}>{toolName}{detail}</Text>
		</Box>
	);
}
