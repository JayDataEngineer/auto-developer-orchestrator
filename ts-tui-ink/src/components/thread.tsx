/**
 * Thread component — message list + composer with slash command support.
 *
 * Uses assistant-ui primitives: ThreadPrimitive, ComposerPrimitive,
 * MessagePrimitive, ToolCallPrimitive, BranchPickerPrimitive,
 * ActionBarPrimitive, DiffPrimitive.
 */

import React, { useState } from "react";
import { Box, Text, useInput } from "ink";
import {
	ThreadPrimitive,
	ComposerPrimitive,
	useAuiState,
} from "@assistant-ui/react-ink";
import { AssistantMessage } from "./assistant-message.js";
import { UserMessage } from "./user-message.js";
import { ActionBar } from "./action-bar.js";
import { BranchPicker } from "./branch-picker.js";
import { colors, BLACK_CIRCLE, BLOCKQUOTE_BAR, symbols } from "../theme.js";

// ── Thread ──

interface ThreadProps {
	onCommand: (input: string) => Promise<string | null>;
}

export function Thread({ onCommand }: ThreadProps) {
	const [commandOutput, setCommandOutput] = useState<string | null>(null);

	useInput((_ch, key) => {
		if (key.escape && commandOutput) {
			setCommandOutput(null);
		}
	});

	return (
		<Box flexDirection="column" flexGrow={1}>
			{/* Command output overlay */}
			{commandOutput && (
				<Box flexDirection="column" paddingX={1} marginBottom={1}>
					<Text dimColor color="gray">
						{commandOutput.split("\n").map((line, i) => (
							<Text key={i}>{line}</Text>
						))}
					</Text>
				</Box>
			)}

			{/* Messages */}
			<Box flexDirection="column" flexGrow={1}>
				<ThreadPrimitive.Empty>
					<Welcome />
				</ThreadPrimitive.Empty>
				<ThreadPrimitive.Messages>
					{() => <MessageWrapper onCommand={onCommand} setCommandOutput={setCommandOutput} />}
				</ThreadPrimitive.Messages>
			</Box>

			{/* Input area */}
			<Box borderStyle="round" borderColor="gray" paddingX={1}>
				<Text color={colors.brand} bold>{">"}</Text>
				<Text> </Text>
				<CommandComposer onCommand={onCommand} onOutput={setCommandOutput} />
			</Box>
		</Box>
	);
}

// ── Message wrapper with branching and action bar ──

function MessageWrapper({
	onCommand,
	setCommandOutput,
}: {
	onCommand: (input: string) => Promise<string | null>;
	setCommandOutput: (out: string | null) => void;
}) {
	const role = useAuiState((s) => s.message.role);
	return (
		<Box flexDirection="column">
			{role === "user" ? <UserMessage /> : <AssistantMessage />}
			<ActionBar />
		</Box>
	);
}

// ── Welcome ──

function Welcome() {
	return (
		<Box flexDirection="column" paddingY={1} paddingX={2}>
			<Text bold color={colors.brand}>{BLACK_CIRCLE} Pux {BLOCKQUOTE_BAR} Agent Orchestrator</Text>
			<Box marginTop={1}>
				<Text dimColor>Type a message or /help for commands.</Text>
			</Box>
			<Box marginTop={1} flexDirection="column">
				<Text dimColor>
					<Text bold>Ctrl+C x2</Text> Quit   <Text bold>Ctrl+P</Text> Model   <Text bold>Enter</Text> Send
				</Text>
			</Box>
		</Box>
	);
}

// ── Command-aware composer ──

function CommandComposer({
	onCommand,
	onOutput,
}: {
	onCommand: (input: string) => Promise<string | null>;
	onOutput: (out: string | null) => void;
}) {
	return (
		<ComposerPrimitive.Input
			submitOnEnter
			placeholder="Message... (type / for commands)"
			autoFocus
			onSubmit={(value) => {
				const trimmed = value?.trim() ?? "";
				if (trimmed.startsWith("/")) {
					onCommand(trimmed).then((output) => {
						if (output) onOutput(output);
					});
					return false;
				}
				return true;
			}}
		/>
	);
}
