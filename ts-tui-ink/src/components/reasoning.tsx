/**
 * ReasoningBlock — thinking/reasoning display.
 *
 * Style: dim italic with blockquote bar (▎), collapsed to a few lines.
 * Matches Claude Code's thinking block style.
 */

import React from "react";
import { Box, Text } from "ink";
import { BLOCKQUOTE_BAR } from "../theme.js";

interface ReasoningBlockProps {
	text: string;
}

export function ReasoningBlock({ text }: ReasoningBlockProps) {
	if (!text) return null;

	const lines = text.split("\n");
	const maxLines = 4;
	const truncated = lines.length > maxLines;
	const displayLines = truncated ? lines.slice(0, maxLines) : lines;

	return (
		<Box flexDirection="column" marginBottom={1} paddingLeft={2}>
			{displayLines.map((line, i) => (
				<Box key={i}>
					<Text color="gray">{BLOCKQUOTE_BAR} </Text>
					<Text dimColor italic>{line}</Text>
				</Box>
			))}
			{truncated && (
				<Text dimColor color="gray">  ...</Text>
			)}
		</Box>
	);
}
