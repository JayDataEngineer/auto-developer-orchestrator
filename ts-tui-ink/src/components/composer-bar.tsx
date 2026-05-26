/**
 * ComposerBar — always-visible input bar with slash command support.
 *
 * Extracted from Thread so it renders below every view (chat, agents,
 * conversations, etc.). Without this, switching to a non-chat view
 * removes the only focused input component, and Ink stops processing
 * stdin — the user gets stuck.
 */

import React, { useState, useMemo, useCallback, useRef, useEffect } from "react";
import { Box, Text, useInput } from "ink";
import { useAuiState, useAui, ComposerPrimitive } from "@assistant-ui/react-ink";
import { usePuxStore } from "@pux/shared";
import { getCommands } from "../commands.js";
import { PathAutocomplete, getCompletions } from "./path-autocomplete.js";
import { ComposerQueue } from "./composer-queue.js";
import { CommandRow } from "./help-overlay.js";
import { useTerminalSize } from "../use-terminal-size.js";
import { useColors } from "../theme.js";

// ── Command history (module-level, persists across renders) ──

const sentHistory: string[] = [];
let historyBrowsing = false;
const MAX_HISTORY = 200;

// ── ComposerBar ──

interface ComposerBarProps {
	onCommand: (input: string) => Promise<string | null>;
}

export function ComposerBar({ onCommand }: ComposerBarProps) {
	const [commandOutput, setCommandOutput] = useState<string | null>(null);
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [pathIdx, setPathIdx] = useState(0);
	const { cols } = useTerminalSize();
	const colors = useColors();

	// Check if the CTO loop is active — block composer when it is.
	// This covers: agents running AND the main message still streaming.
	const runningAgentCount = usePuxStore((s) => {
		let n = 0;
		for (const a of s.agents.values()) {
			if (a.status === "running") n++;
		}
		return n;
	});
	const ctoRunning = usePuxStore((s) => s.ctoRunning);
	const isBlocked = runningAgentCount > 0 || ctoRunning;

	// Auto-dismiss command output after 5s
	useEffect(() => {
		if (!commandOutput) return;
		const timer = setTimeout(() => setCommandOutput(null), 5000);
		return () => clearTimeout(timer);
	}, [commandOutput]);

	const composerText = useAuiState((s) => s.composer.text);
	const projectPath = usePuxStore((s) => s.activeProjectPath);

	return (
		<Box flexDirection="column">
			{/* Command output overlay */}
			{commandOutput && (
				<Box flexDirection="column" paddingX={1} marginBottom={0}>
					<Text dimColor color="gray">
						{commandOutput.split("\n").map((line, i) => (
							<Text key={i}>{line}</Text>
						))}
					</Text>
				</Box>
			)}

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
			<Text color={colors.subtle}>{"─".repeat(cols)}</Text>
			{isBlocked ? (
				<Box paddingX={1} gap={1}>
					<Text color={colors.running}>●</Text>
					<Text color={colors.textDim}>
						{runningAgentCount > 0
							? `${runningAgentCount} agent${runningAgentCount !== 1 ? "s" : ""} running`
							: "thinking"
						} · Esc Esc to cancel
					</Text>
				</Box>
			) : (
				<Box paddingX={1}>
					<Text color={colors.brand} bold>{">"} </Text>
					<CommandComposer
						onCommand={onCommand}
						onOutput={setCommandOutput}
						selectedIdx={selectedIdx}
						onSelectIdx={setSelectedIdx}
						pathIdx={pathIdx}
						onPathIdx={setPathIdx}
						projectPath={projectPath}
					/>
				</Box>
			)}
			<Text color={colors.subtle}>{"─".repeat(cols)}</Text>
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

	const MAX_VISIBLE = 5;
	const startIdx = Math.max(0, Math.min(selectedIdx - MAX_VISIBLE + 1, matches.length - MAX_VISIBLE));
	const visible = matches.slice(startIdx, startIdx + MAX_VISIBLE);

	return (
		<Box flexDirection="column" paddingX={1}>
			{visible.map((c, i) => {
				const globalIdx = startIdx + i;
				return (
					<CommandRow
						key={c.name}
						name={c.name}
						description={c.desc}
						selected={globalIdx === selectedIdx}
					/>
				);
			})}
			<Text dimColor color="gray"> Up/Down navigate · Tab autocomplete</Text>
		</Box>
	);
}

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
	const activeTuiView = usePuxStore((s) => s.activeTuiView);

	// Only enable multiline editing in chat view — frees up/down arrows
	// for other views (agents, files, tools, conversations)
	const isMultiLine = activeTuiView === "chat";

	const matches = useMemo(() => {
		if (!text || !text.startsWith("/")) return [];
		const query = text.slice(1).toLowerCase();
		if (query.includes(" ")) return [];
		return getCommands()
			.filter((c) => c.name.startsWith(query))
			.map((c) => c.name);
	}, [text]);

	const pathCompletions = useMemo(() => {
		if (matches.length > 0) return [];
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
		// When a non-chat view or overlay is active, don't consume arrow keys
		// so the active view's own useInput handler can use them
		const s = usePuxStore.getState();
		const inOverlay = s.agentSelectorOpen || s.zoomedAgentId || s.showProvidersOverlay
			|| s.showSettingsOverlay || s.showSessionSwitcher || s.showLogViewer
			|| s.showSearchOverlay || s.showHelpOverlay || s.showMCPOverlay
			|| !!s.pendingDecision;
		const inNonChatView = s.activeTuiView !== "chat";
		if ((inOverlay || inNonChatView) && (key.upArrow || key.downArrow)) {
			return;
		}

		if (matches.length > 0) {
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
			if (key.upArrow) {
				onPathIdx(pathIdx <= 0 ? pathCompletions.length - 1 : pathIdx - 1);
				return;
			}
			if (key.downArrow) {
				onPathIdx((pathIdx + 1) % pathCompletions.length);
				return;
			}
			if (key.tab) {
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

		// History navigation
		if (key.upArrow) {
			if (sentHistory.length === 0) return;
			if (!historyBrowsing) {
				draftRef.current = text;
				historyBrowsing = true;
			}
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
				historyBrowsing = false;
				aui.composer().setText(draftRef.current);
			}
			return;
		}
	}, [matches, text, aui, selectedIdx, onSelectIdx, pathIdx, onPathIdx, pathCompletions, isMultiLine]));

	const handleSubmit = useCallback((submittedText: string) => {
		const trimmed = submittedText.trim();
		if (trimmed.startsWith("/")) {
			aui.composer().setText("");
			onCommand(trimmed).then((output) => {
				if (output) onOutput(output);
			});
		} else if (trimmed.length > 0) {
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
		<ComposerPrimitive.Input
			submitOnEnter={true}
			multiLine={isMultiLine}
			onSubmit={handleSubmit}
			placeholder="Message..."
			autoFocus
		/>
	);
}
