/**
 * UserMessage — renders user messages.
 *
 * Style: grey "> text" inline, matching Claude Code CLI.
 */

import React from "react";
import { Box, Text } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";

export function UserMessage() {
	const text = useAuiState((s) => {
		const parts = s.message.parts;
		const textPart = parts.find((p: any) => p.type === "text");
		return textPart ? (textPart as any).text : "";
	});

	if (!text) return null;

	// Show first line inline, subsequent lines below
	const lines = text.split("\n");
	const firstLine = lines[0];

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1}>
			<Text>
				<Text color="gray">{">"} </Text>
				<Text color="gray">{firstLine}</Text>
			</Text>
			{lines.slice(1).map((line: string, i: number) => (
				<Text key={i} color="gray">  {line}</Text>
			))}
		</Box>
	);
}
