/**
 * Thread component — message list + composer.
 *
 * Layout: messages scroll up, composer pinned at bottom in a bordered box.
 * Welcome screen shown when thread is empty.
 */

import React from "react";
import { Box, Text } from "ink";
import {
	ThreadPrimitive,
	ComposerPrimitive,
	useAuiState,
} from "@assistant-ui/react-ink";
import { AssistantMessage } from "./assistant-message.js";
import { UserMessage } from "./user-message.js";
import { colors, BLACK_CIRCLE, BLOCKQUOTE_BAR } from "../theme.js";

// ── Thread ──

export function Thread() {
	return (
		<Box flexDirection="column" flexGrow={1}>
			{/* Messages area */}
			<Box flexDirection="column" flexGrow={1}>
				<ThreadPrimitive.Empty>
					<Welcome />
				</ThreadPrimitive.Empty>
				<ThreadPrimitive.Messages>
					{() => <Message />}
				</ThreadPrimitive.Messages>
			</Box>

			{/* Input area — bordered box */}
			<Box borderStyle="round" borderColor="gray" paddingX={1}>
				<Text color={colors.brand} bold>{">"}</Text>
				<Text> </Text>
				<ComposerPrimitive.Input
					submitOnEnter
					placeholder="Message..."
					autoFocus
				/>
			</Box>
		</Box>
	);
}

// ── Message router ──

function Message() {
	const role = useAuiState((s) => s.message.role);

	if (role === "user") return <UserMessage />;
	return <AssistantMessage />;
}

// ── Welcome ──

function Welcome() {
	return (
		<Box flexDirection="column" paddingY={1} paddingX={2}>
			<Text bold color={colors.brand}>{BLACK_CIRCLE} Pux {BLOCKQUOTE_BAR} Agent Orchestrator</Text>
			<Box marginTop={1}>
				<Text dimColor>Type a message to get started.</Text>
			</Box>
			<Box marginTop={1}>
				<Text dimColor>
					<Text bold>Ctrl+Q</Text> Quit   <Text bold>Enter</Text> Send
				</Text>
			</Box>
		</Box>
	);
}
