/**
 * Custom tool UIs — specialized renderers for specific tool types.
 *
 * Uses makeAssistantToolUI from @assistant-ui/react-ink to register
 * custom renderers per tool name. Each tool gets its own visual treatment.
 *
 * Phase 3: DelegateToolUI now shows sub-agent tool calls from Zustand store.
 *
 * Note: status is an object { type: "running" | "complete" | "incomplete" | "requires-action" }
 * not a string. Access status.type for comparison.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	makeAssistantToolUI,
	makeAssistantTool,
	useAuiState,
} from "@assistant-ui/react-ink";
import { usePuxStore, formatToolResult } from "@pux/shared";
import { colors, symbols, BLACK_CIRCLE, BLOCKQUOTE_BAR } from "../theme.js";

// ── Bash execution tool UI ──

export const BashToolUI = makeAssistantToolUI({
	toolName: "bash",
	render: ({ args, result, isError, status }) => {
		const command = (args as any)?.command || (args as any)?.cmd || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";
		const resultLines = formatToolResult(result, 10);

		return (
			<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
				<Box>
					<Text
						color={
							isError
								? colors.error
								: isDone
									? colors.success
									: colors.running
						}
					>
						{BLACK_CIRCLE}{" "}
					</Text>
					<Text bold color={isRunning ? colors.running : undefined}>
						bash
					</Text>
					<Text color="gray"> {command.slice(0, 60)}</Text>
				</Box>
				{resultLines.length > 0 && !isRunning && (
					<Box paddingLeft={2} flexDirection="column">
						{resultLines.map((line: string, i: number) => (
							<Text key={i} dimColor={isError} color={isError ? colors.error : undefined}>
								{line}
							</Text>
						))}
					</Box>
				)}
			</Box>
		);
	},
});

// ── Delegate/sub-agent tool UI ──
// Phase 3: Shows sub-agent tool calls from the Zustand agent store.

export const DelegateToolUI = makeAssistantToolUI({
	toolName: "delegate_to",
	render: ({ args, result, status, toolCallId }) => {
		return (
			<DelegateRenderer
				args={args}
				result={result}
				status={status}
				toolCallId={toolCallId}
			/>
		);
	},
});

export const DelegateAsyncToolUI = makeAssistantToolUI({
	toolName: "delegate_async",
	render: ({ args, result, status, toolCallId }) => {
		return (
			<DelegateRenderer
				args={args}
				result={result}
				status={status}
				toolCallId={toolCallId}
			/>
		);
	},
});

function DelegateRenderer({
	args,
	result,
	status,
	toolCallId,
}: {
	args: unknown;
	result: unknown;
	status: { type: string };
	toolCallId?: string;
}) {
	const agentName = (args as any)?.agent_id || (args as any)?.agent || "agent";
	const task = (args as any)?.task || (args as any)?.prompt || "";
	const isDone = status.type === "complete";
	const isRunning = status.type === "running";

	// Phase 3: Look up sub-agent details from Zustand store
	const agents = usePuxStore((s) => s.agents);
	const agentState = [...agents.values()].find(
		(a) => a.agentName === agentName && a.task === task
	);

	const output =
		typeof result === "string"
			? result
			: result
				? JSON.stringify(result, null, 2)
				: "";
	const resultLines = formatToolResult(result, 6);

	// Count sub-agent tool calls
	const subToolCount = agentState?.toolCalls.length ?? 0;

	return (
		<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
			<Box>
				<Text
					color={isDone ? colors.success : colors.running}
				>
					{BLACK_CIRCLE}{" "}
				</Text>
				<Text bold color={colors.brand}>
					{agentName}
				</Text>
				<Text color="gray">
					{" "}
					{isRunning ? "working..." : isDone ? "done" : ""}
				</Text>
				{subToolCount > 0 && (
					<Text color="gray"> {symbols.dot} {subToolCount} tools</Text>
				)}
			</Box>
			{task && (
				<Text dimColor color="gray">
					{"  "}
					{task.slice(0, 80)}
				</Text>
			)}

			{/* Phase 3: Nested tool call list from agent state */}
			{agentState && agentState.toolCalls.length > 0 && (
				<Box paddingLeft={2} flexDirection="column" marginTop={0}>
					{agentState.toolCalls.slice(0, 5).map((tc, i) => (
						<Box key={i}>
							<Text color={tc.isError ? colors.error : colors.success}>
								{tc.isError ? "  ✕" : "  ●"}{" "}
							</Text>
							<Text dimColor>{tc.toolName}</Text>
							{tc.result !== undefined && (
								<Text color="gray">
									{" "}
									{typeof tc.result === "string"
										? (tc.result as string).split("\n")[0]?.slice(0, 40) || ""
										: ""}
								</Text>
							)}
						</Box>
					))}
					{agentState.toolCalls.length > 5 && (
						<Text dimColor color="gray">
							{"  "}... +{agentState.toolCalls.length - 5} more
						</Text>
					)}
				</Box>
			)}

			{resultLines.length > 0 && isDone && !agentState && (
				<Box paddingLeft={2} flexDirection="column" marginTop={1}>
					{resultLines.map((line: string, i: number) => (
						<Text key={i} dimColor>{line}</Text>
					))}
				</Box>
			)}
		</Box>
	);
}

// ── File write/edit tool UI ──

export const FileEditToolUI = makeAssistantToolUI({
	toolName: "write_file",
	render: ({ args, isError, status }) => {
		const path = (args as any)?.path || (args as any)?.file_path || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";

		return (
			<Box paddingLeft={2} marginBottom={1}>
				<Text
					color={
						isError
							? colors.error
							: isDone
								? colors.success
								: colors.running
					}
				>
					{BLACK_CIRCLE}{" "}
				</Text>
				<Text bold color={isRunning ? colors.running : undefined}>
					write
				</Text>
				<Text color="gray"> {path.slice(0, 60)}</Text>
				{isDone && !isError && (
					<Text color={colors.success}> {symbols.check}</Text>
				)}
			</Box>
		);
	},
});

// ── File read tool UI ──

export const FileReadToolUI = makeAssistantToolUI({
	toolName: "read_file",
	render: ({ args, status }) => {
		const path = (args as any)?.path || (args as any)?.file_path || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";

		return (
			<Box paddingLeft={2} marginBottom={1}>
				<Text
					color={isDone ? colors.success : colors.running}
				>
					{BLACK_CIRCLE}{" "}
				</Text>
				<Text bold color={isRunning ? colors.running : undefined}>
					read
				</Text>
				<Text color="gray"> {path.slice(0, 60)}</Text>
			</Box>
		);
	},
});

// ── Client-side /exit tool (for slash command passthrough) ──

export const ExitTool = makeAssistantTool({
	toolName: "/exit",
	description: "Exit the TUI",
	parameters: {},
	execute: async () => {
		return "exit";
	},
});

// ── Tool Registry — mount inside AssistantRuntimeProvider ──
// Each makeAssistantToolUI returns a component that registers
// itself via hooks when mounted within the runtime context.

export function ToolRegistry() {
	return (
		<>
			<BashToolUI />
			<DelegateToolUI />
			<DelegateAsyncToolUI />
			<FileEditToolUI />
			<FileReadToolUI />
		</>
	);
}
