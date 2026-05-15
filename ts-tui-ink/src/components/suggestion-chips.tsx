/**
 * SuggestionChips — follow-up suggestion buttons.
 *
 * Uses ThreadPrimitive.Suggestion from assistant-ui to render
 * clickable suggestions after messages or on empty thread.
 * Requires the adapter to provide suggestions via the runtime.
 */

import React from "react";
import { Box, Text } from "ink";
import { ThreadPrimitive, useAuiState } from "@assistant-ui/react-ink";
import { colors, symbols } from "../theme.js";

export function SuggestionChips() {
	const isEmpty = useAuiState((s) => s.thread.isEmpty);

	return (
		<Box flexDirection="column" paddingLeft={2} marginTop={1}>
			{/* Thread-level suggestions (empty state or after messages) */}
			<ThreadPrimitive.If empty={true}>
				<Text dimColor color="gray">
					{BLOCKQUOTE_BAR} Try:
				</Text>
			</ThreadPrimitive.If>

			{/* Suggestion items - these render if the adapter provides them */}
			<SuggestionList />
		</Box>
	);
}

function SuggestionList() {
	// This component renders suggestions from the runtime.
	// The adapter can populate suggestions via the runtime config.
	// For now, we show a static set on empty thread.
	const isEmpty = useAuiState((s) => s.thread.isEmpty);
	const isRunning = useAuiState((s) => s.thread.isRunning);

	if (!isEmpty || isRunning) return null;

	return (
		<Box flexDirection="column" paddingLeft={1}>
			{defaultSuggestions.map((s, i) => (
				<ThreadPrimitive.Suggestion
					key={i}
					prompt={s.prompt}
					send={true}
				>
					<Text color={colors.brand}>
						{symbols.arrow} {s.label}
					</Text>
				</ThreadPrimitive.Suggestion>
			))}
		</Box>
	);
}

const BLOCKQUOTE_BAR = "▎";

const defaultSuggestions = [
	{ label: "What can you do?", prompt: "What can you do? What tools and agents do you have available?" },
	{ label: "Show me the project structure", prompt: "Show me the project structure and explain the architecture" },
	{ label: "Run the tests", prompt: "Run the tests and show me the results" },
];
