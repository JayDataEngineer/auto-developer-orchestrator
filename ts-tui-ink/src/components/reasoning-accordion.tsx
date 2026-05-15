/**
 * ReasoningAccordion — collapsible thinking block.
 *
 * Collapsed: single dim line "▎ Thinking..."
 * Expanded: full reasoning with blockquote bars.
 * Always collapsed when message is complete.
 */

import React, { useState, useEffect } from "react";
import { Box, Text } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { BLOCKQUOTE_BAR } from "../theme.js";

export function ReasoningAccordion() {
	const parts = useAuiState((s) => s.message.parts);
	const isRunning = useAuiState((s) => s.message.status?.type === "running");

	const reasoningParts = (parts || []).filter(
		(p: any) => p.type === "reasoning",
	);
	if (reasoningParts.length === 0) return null;

	const fullText = reasoningParts
		.map((p: any) => p.text || "")
		.join("\n");
	if (!fullText.trim()) return null;

	const lines = fullText.split("\n").filter((l: string) => l.trim());

	// Auto-collapse when message completes
	const [expanded, setExpanded] = useState(false);
	useEffect(() => {
		if (!isRunning) setExpanded(false);
	}, [isRunning]);

	// Collapsed: show a short preview of the first line
	if (!expanded) {
		const preview = lines[0]?.slice(0, 80) || "";
		return (
			<Box marginBottom={1}>
				<Text color="gray">{BLOCKQUOTE_BAR} </Text>
				{isRunning ? (
					<Text dimColor italic>Thinking... {preview}</Text>
				) : (
					<Text dimColor italic>Thought ({lines.length} lines)</Text>
				)}
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
					<Text color="gray">{BLOCKQUOTE_BAR} </Text>
					<Text dimColor italic>{line}</Text>
				</Box>
			))}
			{truncated && (
				<Text dimColor color="gray">  ... +{lines.length - maxLines} more</Text>
			)}
		</Box>
	);
}
