/**
 * QuestionDialog — HITL question overlay for the unified decision protocol.
 *
 * Style: yellow question header with numbered options and text input.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";

export function QuestionDialog() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const [input, setInput] = useState("");

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
		<Box flexDirection="column" paddingY={1} paddingX={1}>
			<Box backgroundColor="yellow" paddingX={1}>
				<Text bold>? Question</Text>
			</Box>
			<Box marginTop={1}>
				<Text>{pending.title}</Text>
			</Box>
			{options.length > 0 && (
				<Box flexDirection="column" marginTop={1}>
					{options.map((opt, i) => (
						<Text key={i}>
							<Text backgroundColor="magenta" bold>{` ${i + 1} `}</Text>
							<Text> {opt}</Text>
						</Text>
					))}
				</Box>
			)}
			<Box marginTop={1}>
				<Text color="magenta" bold>{">"} </Text>
				<Text>{input}</Text>
				<Text dimColor>{"\u2588"}</Text>
			</Box>
			<Box>
				<Text dimColor color="gray">
					{pending.allowFreeText
						? "Type answer or number, Enter to submit"
						: "Type option number, Enter to select"}
				</Text>
			</Box>
		</Box>
	);
}
