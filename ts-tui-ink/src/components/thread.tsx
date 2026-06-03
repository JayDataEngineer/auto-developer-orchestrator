/**
 * Thread component — messages + composer.
 *
 * windowSize=2 + windowOverscan=4 = 6 live messages.
 * Older messages graduate to Ink's <Static> (terminal scrollback).
 * ComposerBar is inside ThreadPrimitive.Root (matching library example).
 */

import React from "react";
import { Box, Text } from "ink";
import {
	ThreadPrimitive,
	AuiIf,
	useAuiState,
} from "@assistant-ui/react-ink";
import { AssistantMessage } from "./assistant-message.js";
import { UserMessage } from "./user-message.js";
import { ComposerBar } from "./composer-bar.js";
import { usePuxStore } from "@pux/shared";
import { useColors } from "../theme.js";
import { createRequire } from "node:module";
const puxVersion = createRequire(import.meta.url)("../../../package.json").version;

// ── Thread (messages + composer) ──

export function Thread() {
	return (
		<ThreadPrimitive.Root flexDirection="column">
			{/* Empty state */}
			<AuiIf condition={(s: any) => s.thread.isEmpty}>
				<Welcome />
			</AuiIf>

			<ThreadPrimitive.Messages windowSize={2} windowOverscan={4}>
				{() => <MessageWrapper />}
			</ThreadPrimitive.Messages>

			{/* Composer — inside ThreadPrimitive.Root like library example */}
			<ComposerBar />
		</ThreadPrimitive.Root>
	);
}

// ── Message wrapper ──

const MessageWrapper = React.memo(function MessageWrapper() {
	const role = useAuiState((s) => s.message.role);
	return (
		<Box flexDirection="column">
			{role === "user" ? <UserMessage /> : <AssistantMessage />}
		</Box>
	);
});

// ── Welcome ──

function Welcome() {
	const colors = useColors();
	const activeModel = usePuxStore((s) => s.activeModel);
	const modelList = usePuxStore((s) => s.modelList);
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
				<Text dimColor>{" "}Type a message to start, or try:</Text>
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
