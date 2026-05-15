/**
 * QuestionDialog — HITL question overlay.
 *
 * Style: yellow question header with numbered options and text input.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { symbols } from "../theme.js";

export function QuestionDialog() {
	const pendingQuestion = usePuxStore((s) => s.pendingQuestion);
	const respondToQuestion = usePuxStore((s) => s.respondToQuestion);
	const [input, setInput] = useState("");

	useInput((ch, key) => {
		if (key.backspace || key.delete) {
			setInput((prev) => prev.slice(0, -1));
		} else if (key.return) {
			const num = parseInt(input, 10);
			if (
				pendingQuestion &&
				pendingQuestion.options.length > 0 &&
				!isNaN(num) &&
				num >= 1 &&
				num <= pendingQuestion.options.length
			) {
				respondToQuestion(pendingQuestion.options[num - 1]);
			} else if (input.trim()) {
				respondToQuestion(input.trim());
			}
		} else if (ch && !key.ctrl && !key.meta) {
			setInput((prev) => prev + ch);
		}
	});

	if (!pendingQuestion) return null;

	return (
		<Box flexDirection="column" paddingY={1} paddingX={1}>
			<Box backgroundColor="yellow" paddingX={1}>
				<Text bold>? Question</Text>
			</Box>
			<Box marginTop={1}>
				<Text>{pendingQuestion.question}</Text>
			</Box>
			{pendingQuestion.options.length > 0 && (
				<Box flexDirection="column" marginTop={1}>
					{pendingQuestion.options.map((opt, i) => (
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
					{pendingQuestion.allowFreeText
						? "Type answer or number, Enter to submit"
						: "Type option number, Enter to select"}
				</Text>
			</Box>
		</Box>
	);
}
