/**
 * ToolFallback — compact tool call rendering.
 *
 * Style: BLINKING ● when running, solid ● when done, ✕ on error.
 * Tool name in bold. Brief inline args summary.
 * Matches Claude Code's tool use indicator pattern.
 */

import React from "react";
import { Box, Text } from "ink";
import { useColors, symbols, BLACK_CIRCLE } from "../theme.js";

interface ToolFallbackProps {
	toolName: string;
	args?: Record<string, unknown>;
	argsText?: string;
	result?: unknown;
	isError?: boolean;
}

/** Format tool args into a compact inline summary */
function formatArgs(args?: Record<string, unknown>): string {
	if (!args) return "";
	const entries = Object.entries(args);
	if (entries.length === 0) return "";
	if (entries.length <= 2) {
		const summary = entries
			.map(([k, v]) => {
				const val = typeof v === "string" ? v.slice(0, 40) : JSON.stringify(v)?.slice(0, 40);
				return `${k}: ${val}`;
			})
			.join(", ");
		return summary.length > 80 ? summary.slice(0, 77) + "..." : summary;
	}
	return `${entries.length} args`;
}

export function ToolFallback({ toolName, args, result, isError }: ToolFallbackProps) {
	const colors = useColors();
	const isDone = result !== undefined;
	const isRunning = !isDone && !isError;

	// Color: cyan for running, green for done, red for error
	const statusColor = isError ? colors.error : isDone ? colors.success : colors.running;
	const nameColor = isRunning ? colors.running : undefined;

	const argsSummary = formatArgs(args);

	return (
		<Box>
			<Text color={statusColor}>{BLACK_CIRCLE} </Text>
			<Text bold color={nameColor}>{toolName}</Text>
			{argsSummary && !isDone && (
				<Text color="gray">({argsSummary})</Text>
			)}
			{isError && (
				<Text color={colors.error}> {symbols.cross} failed</Text>
			)}
		</Box>
	);
}
