/**
 * ActionBar — per-message actions (copy, reload, edit, feedback).
 *
 * Uses ActionBarPrimitive from assistant-ui.
 * Shows compact inline actions after each message.
 * - Assistant: copy, retry, thumbs up/down
 * - User: edit (to re-edit and resend)
 */

import React from "react";
import { Box, Text } from "ink";
import { ActionBarPrimitive, useAuiState } from "@assistant-ui/react-ink";
import { useColors, symbols } from "../theme.js";

export function ActionBar() {
	const role = useAuiState((s) => s.message.role);
	const colors = useColors();

	if (role === "assistant") {
		return (
			<Box gap={1} paddingLeft={2} marginTop={0}>
				<ActionBarPrimitive.Copy>
					<Text color="gray">{symbols.dot} copy</Text>
				</ActionBarPrimitive.Copy>
				<ActionBarPrimitive.Reload>
					<Text color="gray">{symbols.dot} retry</Text>
				</ActionBarPrimitive.Reload>
				<ActionBarPrimitive.FeedbackPositive>
					{({ isSubmitted }) => (
						<Text color={isSubmitted ? colors.success : "gray"}>
							{isSubmitted ? symbols.check : symbols.dot} good
						</Text>
					)}
				</ActionBarPrimitive.FeedbackPositive>
				<ActionBarPrimitive.FeedbackNegative>
					{({ isSubmitted }) => (
						<Text color={isSubmitted ? colors.warning : "gray"}>
							{isSubmitted ? symbols.cross : symbols.dot} bad
						</Text>
					)}
				</ActionBarPrimitive.FeedbackNegative>
			</Box>
		);
	}

	if (role === "user") {
		return (
			<Box gap={1} paddingLeft={2} marginTop={0}>
				<ActionBarPrimitive.Edit>
					<Text color="gray">{symbols.dot} edit</Text>
				</ActionBarPrimitive.Edit>
				<ActionBarPrimitive.Copy>
					<Text color="gray">{symbols.dot} copy</Text>
				</ActionBarPrimitive.Copy>
			</Box>
		);
	}

	return null;
}
