/**
 * AssistantMessage — renders assistant messages with text, reasoning, and tools.
 *
 * Style: BLACK_CIRCLE prefix + "Pux" label for the response block.
 * Tool calls get their own compact rows. Text flows below.
 * Matches Claude Code's ⏺ pattern.
 */

import React from "react";
import { Box, Text } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { ToolFallback } from "./tool-fallback.js";
import { ReasoningBlock } from "./reasoning.js";
import { colors, BLACK_CIRCLE } from "../theme.js";

export function AssistantMessage() {
	const parts = useAuiState((s) => s.message.parts);

	if (!parts || parts.length === 0) return null;

	// Separate tool calls from content for layout
	const toolCalls = parts.filter((p: any) => p.type === "tool-call");
	const contentParts = parts.filter((p: any) => p.type !== "tool-call");
	const hasContent = contentParts.length > 0;

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1}>
			{/* Tool calls — compact list */}
			{toolCalls.length > 0 && (
				<Box flexDirection="column">
					{toolCalls.map((part: any, i: number) => (
						<ToolFallback
							key={part.toolCallId || i}
							toolName={part.toolName}
							args={part.args}
							argsText={part.argsText}
							result={part.result}
							isError={part.isError}
						/>
					))}
				</Box>
			)}

			{/* Content — text + reasoning */}
			{hasContent && (
				<Box flexDirection="column">
					{/* Assistant label */}
					{contentParts.some((p: any) => p.type === "text" && p.text?.trim()) && (
						<Box>
							<Text color={colors.assistant}>{BLACK_CIRCLE} </Text>
							<Text color={colors.assistant} bold>Pux</Text>
						</Box>
					)}
					{contentParts.map((part: any, i: number) => {
						switch (part.type) {
							case "reasoning":
								return <ReasoningBlock key={i} text={part.text} />;
							case "text":
								if (!part.text?.trim()) return null;
								return (
									<Box key={i} flexDirection="column" paddingLeft={2}>
										{part.text.split("\n").map((line: string, j: number) => (
											<Text key={j}>{line || " "}</Text>
										))}
									</Box>
								);
							default:
								return null;
						}
					})}
				</Box>
			)}
		</Box>
	);
}
