/**
 * ApprovalDialog — HITL approval overlay.
 *
 * Style: bold warning bar with Y/N prompt.
 */

import React from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { symbols } from "../theme.js";

export function ApprovalDialog() {
	const pendingApproval = usePuxStore((s) => s.pendingApproval);
	const respondToApproval = usePuxStore((s) => s.respondToApproval);

	useInput(async (input) => {
		if (input === "y" || input === "Y") {
			await respondToApproval(true);
		} else if (input === "n" || input === "N") {
			await respondToApproval(false);
		}
	});

	if (!pendingApproval) return null;

	return (
		<Box flexDirection="column" paddingY={1} paddingX={1}>
			<Box backgroundColor="yellow" paddingX={1}>
				<Text bold>{symbols.cross} Approval Required</Text>
			</Box>
			{pendingApproval.title && (
				<Box marginTop={1}>
					<Text bold>{pendingApproval.title}</Text>
				</Box>
			)}
			{pendingApproval.description && (
				<Text color="gray">{pendingApproval.description}</Text>
			)}
			<Box marginTop={1}>
				<Text backgroundColor="green" bold>{" Y "}</Text>
				<Text> Approve  </Text>
				<Text backgroundColor="red" bold>{" N "}</Text>
				<Text> Reject</Text>
			</Box>
		</Box>
	);
}
