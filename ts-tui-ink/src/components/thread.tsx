/**
 * Thread component — message list only.
 *
 * The composer/input bar lives in ComposerBar (always rendered).
 * This component only renders the message area.
 *
 * Scrolling: ThreadPrimitive.Messages uses Ink's <Static> internally.
 * Messages beyond windowSize graduate to Static → written to terminal
 * scrollback → scroll wheel works natively.
 *
 * windowSize is kept small (2) because each message can be many lines
 * (thinking + tool calls + sub-agents). Too many live messages overflow
 * the content area and get clipped by overflow="hidden" in app.tsx.
 * windowOverscan=4 (library default) provides a buffer zone so messages
 * don't vanish at the scrollback boundary.
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
			 * Messages — small window so messages graduate to Static (scrollback)
			 * quickly. Overscan=4 (library default) absorbs boundary churn so
			 * content doesn't vanish at the edge.
			 *
			 * windowSize=2: last 2 messages stay core live.
			 * windowOverscan=4: 4 more messages in the live buffer zone.
			 * Total: 6 live messages. Older → Static → terminal scrollback.
			 */}
			<Box flexDirection="column">
				<ThreadPrimitive.Empty>
					<Welcome />
				</ThreadPrimitive.Empty>
				<ThreadPrimitive.Messages windowSize={2} windowOverscan={4}>
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
			<Box marginTop={1}>
				<Text>
					<Text dimColor>Model: </Text>
					<Text color={colors.assistant}>{modelLabel}</Text>
				</Text>
			</Box>
			{projectName && (
				<Text>
					<Text dimColor>Project: </Text>
					<Text color={colors.text}>{projectName}</Text>
				</Text>
			)}

			<Box flexDirection="column" marginTop={1}>
				<Text dimColor>
					{" "}Type a message to start, or try:
				</Text>
				<Box flexDirection="column" marginTop={1}>
					<Text>
						<Text color="gray">{"  "}/help</Text>
						<Text dimColor>  commands</Text>
					</Text>
					<Text>
						<Text color="gray">{"  "}/model</Text>
						<Text dimColor>  switch model</Text>
					</Text>
				</Box>
			</Box>
		</Box>
	);
}

