/**
 * AssistantMessage — renders assistant messages using assistant-ui primitives.
 *
 * Integrates: ChainOfThoughtPrimitive, BranchPickerPrimitive,
 * ToolCallPrimitive.ToolFallback, DiffView, ErrorPrimitive.
 * Custom tool UIs (bash, delegate, file ops) are registered in app.tsx
 * via makeAssistantToolUI and auto-render by tool name.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	useAuiState,
} from "@assistant-ui/react-ink";
import { ReasoningBlock } from "./reasoning.js";
import { ReasoningAccordion } from "./reasoning-accordion.js";
import { BranchPicker } from "./branch-picker.js";
import { colors, BLACK_CIRCLE, symbols } from "../theme.js";

export function AssistantMessage() {
	const parts = useAuiState((s) => s.message.parts);

	if (!parts || parts.length === 0) return null;

	return (
		<Box flexDirection="column" marginTop={1} paddingX={1}>
			{/* Reasoning accordion (uses ChainOfThoughtPrimitive) */}
			<ReasoningAccordion />

			{/* Parts: tool calls, text, reasoning (fallback), errors */}
			{parts.map((part: any, i: number) => {
				switch (part.type) {
					case "tool-call":
						// Custom tool UIs (bash, delegate, etc.) are auto-rendered
						// by makeAssistantToolUI. This fallback handles unknown tools.
						return (
							<Box key={part.toolCallId || i} flexDirection="column">
								<ToolFallback
									toolName={part.toolName}
									args={part.args}
									argsText={part.argsText}
									result={part.result}
									isError={part.isError}
								/>
							</Box>
						);
					case "reasoning":
						// Fallback: plain reasoning block (ReasoningAccordion handles
						// the ChainOfThoughtPrimitive version above)
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

			{/* Branch picker for forked messages */}
			<BranchPicker />
		</Box>
	);
}

// ── Tool call fallback (for tools without custom UI) ──

function ToolFallback({
	toolName,
	args,
	argsText,
	result,
	isError,
}: {
	toolName: string;
	args?: unknown;
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
			{!isDone && args ? <ArgsSummary args={args as Record<string, unknown>} /> : null}
			{isError ? (
				<Text color={colors.error}> {symbols.cross} failed</Text>
			) : null}
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
