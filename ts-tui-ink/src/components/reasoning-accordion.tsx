/**
 * ReasoningAccordion — collapsible thinking block using ChainOfThoughtPrimitive.
 *
 * Uses ChainOfThoughtPrimitive.Root and AccordionTrigger.
 * When collapsed, shows a single "Thinking..." line.
 * When expanded, shows the full reasoning text with blockquote bar.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	ChainOfThoughtPrimitive,
	useAuiState,
} from "@assistant-ui/react-ink";
import { colors, BLOCKQUOTE_BAR } from "../theme.js";

export function ReasoningAccordion() {
	const parts = useAuiState((s) => s.message.parts);
	const collapsed = useAuiState((s) => s.chainOfThought?.collapsed);

	// Find reasoning parts
	const reasoningParts = (parts || []).filter(
		(p: any) => p.type === "reasoning",
	);
	if (reasoningParts.length === 0) return null;

	const fullText = reasoningParts
		.map((p: any) => p.text || "")
		.join("\n");
	if (!fullText.trim()) return null;

	return (
		<ChainOfThoughtPrimitive.Root flexDirection="column" marginBottom={1}>
			{/* Accordion trigger — click to toggle */}
			<ChainOfThoughtPrimitive.AccordionTrigger>
				<Box>
					<Text color="gray">{BLOCKQUOTE_BAR} </Text>
					<Text dimColor italic>
						{collapsed !== false
							? `Thinking... (${fullText.split("\n").length} lines, press to expand)`
							: "Thinking (press to collapse)"}
					</Text>
				</Box>
			</ChainOfThoughtPrimitive.AccordionTrigger>

			{/* Content — only show when expanded */}
			{collapsed === false && (
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
		</ChainOfThoughtPrimitive.Root>
	);
}
