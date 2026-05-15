/**
 * Pux TUI App — root component.
 *
 * Layout: full-height flex column with header, content, and status bar.
 * Header uses inverse text for visual weight on all terminals.
 */

import React, { useMemo } from "react";
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

export function App({ model, project }: AppProps) {
	return (
		<PuxRuntimeProvider>
			<PuxApp model={model} project={project} />
		</PuxRuntimeProvider>
	);
}

function PuxApp({ model, project }: { model: string; project: string }) {
	const { exit } = useApp();
	const { stdout } = useStdout();

	useInput((input, key) => {
		if (input === "q" && key.ctrl) {
			exit();
		}
	});

	// Use actual terminal dimensions for fullscreen
	const rows = stdout?.rows ?? 24;
	const cols = stdout?.columns ?? 80;

	return (
		<Box flexDirection="column" height={rows} width={cols} borderStyle="round" borderColor="gray">
			{/* Header bar */}
			<Box paddingX={1}>
				<Text inverse bold> {BLACK_CIRCLE} Pux </Text>
				<Text> {symbols.dot} </Text>
				<Text bold>{project}</Text>
			</Box>

			{/* Content: thread or HITL dialog — fills remaining space */}
			<Box flexGrow={1} flexDirection="column">
				<ContentArea />
			</Box>

			{/* Status bar */}
			<StatusBar model={model} />
		</Box>
	);
}

// ── Content Area ──

function ContentArea() {
	const pendingQuestion = usePuxStore((s) => s.pendingQuestion);
	const pendingApproval = usePuxStore((s) => s.pendingApproval);

	if (pendingApproval) return <ApprovalDialog />;
	if (pendingQuestion) return <QuestionDialog />;
	return <Thread />;
}
