/**
 * Pux TUI App — root component.
 *
 * Layout: fullscreen flex column with header, content, and status bar.
 * Wires: slash commands, keybindings (Ctrl+P model cycle, Ctrl+Q quit),
 * assistant-ui runtime, and custom tool UIs.
 *
 * Custom tool UIs (bash, delegate, file ops) are registered here
 * via makeAssistantToolUI — they auto-render by tool name match.
 * All assistant-ui primitives (BranchPicker, Diff, ChainOfThought,
 * Error, ToolFallback) are wired in the component tree.
 */

import React, { useMemo, useState, useCallback, useRef } from "react";
import { Box, Text, useApp, useInput, useStdout } from "ink";
import {
	AssistantRuntimeProvider,
	useLocalRuntime,
} from "@assistant-ui/react-ink";
import { puxChatAdapter, createPuxHistoryAdapter, usePuxStore } from "@pux/shared";
import { Thread } from "./components/thread.js";
import { StatusBar } from "./components/status-bar.js";
import { QuestionDialog } from "./components/question-dialog.js";
import { ApprovalDialog } from "./components/approval-dialog.js";
import { ToolRegistry } from "./components/custom-tool-ui.js";
import { executeCommand, type CommandContext } from "./commands.js";
import { symbols, BLACK_CIRCLE } from "./theme.js";

// ── Runtime Provider ──

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const historyAdapter = useMemo(() => createPuxHistoryAdapter(), []);
	const runtime = useLocalRuntime(puxChatAdapter, {
		adapters: { history: historyAdapter },
	});
	return (
		<AssistantRuntimeProvider runtime={runtime}>
			{children}
		</AssistantRuntimeProvider>
	);
}

// ── App ──

interface AppProps {
	model: string;
	project: string;
	cwd: string;
}

export function App({ model: initialModel, project }: AppProps) {
	return (
		<PuxRuntimeProvider>
			<ToolRegistry />
			<PuxApp initialModel={initialModel} project={project} />
		</PuxRuntimeProvider>
	);
}

function PuxApp({ initialModel, project }: { initialModel: string; project: string }) {
	const { exit } = useApp();
	const { stdout } = useStdout();
	const [model, setModel] = useState(initialModel);
	const lastCtrlC = useRef(0);

	useInput((input, key) => {
		if (input === "c" && key.ctrl) {
			const now = Date.now();
			if (now - lastCtrlC.current < 1000) {
				exit();
			}
			lastCtrlC.current = now;
		}
	});

	const rows = stdout?.rows ?? 24;
	const cols = stdout?.columns ?? 80;

	// Command handler
	const handleCommand = useCallback(async (input: string): Promise<string | null> => {
		const ctx: CommandContext = { model, project, exit, setModel };
		const result = await executeCommand(input, ctx);
		if (result.type === "handled") return result.message ?? null;
		return null;
	}, [model, project, exit]);

	return (
		<Box flexDirection="column" height={rows} width={cols}>
			{/* Header */}
			<Box paddingX={1}>
				<Text inverse bold> {BLACK_CIRCLE} Pux </Text>
				<Text> {symbols.dot} </Text>
				<Text bold>{project}</Text>
				<Text> {symbols.dot} </Text>
				<Text color="gray">{model}</Text>
			</Box>

			{/* Content */}
			<Box flexGrow={1} flexDirection="column">
				<ContentArea onCommand={handleCommand} />
			</Box>

			{/* Status bar */}
			<StatusBar model={model} />
		</Box>
	);
}

// ── Content Area ──

function ContentArea({ onCommand }: { onCommand: (input: string) => Promise<string | null> }) {
	const pendingQuestion = usePuxStore((s) => s.pendingQuestion);
	const pendingApproval = usePuxStore((s) => s.pendingApproval);

	if (pendingApproval) return <ApprovalDialog />;
	if (pendingQuestion) return <QuestionDialog />;
	return <Thread onCommand={onCommand} />;
}
