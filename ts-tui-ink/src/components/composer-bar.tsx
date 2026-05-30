/**
 * ComposerBar — always-visible input bar with slash command support.
 *
 * Extracted from Thread so it renders below every view (chat, agents,
 * conversations, etc.). Without this, switching to a non-chat view
 * removes the only focused input component, and Ink stops processing
 * stdin — the user gets stuck.
 *
 * Uses a custom input instead of ComposerPrimitive.Input to guarantee
 * synchronous text clearing on Enter — no flash possible.
 */

import React, { useState, useMemo, useCallback, useRef, useEffect } from "react";
import { Box, Text, useInput, useFocus } from "ink";
import { useAuiState, useAui, ComposerPrimitive, QueueItemPrimitive } from "@assistant-ui/react-ink";
import { usePuxStore } from "@pux/shared";
import { getCommands } from "../commands.js";
import { PathAutocomplete, getCompletions } from "./path-autocomplete.js";
import { CommandRow } from "./help-overlay.js";
import { useTerminalSize } from "../use-terminal-size.js";
import { useColors, symbols } from "../theme.js";

// ── Constants ──

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
			<ComposerPrimitive.Queue>
				{({ queueItem }) => (
					<Box>
						<Text color={colors.warning}>{symbols.dot} queued: </Text>
						<QueueItemPrimitive.Text dimColor />
					</Box>
				)}
			</ComposerPrimitive.Queue>

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

// ── Simple grapheme helper ──

function getGraphemeAt(text: string, offset: number): string {
	if (offset < 0 || offset >= text.length) return "";
	// For ASCII-heavy terminal input, code point = grapheme is almost always correct
	return text[offset];
}

// ── Command-aware composer (custom input, no library dependency) ──

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
	const storeText = useAuiState((s) => s.composer.text);
	const historyRef = useRef({ sent: [] as string[], browsing: false, draft: "" });
	const activeTuiView = usePuxStore((s) => s.activeTuiView);
	const { isFocused } = useFocus({ autoFocus: true });

	// ── Local buffer (useRef, not useState) ──
	// Renders from the ref directly. No React state intermediary.
	// This guarantees: clear the ref → next render shows empty.
	const bufRef = useRef({ text: "", cursor: 0 });
	const [, forceRender] = useState(0);

	// Sync FROM store (external setText calls, e.g. path autocomplete)
	// Skip sync while sending to avoid restoring stale store text
	useEffect(() => {
		if (sendingRef.current) return;
		if (storeText !== bufRef.current.text) {
			bufRef.current = { text: storeText, cursor: storeText.length };
			forceRender((n) => n + 1);
		}
	}, [storeText]);

	// Send flag — when true, render empty regardless of bufRef
	const sendingRef = useRef(false);
	useEffect(() => {
		if (sendingRef.current && storeText === "") {
			sendingRef.current = false;
			forceRender((n) => n + 1);
		}
	}, [storeText]);

	const text = sendingRef.current ? "" : bufRef.current.text;

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

	// ── Send helper ──
	const doSend = useCallback(() => {
		const submitted = bufRef.current.text.trim();
		if (!submitted) return;

		// 1. Clear local buffer synchronously
		bufRef.current = { text: "", cursor: 0 };
		sendingRef.current = true;

		// 2. Clear store text synchronously
		aui.composer().setText("");

		// 3. Append message to thread
		if (submitted.startsWith("/")) {
			onCommand(submitted).then((output) => {
				if (output) onOutput(output);
			});
		} else {
			aui.thread().append({
				content: [{ type: "text", text: submitted }],
				startRun: true,
			});
		}
	}, [aui, onCommand, onOutput]);

	// ── Input handler ──
	useInput(useCallback((_input: string, key: any) => {
		if (!isFocused) return;

		// When a non-chat view or overlay is active, don't consume arrow keys
		const s = usePuxStore.getState();
		const inOverlay = s.agentSelectorOpen || s.zoomedAgentId || s.showProvidersOverlay
			|| s.showSettingsOverlay || s.showSessionSwitcher || s.showLogViewer
			|| s.showSearchOverlay || s.showHelpOverlay || s.showMCPOverlay
			|| !!s.pendingDecision;
		const inNonChatView = s.activeTuiView !== "chat";
		if ((inOverlay || inNonChatView) && (key.upArrow || key.downArrow)) {
			return;
		}

		// Slash command autocomplete navigation
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
				const newText = "/" + matches[selectedIdx] + " ";
				bufRef.current = { text: newText, cursor: newText.length };
				aui.composer().setText(newText);
				forceRender((n) => n + 1);
				return;
			}
			// Don't process other keys while palette is open
			if (!key.return) return;
		}

		// Path autocomplete navigation
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
				const cur = bufRef.current.text;
				const pathMatch = cur.match(/((?:\.\.?\/|[~/])(?:[^\s"'`|;]*)?)$/);
				if (pathMatch && completion) {
					const before = cur.slice(0, pathMatch.index ?? 0);
					const newText = before + completion.display;
					bufRef.current = { text: newText, cursor: newText.length };
					aui.composer().setText(newText);
					forceRender((n) => n + 1);
				}
				return;
			}
			if (!key.return && !key.backspace && !_input) return;
		}

		// Enter → send
		if (key.return) {
			doSend();
			return;
		}

		// Ctrl shortcuts
		const lower = _input.toLowerCase();
		if (key.ctrl) {
			if (lower === "a") return; // move-home
			if (lower === "e") return; // move-end
			if (lower === "w") { // kill-word-backward
				const cur = bufRef.current;
				const beforeCursor = cur.text.slice(0, cur.cursor);
				const afterCursor = cur.text.slice(cur.cursor);
				const trimmed = beforeCursor.replace(/\S+\s*$/, "");
				const newText = trimmed + afterCursor;
				bufRef.current = { text: newText, cursor: trimmed.length };
				aui.composer().setText(newText);
				forceRender((n) => n + 1);
				return;
			}
			if (lower === "u") { // kill-start
				const cur = bufRef.current;
				const newText = cur.text.slice(cur.cursor);
				bufRef.current = { text: newText, cursor: 0 };
				aui.composer().setText(newText);
				forceRender((n) => n + 1);
				return;
			}
			if (lower === "k") { // kill-end
				const cur = bufRef.current;
				const newText = cur.text.slice(0, cur.cursor);
				bufRef.current = { text: newText, cursor: cur.cursor };
				aui.composer().setText(newText);
				forceRender((n) => n + 1);
				return;
			}
			return;
		}

		// Backspace
		if (key.backspace) {
			const cur = bufRef.current;
			if (cur.cursor > 0) {
				const newText = cur.text.slice(0, cur.cursor - 1) + cur.text.slice(cur.cursor);
				bufRef.current = { text: newText, cursor: cur.cursor - 1 };
				aui.composer().setText(newText);
				forceRender((n) => n + 1);
			}
			return;
		}

		// Left/Right arrows
		if (key.leftArrow) {
			if (bufRef.current.cursor > 0) {
				bufRef.current.cursor--;
				forceRender((n) => n + 1);
			}
			return;
		}
		if (key.rightArrow) {
			if (bufRef.current.cursor < bufRef.current.text.length) {
				bufRef.current.cursor++;
				forceRender((n) => n + 1);
			}
			return;
		}

		// History navigation (up/down when not in multiline)
		if (key.upArrow) {
			const h = historyRef.current;
			if (h.sent.length === 0) return;
			if (!h.browsing) {
				h.draft = bufRef.current.text;
				h.browsing = true;
			}
			const idx = h.sent.indexOf(bufRef.current.text);
			const prevIdx = idx < 0 ? 0 : Math.min(idx + 1, h.sent.length - 1);
			const newText = h.sent[prevIdx];
			bufRef.current = { text: newText, cursor: newText.length };
			aui.composer().setText(newText);
			forceRender((n) => n + 1);
			return;
		}
		if (key.downArrow) {
			const h = historyRef.current;
			if (!h.browsing || h.sent.length === 0) return;
			const idx = h.sent.indexOf(bufRef.current.text);
			if (idx > 0) {
				const newText = h.sent[idx - 1];
				bufRef.current = { text: newText, cursor: newText.length };
				aui.composer().setText(newText);
			} else {
				h.browsing = false;
				bufRef.current = { text: h.draft, cursor: h.draft.length };
				aui.composer().setText(h.draft);
			}
			forceRender((n) => n + 1);
			return;
		}

		// Printable character insertion
		if (_input && !key.ctrl && !key.meta) {
			const cur = bufRef.current;
			const newText = cur.text.slice(0, cur.cursor) + _input + cur.text.slice(cur.cursor);
			bufRef.current = { text: newText, cursor: cur.cursor + _input.length };
			aui.composer().setText(newText);
			forceRender((n) => n + 1);
		}
	}, [isFocused, matches, selectedIdx, onSelectIdx, pathCompletions, pathIdx, onPathIdx, doSend, aui, text, activeTuiView]));

	// Track history by watching thread messages
	const prevMsgCount = useRef(0);
	const msgs = useAuiState((s) => s.thread.messages);
	useEffect(() => {
		if (msgs.length > prevMsgCount.current) {
			const lastMsg = msgs[msgs.length - 1];
			if (lastMsg?.role === "user") {
				const content = typeof lastMsg.content === "string"
					? lastMsg.content
					: Array.isArray(lastMsg.content)
						? lastMsg.content.filter((p: any) => p.type === "text").map((p: any) => p.text).join("")
						: "";
				const trimmed = content.trim();
				const h = historyRef.current;
				if (trimmed && !trimmed.startsWith("/") && (h.sent.length === 0 || h.sent[0] !== trimmed)) {
					h.sent.unshift(trimmed);
					if (h.sent.length > MAX_HISTORY) h.sent.pop();
				}
				h.browsing = false;
				h.draft = "";
			}
		}
		prevMsgCount.current = msgs.length;
	}, [msgs]);

	// ── Render ──
	const displayText = sendingRef.current ? "" : bufRef.current.text;
	const cursor = sendingRef.current ? 0 : bufRef.current.cursor;
	const hasText = displayText.length > 0;

	if (!isFocused) {
		return <Text>{displayText}</Text>;
	}

	const before = hasText ? displayText.slice(0, cursor) : "";
	const charAtCursor = hasText ? getGraphemeAt(displayText, cursor) : "";
	const atCursor = charAtCursor === "" ? " " : charAtCursor;
	const after = hasText ? displayText.slice(cursor + charAtCursor.length) : "";

	return (
		<Text>
			{before}
			<Text inverse>{atCursor}</Text>
			{after}
		</Text>
	);
}
