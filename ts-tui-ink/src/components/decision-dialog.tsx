/**
 * DecisionDialog — unified HITL decision overlay.
 *
 * Handles approval and plan_review hints from the unified PendingDecision.
 * Clean theme-colored design matching the question dialog style.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors, symbols } from "../theme.js";

export function DecisionDialog() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const [feedback, setFeedback] = useState("");
	const [feedbackMode, setFeedbackMode] = useState(false);
	const colors = useColors();

	// Detect if this is a tool permission request
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
				if (ch === "y" || ch === "Y") {
					respond("approve", "");
				} else if (ch === "a" || ch === "A") {
					respond("allow_session", "");
				} else if (ch === "n" || ch === "N") {
					respond("reject", "");
				}
			} else {
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
		<Box flexDirection="column" paddingY={1} paddingX={2}>
			{/* Header */}
			<Box>
				<Text color={isApproval ? colors.warning : colors.brand} bold>
					{isApproval ? `${symbols.cross} ` : "? "}
				</Text>
				<Text bold>
					{isApproval
						? isToolPerm ? `Tool Permission: ${toolName}` : "Approval Required"
						: "Plan Review"}
				</Text>
			</Box>

			{/* Title */}
			{pending.title && (
				<Box marginTop={1}>
					<Text bold>{pending.title}</Text>
				</Box>
			)}

			{/* Description */}
			{pending.description && (
				<Box marginTop={1} flexDirection="column">
					{pending.description.split("\n").map((line, i) => (
						<Text key={i} color={colors.textMuted}>{line}</Text>
					))}
				</Box>
			)}

			{/* Actions */}
			{feedbackMode ? (
				<Box flexDirection="column" marginTop={1}>
					<Text bold>Feedback:</Text>
					<Box>
						<Text color={colors.brand} bold>{">"} </Text>
						<Text>{feedback}</Text>
						<Text color={colors.textMuted}>{"\u2588"}</Text>
					</Box>
					<Text color={colors.textMuted}>Enter submit · Esc cancel</Text>
				</Box>
			) : isApproval && isToolPerm ? (
				<Box marginTop={1} flexDirection="column">
					<Text>
						<Text color={colors.success} bold>Y</Text>
						<Text color={colors.textMuted}> Allow once   </Text>
						<Text color={colors.brand} bold>A</Text>
						<Text color={colors.textMuted}> Always (session)   </Text>
						<Text color={colors.error} bold>N</Text>
						<Text color={colors.textMuted}> Reject</Text>
					</Text>
				</Box>
			) : isApproval ? (
				<Box marginTop={1}>
					<Text color={colors.success} bold>Y</Text>
					<Text color={colors.textMuted}> Approve   </Text>
					<Text color={colors.error} bold>N</Text>
					<Text color={colors.textMuted}> Reject</Text>
				</Box>
			) : (
				<Box marginTop={1}>
					<Text color={colors.success} bold>A</Text>
					<Text color={colors.textMuted}> Accept   </Text>
					<Text color={colors.error} bold>R</Text>
					<Text color={colors.textMuted}> Reject   </Text>
					<Text color={colors.brand} bold>F</Text>
					<Text color={colors.textMuted}> Feedback</Text>
				</Box>
			)}
		</Box>
	);
}
