/**
 * Thread component — message list + composer with slash command support.
 *
 * Uses assistant-ui primitives: ThreadPrimitive, ComposerPrimitive,
 * MessagePrimitive, ToolCallPrimitive, BranchPickerPrimitive,
 * ActionBarPrimitive, DiffPrimitive.
 */

import React, { useState, useEffect, useMemo, useRef, useCallback } from "react";
import { Box, Text, useInput, useStdout } from "ink";
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
	const [selectedIdx, setSelectedIdx] = useState(0);
	const { stdout } = useStdout();
	const cols = stdout?.columns ?? 80;

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
			<CommandPalette selectedIdx={selectedIdx} onSelectIdx={setSelectedIdx} />

			{/* Input area */}
			<Text color="gray">{"─".repeat(cols)}</Text>
			<Box paddingX={1}>
				<Text color={colors.brand} bold>{">"}</Text>
				<Text> </Text>
				<CommandComposer
					onCommand={onCommand}
					onOutput={setCommandOutput}
					selectedIdx={selectedIdx}
					onSelectIdx={setSelectedIdx}
				/>
			</Box>
			<Text color="gray">{"─".repeat(cols)}</Text>
		</Box>
	);
}

// ── Command palette — filtered autocomplete overlay ──

function CommandPalette({
	selectedIdx,
}: {
	selectedIdx: number;
	onSelectIdx: (i: number) => void;
}) {
	const text = useAuiState((s) => s.composer.text);

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
					{i === selectedIdx ? (
						<Text bold color={colors.brand}>/{c.name}</Text>
					) : (
						<Text>/{c.name}</Text>
					)}
					<Text color="gray"> {symbols.dot} {c.desc}</Text>
				</Text>
			))}
			<Text dimColor color="gray"> ↑↓ Navigate   Tab to autocomplete</Text>
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
					<Text bold>Enter</Text> Send   <Text bold>Shift+Enter</Text> Newline   <Text bold>Ctrl+C x2</Text> Quit
				</Text>
			</Box>
		</Box>
	);
}

// ── Command-aware composer with Tab/Arrow autocomplete ──

function CommandComposer({
	onCommand,
	onOutput,
	selectedIdx,
	onSelectIdx,
}: {
	onCommand: (input: string) => Promise<string | null>;
	onOutput: (out: string | null) => void;
	selectedIdx: number;
	onSelectIdx: (i: number) => void;
}) {
	const aui = useAui();
	const text = useAuiState((s) => s.composer.text);

	// Slash command matches for autocomplete
	const matches = useMemo(() => {
		if (!text || !text.startsWith("/")) return [];
		const query = text.slice(1).toLowerCase();
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => c.name);
	}, [text]);

	// Clamp selection when matches change
	useEffect(() => {
		if (matches.length === 0) { onSelectIdx(0); return; }
		if (selectedIdx >= matches.length) onSelectIdx(0);
	}, [matches.length, selectedIdx, onSelectIdx]);

	// Up/Down/Tab → navigate or autocomplete slash commands
	useInput(useCallback((_input: string, key: any) => {
		if (matches.length === 0) return;

		if (key.upArrow) {
			const next = selectedIdx <= 0 ? matches.length - 1 : selectedIdx - 1;
			onSelectIdx(next);
			return;
		}
		if (key.downArrow) {
			const next = (selectedIdx + 1) % matches.length;
			onSelectIdx(next);
			return;
		}
		if (key.tab) {
			const chosen = matches[selectedIdx];
			aui.composer().setText("/" + chosen + " ");
			return;
		}
	}, [matches, aui, selectedIdx, onSelectIdx]));

	return (
		<ComposerPrimitive.Input
			submitOnEnter={true}
			multiLine={true}
			onSubmit={(submittedText: string) => {
				const trimmed = submittedText.trim();
				if (trimmed.startsWith("/")) {
					onCommand(trimmed).then((output) => {
						if (output) onOutput(output);
					});
					aui.composer().setText("");
				} else if (trimmed.length > 0) {
					aui.composer().send();
				}
			}}
			placeholder="Message..."
			autoFocus
		/>
	);
}
