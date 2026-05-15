/**
 * ToolsView — aggregated view of recent tool calls across all messages.
 *
 * Reads tool calls from the assistant-ui runtime state and displays
 * them in a scrollable list with status, args, and result previews.
 */

import React, { useState, useMemo } from "react";
import { Box, Text, useInput, useStdout } from "ink";
import { useAuiState } from "@assistant-ui/react-ink";
import { colors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

interface ToolCallEntry {
	toolCallId: string;
	toolName: string;
	args?: unknown;
	result?: unknown;
	isError?: boolean;
	status: "running" | "complete" | "error";
	messageIndex: number;
}

export function ToolsView() {
	const messages = useAuiState((s) => s.thread.messages);
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [expanded, setExpanded] = useState<Set<string>>(new Set());
	const { stdout } = useStdout();
	const rows = stdout?.rows ?? 24;

	// Extract all tool calls from messages
	const toolCalls = useMemo(() => {
		const entries: ToolCallEntry[] = [];
		messages.forEach((msg: any, msgIdx: number) => {
			if (msg.role !== "assistant") return;
			const parts = msg.parts || [];
			for (const part of parts) {
				if (part.type === "tool-call") {
					entries.push({
						toolCallId: part.toolCallId,
						toolName: part.toolName,
						args: part.args,
						result: part.result,
						isError: part.isError,
						status: part.result !== undefined
							? part.isError ? "error" : "complete"
							: msg.status?.type === "running" ? "running" : "complete",
						messageIndex: msgIdx,
					});
				}
			}
		});
		return entries;
	}, [messages]);

	// Keyboard navigation
	useInput((_input: string, key: any) => {
		if (toolCalls.length === 0) return;
		if (key.upArrow) {
			setSelectedIdx(Math.max(0, selectedIdx - 1));
			return;
		}
		if (key.downArrow) {
			setSelectedIdx(Math.min(toolCalls.length - 1, selectedIdx + 1));
			return;
		}
		if (key.return || _input === " ") {
			const tc = toolCalls[selectedIdx];
			if (tc) {
				setExpanded((prev) => {
					const next = new Set(prev);
					if (next.has(tc.toolCallId)) next.delete(tc.toolCallId);
					else next.add(tc.toolCallId);
					return next;
				});
			}
		}
	});

	const running = toolCalls.filter((t) => t.status === "running").length;
	const completed = toolCalls.filter((t) => t.status === "complete").length;
	const failed = toolCalls.filter((t) => t.status === "error").length;

	if (toolCalls.length === 0) {
		return (
			<Box flexDirection="column" paddingX={2} paddingY={1}>
				<Text bold color={colors.brand}>Tools</Text>
				<Box marginTop={1}>
					<Text dimColor>No tool calls yet. Tools appear when the agent uses them.</Text>
				</Box>
			</Box>
		);
	}

	// Show most recent first, limit to viewport
	const maxVisible = rows - 6;
	const sorted = [...toolCalls].reverse().slice(0, maxVisible);

	return (
		<Box flexDirection="column" paddingX={1}>
			{/* Header */}
			<Box marginBottom={1}>
				<Text bold color={colors.brand}>Tools</Text>
				<Text color="gray"> {symbols.dot} </Text>
				{running > 0 && <Text color={colors.running}>{running} running </Text>}
				<Text color={colors.success}>{completed} done </Text>
				{failed > 0 && <Text color={colors.error}>{failed} failed </Text>}
			</Box>

			{/* Tool call list */}
			{sorted.map((tc, i) => {
				const isExpandedEntry = expanded.has(tc.toolCallId);
				const isSelected = i === selectedIdx;
				const statusIcon = tc.status === "running"
					? symbols.toolRunning
					: tc.status === "error"
						? symbols.toolError
						: symbols.toolDone;
				const statusColor = tc.status === "running"
					? colors.running
					: tc.status === "error"
						? colors.error
						: colors.success;

				return (
					<Box key={tc.toolCallId} flexDirection="column" marginBottom={0}>
						<Box>
							<Text color={statusColor}>{statusIcon} </Text>
							<Text bold color={isSelected ? colors.brand : undefined}>
								{tc.toolName}
							</Text>
							<Text color="gray">
								{" "}
								{getArgsPreview(tc.toolName, tc.args)}
							</Text>
						</Box>

						{isExpandedEntry && (
							<Box flexDirection="column" paddingLeft={3}>
								{/* Args */}
								{tc.args !== undefined && (
									<Box flexDirection="column">
										<Text dimColor bold>Args:</Text>
										{JSON.stringify(tc.args, null, 2).split("\n").slice(0, 5).map((l: string, j: number) => (
											<Text key={j} dimColor>{l}</Text>
										))}
									</Box>
								)}

								{/* Result */}
								{tc.result !== undefined && (
									<Box flexDirection="column" marginTop={0}>
										<Text dimColor bold>Result:</Text>
										{formatResult(tc.result).map((line: string, j: number) => (
											<Text key={j} dimColor>
												{BLOCKQUOTE_BAR} {line.slice(0, 120)}
											</Text>
										))}
									</Box>
								)}
							</Box>
						)}
					</Box>
				);
			})}

			{toolCalls.length > maxVisible && (
				<Text dimColor color="gray">
					... +{toolCalls.length - maxVisible} more
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

function getArgsPreview(toolName: string, args?: unknown): string {
	if (!args || typeof args !== "object") return "";
	const a = args as Record<string, unknown>;
	const entries = Object.entries(a);
	if (entries.length === 0) return "";

	// Tool-specific previews
	if (["bash", "shell"].includes(toolName)) {
		const cmd = (a.command as string) || (a.cmd as string) || "";
		return cmd.slice(0, 50);
	}
	if (["delegate_to", "delegate_async"].includes(toolName)) {
		return (a.agent as string) || "";
	}
	if (["file_read", "file_write", "file_edit"].includes(toolName)) {
		return (a.path as string) || (a.file_path as string) || "";
	}

	// Generic
	const firstVal = entries[0]?.[1];
	if (typeof firstVal === "string") return firstVal.slice(0, 50);
	return `${entries.length} args`;
}

function formatResult(result: unknown): string[] {
	if (result === undefined || result === null) return [];
	let text: string;
	if (typeof result === "string") {
		text = result;
	} else if (typeof result === "object") {
		const obj = result as Record<string, unknown>;
		text = (obj.output as string) || (obj.text as string) || JSON.stringify(result);
	} else {
		text = JSON.stringify(result);
	}
	text = text.replace(/\r\n/g, "\n");
	const lines = text.split("\n").filter((l) => l.trim());
	return lines.slice(0, 8);
}
