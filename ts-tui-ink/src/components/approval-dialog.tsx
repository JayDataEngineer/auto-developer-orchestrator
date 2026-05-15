/**
 * ApprovalDialog — HITL approval overlay for the unified decision protocol.
 *
 * Style: bold warning bar with Y/N prompt.
 */

import React from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";

export function ApprovalDialog() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);

	useInput(async (input) => {
		if (!pending) return;
		if (input === "y" || input === "Y") {
			await respond("approve", "");
		} else if (input === "n" || input === "N") {
			await respond("reject", "");
		}
	});

	if (!pending) return null;

	return (
		<Box flexDirection="column" paddingY={1} paddingX={1}>
			<Box backgroundColor="yellow" paddingX={1}>
				<Text bold>! Approval Required</Text>
			</Box>
			{pending.title && (
				<Box marginTop={1}>
					<Text bold>{pending.title}</Text>
				</Box>
			)}
			{pending.description && (
				<Text color="gray">{pending.description}</Text>
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
