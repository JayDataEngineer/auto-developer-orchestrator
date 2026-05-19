/**
 * Pux TUI App — root component.
 *
 * Layout: fullscreen flex column with tab bar, content, and status bar.
 * Wires: slash commands, keybindings (Ctrl+P model cycle, Ctrl+Q quit,
 * Ctrl+T view cycle), assistant-ui runtime, and custom tool UIs.
 *
 * Phase 4: View router switches between Chat, Agents, Tools, Files views.
 * Phase 1: Full primitive integration (Cancel, Edit, Feedback, Suggestions).
 * Phase 3: Subagent monitoring via Zustand store + AgentsView.
 */

import React, { useMemo, useState, useCallback, useRef } from "react";
import { Box, Text, useApp, useInput } from "ink";
import { useTerminalSize } from "./use-terminal-size.js";
import {
	AssistantRuntimeProvider,
	useLocalRuntime,
} from "@assistant-ui/react-ink";
import type { FeedbackAdapter, SuggestionAdapter } from "@assistant-ui/react-ink";
import { puxChatAdapter, createPuxHistoryAdapter, usePuxStore } from "@pux/shared";
import { getFetch } from "@pux/shared";
import { apiUrl } from "@pux/shared";
import { Thread } from "./components/thread.js";
import { StatusBar } from "./components/status-bar.js";
import { TabBar } from "./components/tab-bar.js";
import { AgentsView } from "./components/agents-view.js";
import { ToolsView } from "./components/tools-view.js";
import { FilesView } from "./components/files-view.js";
import { ConversationsView } from "./components/conversations-view.js";
import { ProvidersOverlay } from "./components/providers-overlay.js";
import { SettingsOverlay } from "./components/settings-overlay.js";
import { SessionSwitcher } from "./components/session-switcher.js";
import { LogViewer } from "./components/log-viewer.js";
import { FilePicker } from "./components/file-picker.js";
import { SearchOverlay } from "./components/search-overlay.js";
import { MCPOverlay } from "./components/mcp-overlay.js";
import { HelpOverlay } from "./components/help-overlay.js";
import { QuestionDialog } from "./components/question-dialog.js";
import { DecisionDialog } from "./components/decision-dialog.js";
import { ToolRegistry } from "./components/custom-tool-ui.js";
import { ThemeProvider } from "./theme.js";
import { executeCommand, type CommandContext } from "./commands.js";

// ── Runtime Provider ──

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const historyAdapter = useMemo(() => createPuxHistoryAdapter(), []);
	const feedbackAdapter = useMemo<FeedbackAdapter>(
		() => ({
			submit: ({ message, type }) => {
				const fetch = getFetch();
				fetch(apiUrl("/api/pux/feedback"), {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({
						messageId: message.id,
						type,
						role: message.role,
					}),
				}).catch(() => {
					// Endpoint may not exist yet — silent no-op
				});
			},
		}),
		[],
	);
	const suggestionAdapter = useMemo<SuggestionAdapter>(
		() => ({
			generate: async ({ messages }) => {
				const isEmpty = messages.length === 0;
				if (!isEmpty) {
					// After conversation: contextual follow-ups from backend
					const fetch = getFetch();
					try {
						const resp = await fetch(apiUrl("/api/pux/suggestions"), {
							method: "POST",
							headers: { "Content-Type": "application/json" },
							body: JSON.stringify({ messages: messages.slice(-4) }),
						});
						if (resp.ok) {
							const data = await resp.json() as { suggestions?: string[] };
							if (data.suggestions?.length) {
								return data.suggestions.map((prompt) => ({ prompt }));
							}
						}
					} catch { /* fallback below */ }
				}
				// Empty thread or backend unavailable — return defaults
				return [
					{ prompt: "What can you do? What tools and agents do you have available?" },
					{ prompt: "Show me the project structure and explain the architecture" },
					{ prompt: "Run the tests and show me the results" },
				];
			},
		}),
		[],
	);
	const runtime = useLocalRuntime(puxChatAdapter, {
		adapters: {
			history: historyAdapter,
			feedback: feedbackAdapter,
			suggestion: suggestionAdapter,
		},
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

// Wrapper that re-keys the runtime provider when conversationKey changes,
// causing useLocalRuntime to be fully recreated (clears internal message state).
function PuxRuntimeWrapper({ children }: { children: React.ReactNode }) {
	const conversationKey = usePuxStore((s) => s.conversationKey);
	return (
		<PuxRuntimeProvider key={conversationKey}>
			{children}
		</PuxRuntimeProvider>
	);
}

export function App({ model: initialModel, project }: AppProps) {
	return (
		<PuxRuntimeWrapper>
			<ThemeProvider>
				<ToolRegistry />
				<PuxApp initialModel={initialModel} project={project} />
			</ThemeProvider>
		</PuxRuntimeWrapper>
	);
}

function PuxApp({ initialModel, project }: { initialModel: string; project: string }) {
	const { exit } = useApp();
	const { rows, cols } = useTerminalSize();
	// Sync local model state with the Zustand store's activeModel
	const storeModel = usePuxStore((s) => s.activeModel);
	const [model, setModel] = useState(initialModel || storeModel);
	const lastCtrlC = useRef(0);

	// Phase 4: Global keybindings for view cycling and quit
	useInput(useCallback((input: string, key: any) => {
		// Double Ctrl+C to quit
		if (input === "c" && key.ctrl) {
			const now = Date.now();
			if (now - lastCtrlC.current < 1000) {
				exit();
			}
			lastCtrlC.current = now;
			return;
		}
		// Ctrl+T: cycle views
		if (input === "t" && key.ctrl) {
			usePuxStore.getState().cycleTuiView();
			return;
		}
	}, [exit]));

	// Command handler
	const handleCommand = useCallback(async (input: string): Promise<string | null> => {
		const ctx: CommandContext = { model, project, exit, setModel };
		const result = await executeCommand(input, ctx);
		if (result.type === "handled") return result.message ?? null;
		return null;
	}, [model, project, exit]);

	return (
		<Box flexDirection="column" height={rows} width={cols}>
			{/* Content area — switches based on active view */}
			<Box flexGrow={1} flexDirection="column">
				<ContentArea onCommand={handleCommand} />
			</Box>

			{/* Status bar */}
			<StatusBar />
		</Box>
	);
}

// ── Content Area ──

function ContentArea({ onCommand }: { onCommand: (input: string) => Promise<string | null> }) {
	const activeView = usePuxStore((s) => s.activeTuiView);
	const pendingDecision = usePuxStore((s) => s.pendingDecision);
	const showProviders = usePuxStore((s) => s.showProvidersOverlay);
	const showSettings = usePuxStore((s) => s.showSettingsOverlay);
	const showSwitcher = usePuxStore((s) => s.showSessionSwitcher);
	const showLogs = usePuxStore((s) => s.showLogViewer);
	const showFilePicker = usePuxStore((s) => s.showFilePicker);
	const showSearch = usePuxStore((s) => s.showSearchOverlay);
	const showHelp = usePuxStore((s) => s.showHelpOverlay);
	const showMCP = usePuxStore((s) => s.showMCPOverlay);

	// HITL decision dialog takes priority over everything
	if (pendingDecision) {
		return (
			<Box
				flexDirection="column"
				justifyContent="flex-end"
				paddingX={1}
				flexGrow={1}
			>
				{pendingDecision.hint === "question" ? (
					<QuestionDialog />
				) : (
					<DecisionDialog />
				)}
			</Box>
		);
	}

	// Search overlay
	if (showSearch) {
		return <SearchOverlay />;
	}

	// MCP server overlay
	if (showMCP) {
		return <MCPOverlay />;
	}

	// Help overlay
	if (showHelp) {
		return <HelpOverlay />;
	}

	// File picker
	if (showFilePicker) {
		return <FilePicker />;
	}

	// Log viewer
	if (showLogs) {
		return <LogViewer />;
	}

	// Session switcher overlay
	if (showSwitcher) {
		return <SessionSwitcher />;
	}

	// Settings overlay
	if (showSettings) {
		return <SettingsOverlay />;
	}

	// Providers overlay takes priority over views
	if (showProviders) {
		return <ProvidersOverlay />;
	}

	// View router
	switch (activeView) {
		case "agents":
			return <AgentsView />;
		case "tools":
			return <ToolsView />;
		case "files":
			return <FilesView />;
		case "conversations":
			return <ConversationsView />;
		case "chat":
		default:
			return <Thread onCommand={onCommand} />;
	}
}
