/**
 * AssistantMessage — renders assistant messages using assistant-ui primitives.
 *
 * Uses MessagePrimitive.If for conditional rendering,
 * ToolCallPrimitive.Fallback for tool calls,
 * and proper part routing for text/reasoning/error.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	useAuiState,
	MessagePrimitive,
} from "@assistant-ui/react-ink";
import { ReasoningBlock } from "./reasoning.js";
import { ToolFallback } from "./tool-fallback.js";
import { colors, BLACK_CIRCLE, symbols } from "../theme.js";

export function AssistantMessage() {
	const parts = useAuiState((s) => s.message.parts);

	if (!parts || parts.length === 0) return null;

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1}>
			{/* Parts: tool calls, text, reasoning, errors */}
			{parts.map((part: any, i: number) => {
				switch (part.type) {
					case "tool-call":
						return (
							<Box key={part.toolCallId || i} flexDirection="column">
								<ToolCallItem
									toolCallId={part.toolCallId}
									toolName={part.toolName}
									args={part.args}
									argsText={part.argsText}
									result={part.result}
									isError={part.isError}
								/>
							</Box>
						);
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
	);
}

// ── Tool call with proper primitive ──

function ToolCallItem({
	toolCallId,
	toolName,
	args,
	argsText,
	result,
	isError,
}: {
	toolCallId: string;
	toolName: string;
	args?: Record<string, unknown>;
	argsText?: string;
	result?: unknown;
	isError?: boolean;
}) {
	const isDone = result !== undefined;
	const isRunning = !isDone && !isError;

	return (
		<Box>
			<Text color={isError ? colors.error : isDone ? colors.success : colors.running}>
				{BLACK_CIRCLE}
			</Text>
			<Text> </Text>
			<Text bold color={isRunning ? colors.running : undefined}>{toolName}</Text>
			{!isDone && args && <ArgsSummary args={args} />}
			{isError && (
				<Text color={colors.error}> {symbols.cross} failed</Text>
			)}
		</Box>
	);
}

// ── Compact args display ──

function ArgsSummary({ args }: { args: Record<string, unknown> }) {
	const entries = Object.entries(args);
	if (entries.length === 0) return null;

	const summary = entries.length <= 2
		? entries
				.map(([k, v]) => {
					const val = typeof v === "string" ? v.slice(0, 40) : JSON.stringify(v)?.slice(0, 40);
					return `${k}: ${val}`;
				})
				.join(", ")
		: `${entries.length} args`;

	if (summary.length > 80) {
		return <Text color="gray">({summary.slice(0, 77)}...)</Text>;
	}
	return <Text color="gray">({summary})</Text>;
}

