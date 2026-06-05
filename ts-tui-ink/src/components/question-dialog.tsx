/**
 * QuestionDialog — HITL question overlay.
 *
 * Clean, minimal design matching the TUI theme. Uses theme colors
 * instead of hardcoded ANSI colors. Options rendered as a simple
 * numbered list with selection indicators.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors } from "../theme.js";

export function QuestionDialog() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const [input, setInput] = useState("");
	const colors = useColors();

	useInput((ch, key) => {
		if (!pending) return;
		if (key.backspace || key.delete) {
			setInput((prev) => prev.slice(0, -1));
		} else if (key.return) {
			const options = pending.options || [];
			const num = parseInt(input, 10);
			if (
				options.length > 0 &&
				!isNaN(num) &&
				num >= 1 &&
				num <= options.length
			) {
				respond("answer", options[num - 1]);
			} else if (input.trim() && (pending.allowFreeText !== false)) {
				respond("answer", input.trim());
			}
		} else if (ch && !key.ctrl && !key.meta) {
			setInput((prev) => prev + ch);
		}
	});

	if (!pending) return null;

	const options = pending.options || [];

	return (
		<Box flexDirection="column" paddingY={1} paddingX={2}>
			{/* Header */}
			<Box>
				<Text color={colors.warning} bold>? </Text>
				<Text bold>{pending.title}</Text>
			</Box>

			{/* Description */}
			{pending.description && (
				<Box marginTop={1}>
					<Text color={colors.textMuted}>{pending.description}</Text>
				</Box>
			)}

			{/* Options */}
			{options.length > 0 && (
				<Box flexDirection="column" marginTop={1}>
					{options.map((opt, i) => {
						const numStr = String(i + 1);
						const isSelected = input === numStr;
						return (
							<Box key={i}>
								<Text color={colors.textMuted}>  </Text>
								<Text color={isSelected ? colors.brand : colors.textMuted} bold>
									{numStr}.
								</Text>
								<Text> {opt}</Text>
							</Box>
						);
					})}
				</Box>
			)}

			{/* Input */}
			<Box marginTop={1}>
				<Text color={colors.brand} bold>{">"} </Text>
				<Text>{input}</Text>
				<Text color={colors.textMuted}>{"\u2588"}</Text>
			</Box>

			{/* Help */}
			<Text color={colors.textMuted}>
				{pending.allowFreeText
					? "Type answer or number, Enter to submit"
					: "Type option number, Enter to select"}
			</Text>
		</Box>
	);
}
