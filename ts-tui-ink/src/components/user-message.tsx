/**
 * UserMessage — renders user messages.
 *
 * Style: colored "> text" with visual weight, matching Claude Code CLI.
 */

import React from "react";
import { Box, Text } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { useColors } from "../theme.js";

export function UserMessage() {
	const text = useAuiState((s) => {
		const parts = s.message.parts;
		const textPart = parts.find((p: any) => p.type === "text");
		return textPart ? (textPart as any).text : "";
	});
	const colors = useColors();

	if (!text) return null;

	const lines = text.split("\n");
	const firstLine = lines[0];

	return (
		<Box flexDirection="column" marginTop={1} marginBottom={1} paddingX={1}>
			<Text>
				<Text color={colors.brand} bold>{">"}</Text>
				<Text> </Text>
				<Text color={colors.text}>{firstLine}</Text>
			</Text>
			{lines.slice(1).map((line: string, i: number) => (
				<Text key={i} color={colors.text}>  {line}</Text>
			))}
		</Box>
	);
}
