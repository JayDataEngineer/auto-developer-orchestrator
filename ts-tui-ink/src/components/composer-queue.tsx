/**
 * ComposerQueue — displays queued messages waiting to be processed.
 *
 * Shows pending prompts above the composer with remove and steer actions.
 * Uses assistant-ui's composer queue state.
 */

import React from "react";
import { Box, Text } from "ink";
import { useAuiState, useAui } from "@assistant-ui/react-ink";
import { useColors, symbols } from "../theme.js";

export function ComposerQueue() {
	// The queue is available in the composer state when using
	// useRemoteThreadListRuntime or when messages are queued via steer.
	// For now, we show a placeholder that reads from the runtime state.
	const queue = useAuiState((s) => s.composer.queue);
	const colors = useColors();

	if (!queue || queue.length === 0) return null;

	return (
		<Box flexDirection="column" paddingX={1} marginBottom={0}>
			{queue.map((item: any, i: number) => (
				<Box key={item.id || i}>
					<Text color={colors.warning}>
						{symbols.dot} queued:{" "}
					</Text>
					<Text dimColor>
						{(item.prompt || "").slice(0, 60)}
					</Text>
				</Box>
			))}
		</Box>
	);
}
