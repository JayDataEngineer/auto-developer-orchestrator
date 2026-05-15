/**
 * UserMessage — renders user messages.
 *
 * Style: bold green "You:" label on first line, then the text body.
 * Matches the two-line pattern common in chat CLIs.
 */

import React from "react";
import { Box, Text } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { colors, BLACK_CIRCLE } from "../theme.js";

export function UserMessage() {
	const text = useAuiState((s) => {
		const parts = s.message.parts;
		const textPart = parts.find((p: any) => p.type === "text");
		return textPart ? (textPart as any).text : "";
	});

	if (!text) return null;

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1}>
			<Text color={colors.user} bold>You</Text>
			<Box>
				<Text>{text}</Text>
			</Box>
		</Box>
	);
}
