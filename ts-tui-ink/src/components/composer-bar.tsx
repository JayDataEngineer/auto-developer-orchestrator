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

	// Left arrow on empty input → agents view
	useInput(useCallback((_input: string, key: any) => {
		if (key.leftArrow && (!composerText || composerText.length === 0)) {
			usePuxStore.getState().setTuiView("agents");
		}
	}, [composerText]));

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
			<CommandPalette />

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

function CommandPalette() {
	const text = useAuiState((s) => s.composer.text);
	const colors = useColors();

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
			{matches.slice(0, 5).map((c, i) => (
				<CommandRow
					key={c.name}
					name={c.name}
					description={c.desc}
					selected={i === 0}
				/>
			))}
			<Text color={colors.textMuted}> Enter to execute</Text>
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
	const storeText = useAuiState((s) => s.composer.text);

	// Track slash command matches for autocomplete
	const matches = useMemo(() => {
		if (!storeText || !storeText.startsWith("/")) return [];
		const query = storeText.slice(1).toLowerCase();
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => c.name);
	}, [storeText]);

	// Handle submit — intercept slash commands, pass chat to runtime
	const handleSubmit = useCallback((text: string) => {
		const trimmed = text.trim();
		if (!trimmed) return;

		if (trimmed.startsWith("/")) {
			const cmdName = trimmed.slice(1).split(" ")[0].toLowerCase();
			const hasArgs = trimmed.includes(" ");
			const isExactMatch = getCommands().some((c) => c.name === cmdName);

			// Autocomplete partial match (only if not already a complete command)
			if (!isExactMatch && !hasArgs && matches.length === 1) {
				const completed = "/" + matches[0] + " ";
				aui.composer().setText(completed);
				return;
			}

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
	}, [aui, exit, matches, onCommandOutput]);

	return (
		<ComposerPrimitive.Input
			submitOnEnter
			placeholder="Type a message..."
			autoFocus
			onSubmit={handleSubmit}
		/>
	);
}
