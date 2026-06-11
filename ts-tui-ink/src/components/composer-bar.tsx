/**
 * ComposerBar — input bar using library ComposerPrimitive.Input.
 *
 * Uses @assistant-ui/react-ink's ComposerPrimitive.Input for text editing,
 * cursor management, and keyboard bindings. Slash commands are intercepted
 * via the onSubmit callback — the library skips its default send when
 * onSubmit is provided, giving us full control.
 *
 * Keyboard bindings handled by the library:
 *   Ctrl+A/E (home/end), Ctrl+W (kill-word), Ctrl+U/K (kill-line),
 *   Ctrl+D (delete-forward), Alt+B/F (word nav), Alt+D (kill-word-forward),
 *   arrows, backspace, delete, home/end, Enter (submit)
 */

import React, { useMemo, useCallback, useEffect, useState } from "react";
import { Box, Text, useApp, useInput } from "ink";
import {
	ComposerPrimitive,
	QueueItemPrimitive,
	useAui,
	useAuiState,
} from "@assistant-ui/react-ink";
import { usePuxStore } from "@pux/shared";
import { executeCommand, type CommandContext, getCommands } from "../commands.js";
import { CommandRow } from "./help-overlay.js";
import { useTerminalSize } from "../use-terminal-size.js";
import { useColors, symbols } from "../theme.js";

// ── ComposerBar ──

export function ComposerBar() {
	const [commandOutput, setCommandOutput] = useState<string | null>(null);
	const { cols } = useTerminalSize();
	const colors = useColors();
	const composerText = useAuiState((s) => s.composer.text);
	const aui = useAui();
	const [selectedIdx, setSelectedIdx] = useState(0);

	// Slash command matches (shared between palette + input handlers)
	const matches = useMemo(() => {
		if (!composerText || !composerText.startsWith("/")) return [];
		const query = composerText.slice(1).toLowerCase();
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => ({ name: c.name, desc: c.description }));
	}, [composerText]);

	// Reset selection when matches change
	useEffect(() => { setSelectedIdx(0); }, [composerText]);

	// Left arrow on empty input → agents view
	// Arrow navigation + Tab autocomplete when palette is visible
	useInput(useCallback((_input: string, key: any) => {
		if (key.leftArrow && (!composerText || composerText.length === 0)) {
			usePuxStore.getState().setTuiView("agents");
			return;
		}
		// Only handle these when palette is visible
		if (matches.length === 0) return;
		const total = matches.length;
		if (key.upArrow) {
			setSelectedIdx((prev) => (prev - 1 + total) % total);
			return;
		}
		if (key.downArrow) {
			setSelectedIdx((prev) => (prev + 1) % total);
			return;
		}
		if (key.tab) {
			const selected = matches[selectedIdx];
			if (selected) {
				aui.composer().setText("/" + selected.name + " ");
			}
			return;
		}
	}, [composerText, matches, selectedIdx, aui]));

	// Auto-dismiss command output after 5s
	useEffect(() => {
		if (!commandOutput) return;
		const timer = setTimeout(() => setCommandOutput(null), 5000);
		return () => clearTimeout(timer);
	}, [commandOutput]);

	return (
		<Box flexDirection="column">
			{/* Command output overlay */}
			{commandOutput && (
				<Box flexDirection="column" paddingX={1} marginBottom={0}>
					<Text color={colors.textMuted}>
						{commandOutput.split("\n").map((line, i) => (
							<Text key={i}>{line}</Text>
						))}
					</Text>
				</Box>
			)}

			{/* Composer queue */}
			<ComposerPrimitive.Queue>
				{({ queueItem }) => (
					<Box>
						<Text color={colors.warning}>{symbols.dot} queued: </Text>
						<QueueItemPrimitive.Text dimColor />
					</Box>
				)}
			</ComposerPrimitive.Queue>

			{/* Slash command autocomplete */}
			<CommandPalette matches={matches} selectedIdx={selectedIdx} />

			{/* Input area */}
			<Text color={colors.subtle}>{"─".repeat(cols)}</Text>
			<Box paddingX={1}>
				<Text color={colors.brand} bold>{">"} </Text>
				<PuxInput onCommandOutput={setCommandOutput} />
			</Box>
			<Text color={colors.subtle}>{"─".repeat(cols)}</Text>
		</Box>
	);
}

// ── Command palette ──

function CommandPalette({
	matches,
	selectedIdx,
}: {
	matches: { name: string; desc: string }[];
	selectedIdx: number;
}) {
	const colors = useColors();
	const VISIBLE = 5;

	if (matches.length === 0) return null;

	// Scroll window to keep the selected item visible
	const maxOffset = Math.max(0, matches.length - VISIBLE);
	const scrollOffset = Math.max(0, Math.min(selectedIdx - VISIBLE + 1, maxOffset));
	const visible = matches.slice(scrollOffset, scrollOffset + VISIBLE);

	return (
		<Box flexDirection="column" paddingX={1}>
			{visible.map((c, i) => (
				<CommandRow
					key={c.name}
					name={c.name}
					description={c.desc}
					selected={scrollOffset + i === selectedIdx}
				/>
			))}
			<Text color={colors.textMuted}> Enter to execute  Tab to complete</Text>
		</Box>
	);
}

// ── Input with command interception ──

function PuxInput({
	onCommandOutput,
}: {
	onCommandOutput: (out: string | null) => void;
}) {
	const aui = useAui();
	const { exit } = useApp();

	// Handle submit — intercept slash commands, pass chat to runtime
	const handleSubmit = useCallback((text: string) => {
		const trimmed = text.trim();
		if (!trimmed) return;

		if (trimmed.startsWith("/")) {
			const cmdName = trimmed.slice(1).split(" ")[0].toLowerCase();

			// Execute the slash command
			const ctx: CommandContext = {
				model: usePuxStore.getState().activeModel || "",
				project: usePuxStore.getState().activeProject || "",
				exit,
				setModel: (m: string) => usePuxStore.getState().setModel(m),
			};

			// Clear input and execute
			aui.composer().setText("");

			executeCommand(trimmed, ctx).then((result) => {
				if (result.type === "handled" && result.message) {
					onCommandOutput(result.message);
				}
			});
		} else {
			// Normal chat — send through runtime
			aui.composer().send();
		}
	}, [aui, exit, onCommandOutput]);

	return (
		<ComposerPrimitive.Input
			submitOnEnter
			multiLine
			placeholder="Type a message..."
			autoFocus
			onSubmit={handleSubmit}
		/>
	);
}
