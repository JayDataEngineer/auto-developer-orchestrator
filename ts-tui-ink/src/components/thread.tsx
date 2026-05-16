/**
 * Thread component — message list + composer with slash command support.
 *
 * Scrolling: ThreadPrimitive.Messages uses Ink's <Static> internally.
 * Messages beyond windowSize+overscan graduate to Static → written to
 * terminal scrollback → scroll wheel works natively.
 *
 * Phase 1 additions: Cancel button, suggestion chips, composer queue.
 */

import React, { useState, useEffect, useMemo, useCallback, useRef } from "react";
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
import { usePuxStore } from "@pux/shared";
import { getCommands } from "../commands.js";
import { PathAutocomplete, getCompletions } from "./path-autocomplete.js";
import { VimInput } from "./vim-input.js";
import { useColors, symbols } from "../theme.js";

// ── Thread ──

interface ThreadProps {
	onCommand: (input: string) => Promise<string | null>;
}

export function Thread({ onCommand }: ThreadProps) {
	const [commandOutput, setCommandOutput] = useState<string | null>(null);
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [pathIdx, setPathIdx] = useState(0);
	const { stdout } = useStdout();
	const cols = stdout?.columns ?? 80;

	// Auto-dismiss command output after 5s
	useEffect(() => {
		if (!commandOutput) return;
		const timer = setTimeout(() => setCommandOutput(null), 5000);
		return () => clearTimeout(timer);
	}, [commandOutput]);

	// Phase 1: useAuiEvent lifecycle hooks
	useAuiEvent("thread.runStart", () => {
		// Could trigger notification, sound, etc.
	});
	useAuiEvent("thread.runEnd", () => {
		// Stream completed
	});

	// Running state for cancel button
	const isRunning = useAuiState((s) => s.thread.isRunning);
	const composerText = useAuiState((s) => s.composer.text);
	const projectPath = usePuxStore((s) => s.activeProjectPath);
	const colors = useColors();

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

			{/*
			 * Messages — ThreadPrimitive.Messages uses Ink <Static> internally.
			 * Older messages (beyond windowSize+overscan) are written to terminal
			 * scrollback via Static, so the scroll wheel works natively.
			 * windowSize=5 keeps the last 5 messages live; overscan=5 adds a
			 * buffer so up to 10 messages stay live before any graduate to Static.
			 */}
			<Box flexDirection="column" flexGrow={1}>
				<ThreadPrimitive.Empty>
					<Welcome />
				</ThreadPrimitive.Empty>
				<ThreadPrimitive.Messages windowSize={5} windowOverscan={5}>
					{() => <MessageWrapper />}
				</ThreadPrimitive.Messages>
			</Box>

			{/* Composer queue — queued messages */}
			<ComposerQueue />

			{/* Slash command autocomplete */}
			<CommandPalette selectedIdx={selectedIdx} onSelectIdx={setSelectedIdx} />

			{/* Path autocomplete */}
			<PathAutocomplete
				text={composerText}
				cwd={projectPath}
				selectedIdx={pathIdx}
			/>

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
					pathIdx={pathIdx}
					onPathIdx={setPathIdx}
					projectPath={projectPath}
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
	const colors = useColors();
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

	const MAX_VISIBLE = 5;
	const visible = matches.slice(0, MAX_VISIBLE);
	const hasMore = matches.length > MAX_VISIBLE;

	return (
		<Box flexDirection="column" paddingX={1}>
			{visible.map((c, i) => (
				<Text key={c.name}>
					{i === selectedIdx ? (
						<Text bold color={colors.brand}>/{c.name}</Text>
					) : (
						<Text>/{c.name}</Text>
					)}
					<Text color="gray"> {symbols.dot} {c.desc}</Text>
				</Text>
			))}
			{hasMore && (
				<Text dimColor color="gray">  ... {matches.length - MAX_VISIBLE} more</Text>
			)}
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
	const colors = useColors();
	const activeModel = usePuxStore((s) => s.activeModel);
	const modelList = usePuxStore((s) => s.modelList);
	const projectPath = usePuxStore((s) => s.activeProjectPath);
	const projectName = usePuxStore((s) => s.activeProject);

	const modelEntry = modelList.find((m) => m.id === activeModel);
	const modelLabel = modelEntry?.name || activeModel || "no model";

	// Shorten path: ~/Documents/programs/dev/foo → …/dev/foo
	const shortPath = projectPath
		? "…" + projectPath.replace(/.*\/([^/]+\/[^/]+)$/, "/$1")
		: projectName;

	return (
		<Box flexDirection="column" paddingY={2} paddingX={3}>
			<Box>
				<Box flexDirection="column" marginRight={2}>
					<Text color={colors.brand}>{"▐▛██▜▌"}</Text>
					<Text color={colors.brand}>{"▝▜██▛▘"}</Text>
					<Text color={colors.brand}>{" ▘▘▝▝ "}</Text>
				</Box>
				<Box flexDirection="column" justifyContent="center">
					<Text bold color={colors.brand}>Pux</Text>
					<Text dimColor>{modelLabel}</Text>
					<Text dimColor>{shortPath}</Text>
				</Box>
			</Box>
		</Box>
	);
}

// ── Command history (module-level, persists across renders) ──

const sentHistory: string[] = [];
let historyBrowsing = false; // true while user is navigating history
const MAX_HISTORY = 200;

// ── Command-aware composer ──

function CommandComposer({
	onCommand,
	onOutput,
	selectedIdx,
	onSelectIdx,
	pathIdx,
	onPathIdx,
	projectPath,
}: {
	onCommand: (input: string) => Promise<string | null>;
	onOutput: (out: string | null) => void;
	selectedIdx: number;
	onSelectIdx: (i: number) => void;
	pathIdx: number;
	onPathIdx: (i: number) => void;
	projectPath: string;
}) {
	const aui = useAui();
	const text = useAuiState((s) => s.composer.text);
	const draftRef = useRef("");

	const matches = useMemo(() => {
		if (!text || !text.startsWith("/")) return [];
		const query = text.slice(1).toLowerCase();
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => c.name);
	}, [text]);

	const pathCompletions = useMemo(() => {
		if (matches.length > 0) return []; // Don't show path completions when command palette is active
		return getCompletions(text || "", projectPath);
	}, [text, matches.length, projectPath]);

	useEffect(() => {
		if (matches.length === 0) { onSelectIdx(0); return; }
		if (selectedIdx >= matches.length) onSelectIdx(0);
	}, [matches.length, selectedIdx, onSelectIdx]);

	useEffect(() => {
		if (pathCompletions.length === 0) { onPathIdx(0); return; }
		if (pathIdx >= pathCompletions.length) onPathIdx(0);
	}, [pathCompletions.length, pathIdx, onPathIdx]);

	useInput(useCallback((_input: string, key: any) => {
		if (matches.length > 0) {
			// Command palette navigation takes priority
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
			return;
		}

		if (pathCompletions.length > 0) {
			// Path autocomplete navigation
			if (key.upArrow) {
				onPathIdx(pathIdx <= 0 ? pathCompletions.length - 1 : pathIdx - 1);
				return;
			}
			if (key.downArrow) {
				onPathIdx((pathIdx + 1) % pathCompletions.length);
				return;
			}
			if (key.tab) {
				// Replace the path prefix with the selected completion
				const completion = pathCompletions[pathIdx];
				const pathMatch = text?.match(/((?:\.\.?\/|[~/])(?:[^\s"'`|;]*)?)$/);
				if (pathMatch && completion) {
					const before = text?.slice(0, pathMatch.index);
					const newText = (before || "") + completion.display;
					aui.composer().setText(newText);
				}
				return;
			}
			return;
		}

		// History navigation (no command palette showing)
		if (key.upArrow) {
			if (sentHistory.length === 0) return;
			if (!historyBrowsing) {
				// Save current draft before navigating history
				draftRef.current = text;
				historyBrowsing = true;
			}
			// Find current position in history
			const idx = sentHistory.indexOf(text);
			const prevIdx = idx < 0 ? 0 : Math.min(idx + 1, sentHistory.length - 1);
			aui.composer().setText(sentHistory[prevIdx]);
			return;
		}
		if (key.downArrow) {
			if (!historyBrowsing || sentHistory.length === 0) return;
			const idx = sentHistory.indexOf(text);
			if (idx > 0) {
				aui.composer().setText(sentHistory[idx - 1]);
			} else {
				// Restore draft (past newest entry)
				historyBrowsing = false;
				aui.composer().setText(draftRef.current);
			}
			return;
		}
	}, [matches, text, aui, selectedIdx, onSelectIdx, pathIdx, onPathIdx, pathCompletions]));

	const handleSubmit = useCallback((submittedText: string) => {
		const trimmed = submittedText.trim();
		if (trimmed.startsWith("/")) {
			aui.composer().setText("");
			onCommand(trimmed).then((output) => {
				if (output) onOutput(output);
			});
		} else if (trimmed.length > 0) {
			// Save to history before sending
			if (sentHistory.length === 0 || sentHistory[0] !== trimmed) {
				sentHistory.unshift(trimmed);
				if (sentHistory.length > MAX_HISTORY) sentHistory.pop();
			}
			historyBrowsing = false;
			draftRef.current = "";
			aui.composer().send();
		}
	}, [aui, onCommand, onOutput]);

	return (
		<VimInput
			submitOnEnter={true}
			multiLine={true}
			onSubmit={handleSubmit}
			placeholder="Message..."
			autoFocus
		/>
	);
}
