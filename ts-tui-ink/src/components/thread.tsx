/**
 * Thread component — message list + composer with slash command support.
 *
 * Uses assistant-ui primitives: ThreadPrimitive, ComposerPrimitive,
 * MessagePrimitive, ToolCallPrimitive, BranchPickerPrimitive,
 * ActionBarPrimitive, DiffPrimitive.
 */

import React, { useState, useEffect, useMemo, useRef, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import {
	ThreadPrimitive,
	ComposerPrimitive,
	useAuiState,
	useAui,
} from "@assistant-ui/react-ink";
import { AssistantMessage } from "./assistant-message.js";
import { UserMessage } from "./user-message.js";
import { ActionBar } from "./action-bar.js";
import { BranchPicker } from "./branch-picker.js";
import { getCommands } from "../commands.js";
import { colors, BLACK_CIRCLE, BLOCKQUOTE_BAR, symbols } from "../theme.js";

// ── Thread ──

interface ThreadProps {
	onCommand: (input: string) => Promise<string | null>;
}

export function Thread({ onCommand }: ThreadProps) {
	const [commandOutput, setCommandOutput] = useState<string | null>(null);

	// Auto-dismiss command output after 5s
	useEffect(() => {
		if (!commandOutput) return;
		const timer = setTimeout(() => setCommandOutput(null), 5000);
		return () => clearTimeout(timer);
	}, [commandOutput]);

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
					{() => <MessageWrapper />}
				</ThreadPrimitive.Messages>
			</Box>

			{/* Slash command autocomplete */}
			<CommandPalette />

			{/* Input area */}
			<Box borderStyle="round" borderColor="gray" paddingX={1}>
				<Text color={colors.brand} bold>{">"}</Text>
				<Text> </Text>
				<CommandComposer onCommand={onCommand} onOutput={setCommandOutput} />
			</Box>
		</Box>
	);
}

// ── Command palette — filtered autocomplete overlay ──

function CommandPalette() {
	const text = useAuiState((s) => s.composer.text);
	const selectedIdx = useAuiState((s) => s.composer.text); // re-render on text change

	const matches = useMemo(() => {
		if (!text || !text.startsWith("/")) return [];
		const query = text.slice(1).toLowerCase();
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => ({ name: c.name, desc: c.description }));
	}, [text]);

	if (matches.length === 0) return null;

	return (
		<Box flexDirection="column" paddingX={1}>
			{matches.map((c, i) => (
				<Text key={c.name}>
					{i === 0 ? (
						<Text bold color={colors.brand}>/{c.name}</Text>
					) : (
						<Text>/{" " + c.name}</Text>
					)}
					<Text color="gray"> {symbols.dot} {c.desc}</Text>
				</Text>
			))}
			<Text dimColor color="gray"> Tab to autocomplete</Text>
		</Box>
	);
}

// ── Message wrapper with branching and action bar ──

function MessageWrapper() {
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

// ── Command-aware composer with Tab autocomplete ──

function CommandComposer({
	onCommand,
	onOutput,
}: {
	onCommand: (input: string) => Promise<string | null>;
	onOutput: (out: string | null) => void;
}) {
	const aui = useAui();
	const text = useAuiState((s) => s.composer.text);
	const tabIdx = useRef(0);

	// Reset tab index when text changes to something not matching
	const matches = useMemo(() => {
		if (!text || !text.startsWith("/")) return [];
		const query = text.slice(1).toLowerCase();
		// Only match while still typing the command name (no space yet)
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => c.name);
	}, [text]);

	// Keep tabIdx in range
	useEffect(() => {
		if (matches.length === 0) { tabIdx.current = 0; return; }
		if (tabIdx.current >= matches.length) tabIdx.current = 0;
	}, [matches.length]);

	useInput(useCallback((_input: string, key: any) => {
		if (!key.tab) return;
		if (matches.length === 0) return;
		const chosen = matches[tabIdx.current];
		aui.composer().setText("/" + chosen + " ");
		tabIdx.current = (tabIdx.current + 1) % matches.length;
	}, [matches, aui]));

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
				aui.composer().send();
				return true;
			}}
		/>
	);
}
