/**
 * BranchPicker — navigate message forks/branches.
 *
 * Uses BranchPickerPrimitive from assistant-ui.
 * Shows ◀ N/M ▶ when a message has multiple branches.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	BranchPickerPrimitive,
	useAuiState,
} from "@assistant-ui/react-ink";
import { useColors, symbols } from "../theme.js";

export function BranchPicker() {
	const branchCount = useAuiState((s) => s.message.branchCount);
	const branchNumber = useAuiState((s) => s.message.branchNumber);
	const colors = useColors();

	// Only render if there are multiple branches
	if (!branchCount || branchCount <= 1) return null;

	return (
		<Box gap={0} paddingLeft={2}>
			<BranchPickerPrimitive.Previous>
				<Text color="gray">{"<"}</Text>
			</BranchPickerPrimitive.Previous>
			<Text color="gray"> </Text>
			<BranchPickerPrimitive.Number
				color={colors.brand}
				bold
			/>
			<Text color="gray">/</Text>
			<BranchPickerPrimitive.Count color="gray" />
			<Text color="gray"> </Text>
			<BranchPickerPrimitive.Next>
				<Text color="gray">{">"}</Text>
			</BranchPickerPrimitive.Next>
		</Box>
	);
}
