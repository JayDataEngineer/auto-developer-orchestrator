/**
 * DecisionDialog — unified HITL decision overlay.
 *
 * Handles approval and plan_review hints from the unified PendingDecision.
 * Approval: Y/N prompt.
 * Plan review: Accept/Revise/Reject with optional feedback input.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { colors, symbols } from "../theme.js";

export function DecisionDialog() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const [feedback, setFeedback] = useState("");
	const [feedbackMode, setFeedbackMode] = useState(false);

	// Detect if this is a tool permission request: sourceTool is set and
	// the title follows the "Allow %q?" pattern or metadata has toolName.
	const isToolPerm = pending?.metadata &&
		(typeof pending.metadata.toolName === "string" ||
		 (pending.sourceTool && pending.sourceTool !== "ask_user" && pending.sourceTool !== "create_plan"));

	useInput((ch, key) => {
		if (!pending) return;

		if (feedbackMode) {
			if (key.backspace || key.delete) {
				setFeedback((prev) => prev.slice(0, -1));
			} else if (key.return) {
				respond("refine", feedback.trim() || "Please revise");
			} else if (key.escape) {
				setFeedbackMode(false);
			} else if (ch && !key.ctrl && !key.meta) {
				setFeedback((prev) => prev + ch);
			}
			return;
		}

		if (pending.hint === "approval") {
			if (isToolPerm) {
				// Tool permission dialog: Y = once, A = always, N = reject
				if (ch === "y" || ch === "Y") {
					respond("approve", "");
				} else if (ch === "a" || ch === "A") {
					respond("allow_session", "");
				} else if (ch === "n" || ch === "N") {
					respond("reject", "");
				}
			} else {
				// Standard approval
				if (ch === "y" || ch === "Y") {
					respond("approve", "");
				} else if (ch === "n" || ch === "N") {
					respond("reject", "");
				}
			}
		} else if (pending.hint === "plan_review") {
			if (ch === "a" || ch === "A") {
				respond("approve", "");
			} else if (ch === "r" || ch === "R") {
				respond("cancel", "");
			} else if (ch === "f" || ch === "F") {
				setFeedbackMode(true);
			}
		}
	});

	if (!pending) return null;

	const isApproval = pending.hint === "approval";
	const toolName = isToolPerm
		? (pending.metadata?.toolName as string) || pending.sourceTool
		: null;

	return (
		<Box flexDirection="column" paddingY={1} paddingX={1}>
			{isApproval ? (
				<Box backgroundColor="yellow" paddingX={1}>
					<Text bold>
						{symbols.cross}
						{isToolPerm ? ` Tool Permission: ${toolName}` : " Approval Required"}
					</Text>
				</Box>
			) : (
				<Box backgroundColor="magenta" paddingX={1}>
					<Text bold>? Plan Review</Text>
				</Box>
			)}

			{pending.title && (
				<Box marginTop={1}>
					<Text bold>{pending.title}</Text>
				</Box>
			)}

			{/* Tool permission description — show args in monospace style */}
			{pending.description && (
				<Box marginTop={1} flexDirection="column">
					{isToolPerm ? (
						pending.description.split("\n").map((line, i) => (
							<Text key={i} color="gray">{line}</Text>
						))
					) : (
						<Text color="gray">{pending.description}</Text>
					)}
				</Box>
			)}

			{feedbackMode ? (
				<Box flexDirection="column" marginTop={1}>
					<Text bold>Feedback:</Text>
					<Box>
						<Text color={colors.brand} bold>{">"} </Text>
						<Text>{feedback}</Text>
						<Text dimColor>{"\u2588"}</Text>
					</Box>
					<Text dimColor color="gray">Enter submit · Esc cancel</Text>
				</Box>
			) : isApproval && isToolPerm ? (
				<Box marginTop={1} flexDirection="column">
					<Box>
						<Text backgroundColor="green" bold>{" Y "}</Text>
						<Text> Allow once  </Text>
						<Text backgroundColor="cyan" bold>{" A "}</Text>
						<Text> Always allow (session)  </Text>
					</Box>
					<Box marginTop={1}>
						<Text backgroundColor="red" bold>{" N "}</Text>
						<Text> Reject</Text>
					</Box>
				</Box>
			) : isApproval ? (
				<Box marginTop={1}>
					<Text backgroundColor="green" bold>{" Y "}</Text>
					<Text> Approve  </Text>
					<Text backgroundColor="red" bold>{" N "}</Text>
					<Text> Reject</Text>
				</Box>
			) : (
				<Box marginTop={1}>
					<Text backgroundColor="green" bold>{" A "}</Text>
					<Text> Accept  </Text>
					<Text backgroundColor="red" bold>{" R "}</Text>
					<Text> Reject  </Text>
					<Text backgroundColor="magenta" bold>{" F "}</Text>
					<Text> Feedback</Text>
				</Box>
			)}
		</Box>
	);
}
