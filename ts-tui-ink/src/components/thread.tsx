/**
 * Thread component — message list + composer with slash command support.
 *
 * Scrolling: Manual offset with overflow="hidden" and marginTop.
 * Old messages go to Ink Static (terminal scrollback).
 * Auto-scrolls to bottom during streaming.
 */

import React, { useState, useEffect, useMemo, useCallback } from "react";
import { Box, Text, useInput, useStdout } from "ink";
import {
	ThreadPrimitive,
	ComposerPrimitive,
	useAuiState,
	useAui,
} from "@assistant-ui/react-ink";
import { AssistantMessage } from "./assistant-message.js";
import { UserMessage } from "./user-message.js";
import { getCommands } from "../commands.js";
import { colors, symbols } from "../theme.js";

// ── Thread ──

interface ThreadProps {
	onCommand: (input: string) => Promise<string | null>;
}

export function Thread({ onCommand }: ThreadProps) {
	const [commandOutput, setCommandOutput] = useState<string | null>(null);
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [scrollOffset, setScrollOffset] = useState(0);
	const { stdout } = useStdout();
	const cols = stdout?.columns ?? 80;
	const rows = stdout?.rows ?? 24;

	// Fixed rows: status bar(1) + separators(2) + prompt(1) = 4
	const viewportRows = rows - 4;

	// Auto-dismiss command output after 5s
	useEffect(() => {
		if (!commandOutput) return;
		const timer = setTimeout(() => setCommandOutput(null), 5000);
		return () => clearTimeout(timer);
	}, [commandOutput]);

	// Scroll keyboard controls
	useInput(useCallback((_input: string, key: any) => {
		if (key.pageDown) {
			setScrollOffset((prev) => Math.max(0, prev - Math.floor(viewportRows / 2)));
			return;
		}
		if (key.pageUp) {
			setScrollOffset((prev) => prev + Math.floor(viewportRows / 2));
			return;
		}
		if (key.upArrow && key.shift) {
			setScrollOffset((prev) => prev + 1);
			return;
		}
		if (key.downArrow && key.shift) {
			setScrollOffset((prev) => Math.max(0, prev - 1));
			return;
		}
		if (key.escape) {
			setScrollOffset(0);
			return;
		}
	}, [viewportRows]));

	// Auto-scroll to bottom when streaming
	const lastMsgRunning = useAuiState((s) => {
		const msgs = s.thread.messages;
		const last = msgs[msgs.length - 1];
		return last?.role === "assistant" && last.status?.type === "running";
	});
	const msgCount = useAuiState((s) => s.thread.messages.length);

	useEffect(() => {
		setScrollOffset(0); // Reset scroll when new messages arrive
	}, [msgCount, lastMsgRunning]);

	const isScrolledUp = scrollOffset > 0;

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

			{/* Messages — overflow hidden, manual scroll offset */}
			<Box flexDirection="column" height={viewportRows} overflow="hidden">
				<Box flexDirection="column" marginTop={-scrollOffset}>
					<ThreadPrimitive.Empty>
						<Welcome />
					</ThreadPrimitive.Empty>
					<ThreadPrimitive.Messages windowSize={100} windowOverscan={10}>
						{() => <MessageWrapper />}
					</ThreadPrimitive.Messages>
				</Box>
			</Box>

			{/* Scroll indicator */}
			{isScrolledUp && (
				<Text dimColor color="gray"> PgUp/PgDn scroll · Shift+Up/Down line · Esc bottom ({scrollOffset} lines up)</Text>
			)}

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

// ── Command palette ──

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
			<Text dimColor color="gray"> Up/Down navigate · Tab autocomplete</Text>
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
	return (
		<Box flexDirection="column" paddingY={1} paddingX={2}>
			<Text bold color={colors.brand}>Pux {symbols.dot} Agent Orchestrator</Text>
			<Box marginTop={1}>
				<Text dimColor>Type a message or /help for commands.</Text>
			</Box>
			<Box marginTop={1} flexDirection="column">
				<Text dimColor>
					<Text bold>Enter</Text> Send  <Text bold>PgUp/PgDn</Text> Scroll  <Text bold>Ctrl+C x2</Text> Quit
				</Text>
			</Box>
		</Box>
	);
}

// ── Command-aware composer ──

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

	const matches = useMemo(() => {
		if (!text || !text.startsWith("/")) return [];
		const query = text.slice(1).toLowerCase();
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => c.name);
	}, [text]);

	useEffect(() => {
		if (matches.length === 0) { onSelectIdx(0); return; }
		if (selectedIdx >= matches.length) onSelectIdx(0);
	}, [matches.length, selectedIdx, onSelectIdx]);

	useInput(useCallback((_input: string, key: any) => {
		if (matches.length === 0) return;
		if (key.upArrow) {
			onSelectIdx(selectedIdx <= 0 ? matches.length - 1 : selectedIdx - 1);
			return;
		}
		if (key.downArrow) {
			onSelectIdx((selectedIdx + 1) % matches.length);
			return;
		}
		if (key.tab) {
			aui.composer().setText("/" + matches[selectedIdx] + " ");
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
					setTimeout(() => aui.composer().setText(""), 0);
				} else if (trimmed.length > 0) {
					aui.composer().send();
				}
			}}
			placeholder="Message..."
			autoFocus
		/>
	);
}
