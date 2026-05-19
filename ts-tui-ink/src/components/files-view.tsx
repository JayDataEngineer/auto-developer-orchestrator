/**
 * FilesView — shows all files read/written in the session.
 *
 * Extracts file operations from tool calls in the runtime state.
 * Groups by file path, shows status (read/write/edit), and offers
 * expandable result previews.
 */

import React, { useState, useMemo } from "react";
import { Box, Text, useInput } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

interface FileEntry {
	path: string;
	operations: Array<{
		toolName: string;
		type: "read" | "write" | "edit";
		result?: unknown;
		isError?: boolean;
		timestamp: number;
	}>;
}

export function FilesView() {
	const messages = useAuiState((s) => s.thread.messages);
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [expanded, setExpanded] = useState<Set<string>>(new Set());
	const colors = useColors();
	const { rows } = useTerminalSize();

	// Extract file operations from tool calls
	const fileEntries = useMemo(() => {
		const fileMap = new Map<string, FileEntry>();

		messages.forEach((msg: any) => {
			if (msg.role !== "assistant") return;
			const parts = msg.parts || [];
			for (const part of parts) {
				if (part.type !== "tool-call") continue;

				const toolName = part.toolName as string;
				const args = part.args as Record<string, unknown> | undefined;
				const filePath = args?.path as string || args?.file_path as string || "";
				if (!filePath) continue;

				// Only include file-related tools
				if (!["file_read", "file_write", "file_edit", "read_file", "write_file", "edit_file"].includes(toolName)) continue;

				const entry = fileMap.get(filePath) || { path: filePath, operations: [] };
				const opType = toolName.includes("write") || toolName.includes("edit") ? "write" : "read";
				entry.operations.push({
					toolName,
					type: opType as "read" | "write" | "edit",
					result: part.result,
					isError: part.isError,
					timestamp: Date.now(),
				});
				fileMap.set(filePath, entry);
			}
		});

		return [...fileMap.values()].sort((a, b) => {
			// Write operations first, then by path
			const aHasWrite = a.operations.some((op) => op.type !== "read");
			const bHasWrite = b.operations.some((op) => op.type !== "read");
			if (aHasWrite && !bHasWrite) return -1;
			if (!aHasWrite && bHasWrite) return 1;
			return a.path.localeCompare(b.path);
		});
	}, [messages]);

	// Keyboard navigation
	useInput((_input: string, key: any) => {
		if (fileEntries.length === 0) return;
		if (key.upArrow) {
			setSelectedIdx(Math.max(0, selectedIdx - 1));
			return;
		}
		if (key.downArrow) {
			setSelectedIdx(Math.min(fileEntries.length - 1, selectedIdx + 1));
			return;
		}
		if (key.return || _input === " ") {
			const entry = fileEntries[selectedIdx];
			if (entry) {
				setExpanded((prev) => {
					const next = new Set(prev);
					if (next.has(entry.path)) next.delete(entry.path);
					else next.add(entry.path);
					return next;
				});
			}
		}
	});

	const reads = fileEntries.filter((f) => f.operations.every((op) => op.type === "read")).length;
	const writes = fileEntries.filter((f) => f.operations.some((op) => op.type !== "read")).length;

	if (fileEntries.length === 0) {
		return (
			<Box flexDirection="column" paddingX={2} paddingY={1}>
				<Text bold color={colors.brand}>Files</Text>
				<Box marginTop={1}>
					<Text dimColor>No file operations yet. Files appear when the agent reads or writes them.</Text>
				</Box>
			</Box>
		);
	}

	const maxVisible = rows - 5;

	return (
		<Box flexDirection="column" paddingX={1}>
			{/* Header */}
			<Box marginBottom={1}>
				<Text bold color={colors.brand}>Files</Text>
				<Text color="gray"> {symbols.dot} </Text>
				<Text>{fileEntries.length} files </Text>
				<Text color={colors.success}>{writes} modified </Text>
				<Text color="gray">{reads} read</Text>
			</Box>

			{/* File list */}
			{fileEntries.slice(0, maxVisible).map((entry, i) => {
				const isSelected = i === selectedIdx;
				const isExpandedEntry = expanded.has(entry.path);
				const hasWrite = entry.operations.some((op) => op.type !== "read");
				const hasError = entry.operations.some((op) => op.isError);
				const lastOp = entry.operations[entry.operations.length - 1];

				// Status icon
				const statusIcon = hasError
					? symbols.toolError
					: hasWrite
						? symbols.toolDone
						: symbols.dot;
				const statusColor = hasError
					? colors.error
					: hasWrite
						? colors.success
						: "gray";

				// Shorten path for display
				const shortPath = entry.path.length > 70
					? "..." + entry.path.slice(-67)
					: entry.path;

				return (
					<Box key={entry.path} flexDirection="column" marginBottom={0}>
						<Box>
							<Text color={statusColor}>{statusIcon} </Text>
							<Text color={isSelected ? colors.brand : undefined}>
								{shortPath}
							</Text>
							<Text color="gray">
								{" "}({entry.operations.length} ops)
							</Text>
						</Box>

						{isExpandedEntry && (
							<Box flexDirection="column" paddingLeft={3}>
								{entry.operations.map((op, j) => (
									<Box key={j}>
										<Text color={op.isError ? colors.error : op.type === "read" ? "gray" : colors.success}>
											{op.type === "read" ? "R" : "W"}{" "}
										</Text>
										<Text dimColor>{op.toolName}</Text>
										{op.result !== undefined && (
											<Text color="gray">
												{" "}
												{typeof op.result === "string"
													? (op.result as string).split("\n")[0]?.slice(0, 50) || ""
													: "(result)"}
											</Text>
										)}
									</Box>
								))}
							</Box>
						)}
					</Box>
				);
			})}

			{fileEntries.length > maxVisible && (
				<Text dimColor color="gray">
					... +{fileEntries.length - maxVisible} more
				</Text>
			)}

			<Box marginTop={1}>
				<Text dimColor>
					<Text bold>Up/Down</Text> navigate <Text bold>Enter</Text> expand <Text bold>Ctrl+T</Text> back to chat
				</Text>
			</Box>
		</Box>
	);
}
