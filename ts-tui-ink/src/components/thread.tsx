/**
 * Thread component — message list + composer with slash command support.
 *
 * Scrolling: Manual offset with overflow="hidden" and marginTop.
 * Content height is estimated from messages to cap the scroll offset.
 * Auto-scrolls to bottom during streaming.
 *
 * Phase 1 additions: Cancel button, suggestion chips, composer queue.
 */

import React, { useState, useEffect, useMemo, useCallback } from "react";
import { Box, Text, useInput, useStdout } from "ink";
import {
	ThreadPrimitive,
	ComposerPrimitive,
	useAuiState,
	useAui,
	useAuiEvent,
} from "@assistant-ui/react-ink";
import { AssistantMessage } from "./assistant-message.js";
import { UserMessage } from "./user-message.js";
import { SuggestionChips } from "./suggestion-chips.js";
import { ComposerQueue } from "./composer-queue.js";
import { getCommands } from "../commands.js";
import { colors, symbols } from "../theme.js";

// ── Content height estimation ──

/** Count visual lines for text, accounting for terminal wrapping. */
function wrappedLineCount(text: string, maxWidth: number): number {
	if (!text) return 0;
	let count = 0;
	for (const line of text.split("\n")) {
		// Strip ANSI escape sequences for width calculation
		const clean = line.replace(/\x1b\[[0-9;]*m/g, "");
		count += Math.max(1, Math.ceil((clean.length + 1) / maxWidth));
	}
	return count;
}

/** Estimate rendered line height of a message (for scroll cap). */
function estimateMessageHeight(msg: any, cols: number): number {
	// Effective width for text: cols minus padding (2 chars from paddingLeft)
	const textWidth = Math.max(20, cols - 2);

	if (msg.role === "user") {
		const text = typeof msg.content === "string"
			? msg.content
			: Array.isArray(msg.content)
				? msg.content.filter((p: any) => p.type === "text").map((p: any) => p.text).join("")
				: "";
		const lines = text ? wrappedLineCount(text, textWidth) : 1;
		return 1 + lines; // marginTop + text lines
	}

	// Assistant message
	const parts = msg.parts || [];
	let height = 1; // marginTop
	for (const part of parts) {
		switch (part.type) {
			case "reasoning":
				height += 1; // collapsed accordion (single line)
				break;
			case "tool-call":
				height += 1; // tool name line
				if (part.result !== undefined) height += 3; // result preview (max 3 lines)
				if (part.isError) height += 1;
				break;
			case "text":
				if (part.text?.trim()) {
					height += wrappedLineCount(part.text, textWidth);
				}
				break;
			case "source":
				height += 1; // source URL line
				break;
		}
	}
	return height;
}

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

	// Fixed rows: status bar(1) + tab bar(1) + separators(2) + prompt(1) = 5
	const viewportRows = rows - 5;

	// Estimate total content height from messages (accounts for line wrapping)
	const contentHeight = useAuiState((s) => {
		const msgs = s.thread.messages;
		if (msgs.length === 0) return 0; // welcome screen handled separately
		let h = 0;
		for (const msg of msgs) {
			h += estimateMessageHeight(msg, cols);
		}
		// Small safety margin for Ink spacing between components.
		// Keep this small — overestimating causes content to be pushed
		// above the viewport and become invisible.
		h += msgs.length * 2;
		return h;
	});

	// Max scroll offset — don't scroll past the content
	const contentOverhang = Math.max(0, contentHeight - viewportRows);
	const maxScrollOffset = contentOverhang;

	// Auto-dismiss command output after 5s
	useEffect(() => {
		if (!commandOutput) return;
		const timer = setTimeout(() => setCommandOutput(null), 5000);
		return () => clearTimeout(timer);
	}, [commandOutput]);

	// Scroll keyboard controls — capped to content height
	// scrollOffset=0 shows bottom (most recent), scrollOffset=max shows top
	useInput(useCallback((_input: string, key: any) => {
		const pageStep = Math.floor(viewportRows / 2);
		if (key.pageDown) {
			// PageDown → toward bottom → decrease offset
			setScrollOffset((prev) => Math.max(0, prev - pageStep));
			return;
		}
		if (key.pageUp) {
			// PageUp → toward top → increase offset
			setScrollOffset((prev) => Math.min(maxScrollOffset, prev + pageStep));
			return;
		}
		if (key.upArrow && key.shift) {
			setScrollOffset((prev) => Math.min(maxScrollOffset, prev + 1));
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
	}, [viewportRows, maxScrollOffset]));

	// Auto-scroll to bottom when streaming
	const lastMsgRunning = useAuiState((s) => {
		const msgs = s.thread.messages;
		const last = msgs[msgs.length - 1];
		return last?.role === "assistant" && last.status?.type === "running";
	});
	const msgCount = useAuiState((s) => s.thread.messages.length);

	useEffect(() => {
		setScrollOffset(0); // Reset to bottom when new messages arrive
	}, [msgCount, lastMsgRunning]);

	// Phase 1: useAuiEvent lifecycle hooks
	useAuiEvent("thread.runStart", () => {
		// Could trigger notification, sound, etc.
	});
	useAuiEvent("thread.runEnd", () => {
		// Auto-scroll is handled by the effect above
	});

	// Clamp offset if content shrinks
	const clampedOffset = Math.min(scrollOffset, maxScrollOffset);
	const isScrolledUp = clampedOffset > 0;

	// marginTop: 0 = content starts at top (shows top).
	// We want to show the bottom by default.
	// Base shift = contentOverhang (shifts content up to show bottom).
	// Scrolling up decreases the shift (reveals older content above).
	const marginTop = -(contentOverhang - clampedOffset);

	// Running state for cancel button
	const isRunning = useAuiState((s) => s.thread.isRunning);

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
				<Box flexDirection="column" marginTop={marginTop}>
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
				<Text dimColor color="gray"> PgUp/PgDn scroll · Shift+Up/Down line · Esc bottom ({clampedOffset} lines up)</Text>
			)}

			{/* Composer queue — queued messages */}
			<ComposerQueue />

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
				{/* Phase 1: Cancel button when running */}
				{isRunning && (
					<ComposerPrimitive.Cancel>
						<Text color="red"> {" "}cancel</Text>
					</ComposerPrimitive.Cancel>
				)}
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
					<Text bold>Enter</Text> Send  <Text bold>PgUp/PgDn</Text> Scroll  <Text bold>Ctrl+C x2</Text> Quit  <Text bold>Ctrl+T</Text> Switch view
				</Text>
			</Box>
			{/* Phase 1: Suggestion chips on empty thread */}
			<SuggestionChips />
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
