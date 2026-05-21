/**
 * Thread component — message list only.
 *
 * The composer/input bar lives in ComposerBar (always rendered).
 * This component only renders the message area.
 *
 * Scrolling: ThreadPrimitive.Messages uses Ink's <Static> internally.
 * Messages beyond windowSize+overscan graduate to Static → written to
 * terminal scrollback → scroll wheel works natively.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	ThreadPrimitive,
	useAuiState,
	useAuiEvent,
} from "@assistant-ui/react-ink";
import { AssistantMessage } from "./assistant-message.js";
import { UserMessage } from "./user-message.js";
import { usePuxStore } from "@pux/shared";
import { useColors } from "../theme.js";
import { createRequire } from "node:module";
const puxVersion = createRequire(import.meta.url)("../../../package.json").version;

// ── Thread ──

export function Thread() {
	// Lifecycle hooks
	useAuiEvent("thread.runStart", () => {});
	useAuiEvent("thread.runEnd", () => {});

	return (
		<Box flexDirection="column" flexGrow={1}>
			{/*
			 * Messages — ThreadPrimitive.Messages uses Ink <Static> internally.
			 * Older messages (beyond windowSize+overscan) are written to terminal
			 * scrollback via Static, so the scroll wheel works natively.
			 * windowSize=5 keeps the last 5 messages live; overscan=5 adds a
			 * buffer so up to 10 messages stay live before any graduate to Static.
			 */}
			<Box flexDirection="column">
				<ThreadPrimitive.Empty>
					<Welcome />
				</ThreadPrimitive.Empty>
				<ThreadPrimitive.Messages windowSize={5} windowOverscan={5}>
					{() => <MessageWrapper />}
				</ThreadPrimitive.Messages>
			</Box>
		</Box>
	);
}

// ── Message wrapper ──

function MessageWrapper() {
	const role = useAuiState((s) => s.message.role);
	return (
		<Box flexDirection="column">
			{role === "user" ? <UserMessage /> : <AssistantMessage />}
		</Box>
	);
}

// ── Welcome ──

function Welcome() {
	const colors = useColors();
	const activeModel = usePuxStore((s) => s.activeModel);
	const modelList = usePuxStore((s) => s.modelList);
	const projectPath = usePuxStore((s) => s.activeProjectPath);
	const projectName = usePuxStore((s) => s.activeProject);

	const modelEntry = modelList.find((m) => m.id === activeModel);
	const modelLabel = modelEntry?.name || activeModel || "no model";

	return (
		<Box flexDirection="column" paddingY={1} paddingX={2}>
			<Text bold color={colors.brand}>
				{" "}Pux v{puxVersion}
			</Text>
			<Box gap={1} marginTop={1}>
				<Text dimColor>Model:</Text>
				<Text color={colors.assistant}>{modelLabel}</Text>
			</Box>
			{projectName && (
				<Box gap={1}>
					<Text dimColor>Project:</Text>
					<Text color={colors.text}>{projectName}</Text>
				</Box>
			)}

			<Box flexDirection="column" marginTop={1}>
				<Text dimColor>
					{" "}Type a message to start, or try:
				</Text>
				<Box gap={1} marginTop={1}>
					<Text color="gray">{"  "}/help</Text>
					<Text dimColor>commands</Text>
					<Text color="gray">/model</Text>
					<Text dimColor>switch model</Text>
					<Text color="gray">/compact</Text>
					<Text dimColor>free context</Text>
				</Box>
			</Box>
		</Box>
	);
}

