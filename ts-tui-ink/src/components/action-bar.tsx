/**
 * ActionBar — per-message actions (copy, reload).
 *
 * Uses ActionBarPrimitive from assistant-ui.
 * Shows compact inline actions after each message.
 */

import React from "react";
import { Box, Text } from "ink";
import { ActionBarPrimitive, useAuiState } from "@assistant-ui/react-ink";
import { colors, symbols } from "../theme.js";

export function ActionBar() {
	const role = useAuiState((s) => s.message.role);

	// Only show actions on assistant messages
	if (role !== "assistant") return null;

	return (
		<Box gap={1} paddingLeft={2} marginTop={0}>
			<ActionBarPrimitive.Copy>
				<Text color="gray">{symbols.dot} copy</Text>
			</ActionBarPrimitive.Copy>
			<ActionBarPrimitive.Reload>
				<Text color="gray">{symbols.dot} retry</Text>
			</ActionBarPrimitive.Reload>
		</Box>
	);
}
