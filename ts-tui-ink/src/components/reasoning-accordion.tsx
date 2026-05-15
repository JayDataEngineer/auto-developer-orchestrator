/**
 * ReasoningAccordion — collapsible thinking block.
 *
 * Shows reasoning/thinking content from assistant messages.
 * When collapsed, shows a single "Thinking..." line.
 * When expanded, shows the full reasoning text with blockquote bar.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { colors, BLOCKQUOTE_BAR } from "../theme.js";

export function ReasoningAccordion() {
	const parts = useAuiState((s) => s.message.parts);
	const [expanded, setExpanded] = useState(false);

	// Find reasoning parts
	const reasoningParts = (parts || []).filter(
		(p: any) => p.type === "reasoning",
	);
	if (reasoningParts.length === 0) return null;

	const fullText = reasoningParts
		.map((p: any) => p.text || "")
		.join("\n");
	if (!fullText.trim()) return null;

	const lineCount = fullText.split("\n").length;

	useInput((_input: string, key: any) => {
		if (key.return && _input === "") {
			// Only toggle when a special key combo is used, not regular input
		}
	});

	return (
		<Box flexDirection="column" marginBottom={1}>
			<Box>
				<Text color="gray">{BLOCKQUOTE_BAR} </Text>
				<Text dimColor italic>
					{expanded
						? `Thinking (${lineCount} lines)`
						: `Thinking... (${lineCount} lines)`}
				</Text>
			</Box>

			{expanded && (
				<Box flexDirection="column" paddingLeft={2}>
					{fullText.split("\n").map((line: string, i: number) => (
						<Box key={i}>
							<Text color="gray">{BLOCKQUOTE_BAR} </Text>
							<Text dimColor italic>
								{line || " "}
							</Text>
						</Box>
					))}
				</Box>
			)}
		</Box>
	);
}
