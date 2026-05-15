/**
 * Custom tool UIs — specialized renderers for specific tool types.
 *
 * Uses makeAssistantToolUI from @assistant-ui/react-ink to register
 * custom renderers per tool name. Each tool gets its own visual treatment.
 *
 * Note: status is an object { type: "running" | "complete" | "incomplete" | "requires-action" }
 * not a string. Access status.type for comparison.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	makeAssistantToolUI,
	makeAssistantTool,
} from "@assistant-ui/react-ink";
import { colors, symbols, BLACK_CIRCLE } from "../theme.js";

// ── Bash execution tool UI ──

export const BashToolUI = makeAssistantToolUI({
	toolName: "bash",
	render: ({ args, result, isError, status }) => {
		const command = (args as any)?.command || (args as any)?.cmd || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";
		const output =
			typeof result === "string"
				? result
				: result
					? JSON.stringify(result, null, 2)
					: "";

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
				{output && !isRunning && (
					<Box paddingLeft={2} flexDirection="column">
						{output
							.split("\n")
							.slice(0, 10)
							.map((line: string, i: number) => (
								<Text key={i} dimColor={isError} color={isError ? colors.error : undefined}>
									{line}
								</Text>
							))}
						{output.split("\n").length > 10 && (
							<Text dimColor color="gray">
								{"  "}...{output.split("\n").length - 10} more lines
							</Text>
						)}
					</Box>
				)}
			</Box>
		);
	},
});

// ── Delegate/sub-agent tool UI ──

export const DelegateToolUI = makeAssistantToolUI({
	toolName: "delegate_to",
	render: ({ args, result, status }) => {
		const agentName = (args as any)?.agent_id || (args as any)?.agent || "agent";
		const task = (args as any)?.task || (args as any)?.prompt || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";
		const output =
			typeof result === "string"
				? result
				: result
					? JSON.stringify(result, null, 2)
					: "";

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
				</Box>
				{task && (
					<Text dimColor color="gray">
						{"  "}
						{task.slice(0, 80)}
					</Text>
				)}
				{output && isDone && (
					<Box paddingLeft={2} flexDirection="column" marginTop={1}>
						<Text dimColor>
							{output
								.split("\n")
								.slice(0, 6)
								.join("\n")}
						</Text>
						{output.split("\n").length > 6 && (
							<Text dimColor color="gray">
								{"  "}...{output.split("\n").length - 6} more lines
							</Text>
						)}
					</Box>
				)}
			</Box>
		);
	},
});

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
			<FileEditToolUI />
			<FileReadToolUI />
		</>
	);
}
