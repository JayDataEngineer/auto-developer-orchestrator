/**
 * AssistantMessage — renders assistant messages via library pipeline.
 *
 * Uses MessagePrimitive.Parts with the children render function API.
 * Registered tool UIs (makeAssistantToolUI) are automatically resolved
 * via part.toolUI.
 *
 * Reasoning parts are reordered in the adapter (reorderParts) so they
 * come first. Text and tool-call parts stay in their natural stream order.
 * Thinking blocks are collapsible via Ctrl+E.
 */

import React, { useState, useEffect, useRef } from "react";
import { Box, Text } from "ink";
import {
	useAuiState,
	MessagePrimitive,
} from "@assistant-ui/react-ink";
import { MarkdownText as _MarkdownText } from "@assistant-ui/react-ink-markdown";
import { render as renderMd, type Theme as MarkdansiTheme } from "markdansi";
import { usePuxStore } from "@pux/shared";
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

function normalizeText(text: string): string {
	return text
		.replace(/\r/g, "")
		.split(/\n\n+/)
		.map(para => para
			.replace(/\n/g, " ")
			.replace(/\.([A-Z])/g, ". $1")
			.replace(/ +/g, " ")
			.trim())
		.filter(para => para.length > 0)
		.join("\n\n");
}

// ── Render markdown paragraph ──

function renderMarkdown(text: string, cols: number, theme: MarkdansiTheme): string {
	const rendered = renderMd(text, { width: cols - 3, theme });
	return rendered.replace(/\n$/, "");
}

// ── Main component ──

export function AssistantMessage() {
	const colors = useColors();
	const { cols } = useTerminalSize();
	const isRunning = useAuiState((s) => s.message.status?.type === "running");

	const startRef = useRef(Date.now());
	const [elapsed, setElapsed] = useState(0);
	useEffect(() => {
		if (!isRunning) return;
		const timer = setInterval(() => setElapsed(Math.floor((Date.now() - startRef.current) / 1000)), 200);
		return () => clearInterval(timer);
	}, [isRunning]);

	const [frame, setFrame] = useState(0);
	useEffect(() => {
		if (!isRunning) return;
		const timer = setInterval(() => setFrame((f) => (f + 1) % 10), 80);
		return () => clearInterval(timer);
	}, [isRunning]);
	const spinnerChars = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"];

	const parts = useAuiState((s) => s.message.parts);
	const hasContent = parts && parts.some((p: any) =>
		(p.type === "text" && p.text?.trim()) ||
		p.type === "tool-call" ||
		p.type === "reasoning" ||
		p.type === "source"
	);

	const textWidth = Math.max(20, cols - 5);
	const showSpinner = isRunning && !hasContent;
	const providerRetry = usePuxStore((s) => s.providerRetry);
	const thinkingExpanded = usePuxStore((s) => s.thinkingExpanded);

	function fmtTime(secs: number): string {
		if (secs < 60) return `${secs}s`;
		const m = Math.floor(secs / 60);
		const s = secs % 60;
		return `${m}m ${s}s`;
	}

	return (
		<Box flexDirection="column" marginTop={1} paddingRight={1}>
			{/* Initial spinner */}
			{showSpinner && !providerRetry && (
				<Box gap={1}>
					<Text color={colors.running}>{spinnerChars[frame]}</Text>
					<Text color={colors.textMuted}>({fmtTime(elapsed)})</Text>
				</Box>
			)}

			{/* Provider retry indicator */}
			{providerRetry && (
				<Box gap={1}>
					<Text color={colors.running}>{spinnerChars[frame]}</Text>
					<Text color={colors.textMuted}>
						Retrying ({providerRetry.attempt}/{providerRetry.maxRetry}) in {providerRetry.backoffSecs}s — {providerRetry.error.slice(0, 60)}
					</Text>
				</Box>
			)}

			{/* Parts pipeline — children render function.
			    key forces re-render when thinkingExpanded changes so
			    collapsed/expanded reasoning actually updates. */}
			<MessagePrimitive.Parts key={thinkingExpanded ? 'expanded' : 'collapsed'}>
				{({ part }) => {
					switch (part.type) {
						case "reasoning": {
							const rText = (part as any).text || "";
							if (!rText.trim()) return null;
							const rParagraphs = rText.split(/\n\n+/).filter((p: string) => p.trim());
							if (rParagraphs.length === 0) return null;
							const rLines: string[] = [];
							rParagraphs.forEach((para: string, pIdx: number) => {
								if (pIdx > 0) rLines.push("");
								for (const line of para.split("\n")) {
									if (!line.trim()) continue;
									rLines.push(...wrapText(line, textWidth));
								}
							});
							if (rLines.length === 0) return null;

							// Collapsed: one-line header
							if (!thinkingExpanded) {
								return (
									<Box marginBottom={1}>
										<Text color={colors.textMuted}>
											{BLOCKQUOTE_BAR} {"\u25B6"} Thinking ({rLines.length} lines){"  "}
											<Text dimColor>Ctrl+L to expand</Text>
										</Text>
									</Box>
								);
							}

							// Expanded: full thinking text
							return (
								<Box marginBottom={1} paddingLeft={2} flexDirection="column">
									{rLines.map((line, i) => (
										<Text key={i} color={colors.textMuted}>
											{BLOCKQUOTE_BAR} {line}
										</Text>
									))}
								</Box>
							);
						}
						case "text": {
							const rawText = (part as any).text || "";
							if (!rawText.trim()) return null;
							const normalized = normalizeText(rawText);
							if (!normalized) return null;
							const paragraphs = normalized.split(/\n\n+/).filter((p: string) => p.trim());
							if (paragraphs.length <= 1) {
								return (
									<Box paddingLeft={2}>
										<MarkdownText
											text={normalized}
											tableTruncate={false}
											theme={mdTheme}
											width={cols - 3}
										/>
									</Box>
								);
							}
							return (
								<Box paddingLeft={2} flexDirection="column">
									{paragraphs.map((para: string, i: number) => (
										<Text key={i}>{renderMarkdown(para, cols, mdTheme)}</Text>
									))}
								</Box>
							);
						}
						case "tool-call": {
							// Registered tool UIs are resolved via part.toolUI.
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

			{/* Completion time */}
			{!isRunning && hasContent && (
				<Box marginTop={1}>
					<Text color={colors.textMuted}>● Completed in {fmtTime(elapsed || Math.floor((Date.now() - startRef.current) / 1000))}</Text>
				</Box>
			)}
		</Box>
	);
}

// ── Compact tool call (for unregistered tools) ──

function CompactToolCall({ part }: { part: any }) {
	const colors = useColors();
	const status = part.status?.type;
	const toolName = part.toolName || "tool";
	const icon = status === "running" ? "●" : status === "error" ? "✗" : "✓";
	const color = status === "running" ? colors.running : status === "error" ? "red" : colors.success;

	let detail = "";
	if (status === "running" && part.args) {
		try {
			const args = typeof part.args === "string" ? JSON.parse(part.args) : part.args;
			const goal = args.goal || args.task || args.command || args.path || "";
			if (goal) detail = `: ${trunc(String(goal), 60)}`;
		} catch {}
	}

	return (
		<Box marginBottom={1}>
			<Text color={color}>{icon} </Text>
			<Text color={colors.textMuted}>{toolName}{detail}</Text>
		</Box>
	);
}
