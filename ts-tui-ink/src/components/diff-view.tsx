/**
 * DiffView — renders file diffs with syntax coloring.
 *
 * Uses DiffView from @assistant-ui/react-ink which provides
 * built-in diff parsing, line numbers, fold support, and stats.
 * Shows additions in green, deletions in red.
 */

import React from "react";
import { Box, Text } from "ink";
import { DiffView } from "@assistant-ui/react-ink";

interface DiffViewDisplayProps {
	patch?: string;
	oldFile?: { content: string; name?: string };
	newFile?: { content: string; name?: string };
}

export function DiffViewDisplay({ patch, oldFile, newFile }: DiffViewDisplayProps) {
	return (
		<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
			<DiffView
				patch={patch}
				oldFile={oldFile}
				newFile={newFile}
				showLineNumbers={true}
				contextLines={3}
				maxLines={50}
			/>
		</Box>
	);
}
