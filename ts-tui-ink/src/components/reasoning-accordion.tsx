/**
 * ReasoningAccordion — collapsible thinking block.
 *
 * Collapsed: single dim line "▎ Thinking..." with preview (truncated to fit terminal)
 * Expanded: full reasoning with blockquote bars.
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

	// Collapsed: show a short preview of the first line
	// Width budget: bar(2) + "Thinking..."(11) + space(1) = 14 overhead
	if (!expanded) {
		const maxPreview = Math.max(20, cols - 16);
		const preview = lines[0]?.slice(0, maxPreview) || "";
		if (isRunning) {
			return (
				<Box marginBottom={1}>
					<Text color={colors.subtle}>{BLOCKQUOTE_BAR} </Text>
					<Text dimColor italic color={colors.subtle}>
						Thinking
					</Text>
					{preview && <Text dimColor color={colors.textMuted}> {preview}</Text>}
				</Box>
			);
		}
		return (
			<Box marginBottom={1}>
				<Text color={colors.subtle}>{BLOCKQUOTE_BAR} </Text>
				<Text dimColor color={colors.subtle}>
					Thought
				</Text>
				{preview && <Text dimColor color={colors.textMuted}> — {preview}</Text>}
			</Box>
		);
	}

	// Expanded: show all lines with blockquote bars
	const maxLines = 8;
	const truncated = lines.length > maxLines;
	const displayLines = truncated ? lines.slice(0, maxLines) : lines;

	return (
		<Box flexDirection="column" marginBottom={1}>
			{displayLines.map((line: string, i: number) => (
				<Box key={i}>
					<Text color={colors.subtle}>{BLOCKQUOTE_BAR} </Text>
					<Box flexGrow={1} flexDirection="column">
						<Text dimColor italic color={colors.textDim}>{line}</Text>
					</Box>
				</Box>
			))}
			{truncated && (
				<Text dimColor color={colors.textMuted}>  ... +{lines.length - maxLines} more</Text>
			)}
		</Box>
	);
}
