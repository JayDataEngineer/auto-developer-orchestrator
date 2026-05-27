/**
 * ReasoningAccordion — collapsible thinking block.
 *
 * Collapsed: "▎ Thinking" label + full first line (wraps at terminal width)
 * Expanded: full reasoning with blockquote bars (each line wraps).
 * Always collapsed when message is complete.
 */

import React, { useState, useEffect } from "react";
import { Box, Text } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { BLOCKQUOTE_BAR } from "../theme.js";
import { useColors } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

export function ReasoningAccordion() {
	const parts = useAuiState((s) => s.message.parts);
	const isRunning = useAuiState((s) => s.message.status?.type === "running");
	const colors = useColors();
	const { cols } = useTerminalSize();
	const textWidth = cols - 4; // paddingX(1) from parent + BLOCKQUOTE_BAR + space

	// Hooks MUST be called before any early returns (React hooks rules)
	const [expanded, setExpanded] = useState(false);
	useEffect(() => {
		if (!isRunning) setExpanded(false);
	}, [isRunning]);

	const reasoningParts = (parts || []).filter(
		(p: any) => p.type === "reasoning",
	);
	if (reasoningParts.length === 0) return null;

	const fullText = reasoningParts
		.map((p: any) => p.text || "")
		.join("\n");
	if (!fullText.trim()) return null;

	const lines = fullText.split("\n").filter((l: string) => l.trim());

	// Collapsed: label + first line, wraps at terminal width
	if (!expanded) {
		const firstLine = lines[0] || "";
		const label = isRunning ? "Thinking" : "Thought";
		return (
			<Box flexDirection="column" marginBottom={1} width={textWidth}>
				<Text>
					<Text color={colors.subtle}>{BLOCKQUOTE_BAR} </Text>
					<Text dimColor italic color={colors.subtle}>{label}</Text>
					{firstLine && <Text dimColor color={colors.textMuted}> {firstLine}</Text>}
				</Text>
			</Box>
		);
	}

	// Expanded: show all lines with blockquote bars, each wraps
	const maxLines = 8;
	const truncated = lines.length > maxLines;
	const displayLines = truncated ? lines.slice(0, maxLines) : lines;

	return (
		<Box flexDirection="column" marginBottom={1}>
			{displayLines.map((line: string, i: number) => (
				<Box key={i} width={textWidth}>
					<Text>
						<Text color={colors.subtle}>{BLOCKQUOTE_BAR} </Text>
						<Text dimColor italic color={colors.textDim}>{line}</Text>
					</Text>
				</Box>
			))}
			{truncated && (
				<Text dimColor color={colors.textMuted}>  ... +{lines.length - maxLines} more</Text>
			)}
		</Box>
	);
}
