/**
 * QuestionDialog — HITL question overlay.
 *
 * Arrow key navigation through options. Enter to select.
 * Arrow down past last option to reach free-text input.
 * Clean, minimal design matching the TUI theme.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors } from "../theme.js";

export function QuestionDialog() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const colors = useColors();

	const options = pending?.options || [];
	// selectedIndex: 0..options.length-1 = options, options.length = free text input
	const [selectedIndex, setSelectedIndex] = useState(0);
	const [textInput, setTextInput] = useState("");
	const [dirty, setDirty] = useState(false);

	useInput((ch, key) => {
		if (!pending) return;

		const totalItems = options.length + (pending.allowFreeText !== false ? 1 : 0);

		if (key.upArrow) {
			setSelectedIndex((prev) => (prev - 1 + totalItems) % totalItems);
			setDirty(false);
			return;
		}
		if (key.downArrow) {
			setSelectedIndex((prev) => (prev + 1) % totalItems);
			setDirty(false);
			return;
		}

		const isFreeTextRow = selectedIndex === options.length;

		if (key.return) {
			if (isFreeTextRow) {
				if (textInput.trim()) {
					respond("answer", textInput.trim());
				}
			} else if (options.length > 0) {
				respond("answer", options[selectedIndex]);
			}
			return;
		}

		// Free-text input handling
		if (isFreeTextRow) {
			if (key.backspace || key.delete) {
				setTextInput((prev) => prev.slice(0, -1));
			} else if (ch && !key.ctrl && !key.meta) {
				setTextInput((prev) => prev + ch);
				setDirty(true);
			}
		}
	});

	if (!pending) return null;

	const showFreeText = pending.allowFreeText !== false;
	const isFreeTextRow = selectedIndex === options.length;

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
						const isActive = i === selectedIndex;
						return (
							<Box key={i}>
								<Text>{isActive ? "  › " : "    "}</Text>
								<Text color={isActive ? colors.brand : undefined} bold={isActive}>
									{opt}
								</Text>
							</Box>
						);
					})}
				</Box>
			)}

			{/* Free-text input */}
			{showFreeText && (
				<Box marginTop={options.length > 0 ? 0 : 1}>
					<Text>{isFreeTextRow ? "  › " : "    "}</Text>
					<Text color={colors.brand} bold>{">"} </Text>
					<Text>{isFreeTextRow ? textInput : ""}</Text>
					{isFreeTextRow && <Text color={colors.textMuted}>{"\u2588"}</Text>}
				</Box>
			)}

			{/* Help */}
			<Text color={colors.textMuted}>
				Up/Down select · Enter confirm
			</Text>
		</Box>
	);
}
