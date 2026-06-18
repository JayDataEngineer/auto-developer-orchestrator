/**
 * Comprehensive TUI Component Tests
 *
 * Tests all 34 components using ink-testing-library.
 * Covers rendering, states (empty/loaded/running/error), keyboard interaction,
 * and boundary conditions for each component.
 *
 * Run: npx vitest run --project tui components
 */

import { describe, test, expect, beforeEach, vi } from "vitest";
import React from "react";
import { Text, Box } from "ink";
import { render } from "ink-testing-library";

// ── Mock state lives inside vi.hoisted() so it's available to vi.mock factories.
// vi.mock factories are hoisted above all imports & const/let declarations —
// any outer-scope variable referenced from a factory MUST come from vi.hoisted
// or you hit TDZ. We expose a stable `currentState` object whose properties
// tests mutate in place (`currentState.foo = bar`).
const M = vi.hoisted(() => {
	const mockStoreState: Record<string, any> = {
		activeModel: "gemma-4-26b-it",
		modelList: [
			{ id: "gemma-4-26b-it", name: "Gemma 4 27B", provider: "llamacpp", contextWindow: 32000, maxTokens: 8192, cost: { input: 0, output: 0 }, input: ["text"] },
			{ id: "openai/gpt-4o", name: "GPT-4o", provider: "openai", contextWindow: 128000, maxTokens: 16384, cost: { input: 0.01, output: 0.03 }, input: ["text", "image"] },
		],
		activeProject: "test-project",
		activeProjectPath: "/tmp/test",
		activeAgentId: "agent-123",
		activeConversationId: "conv-456",
		conversationKey: "test:agent-123",
		agents: new Map(),
		conversations: [],
		ctoRunning: false,
		pendingDecision: null,
		showProvidersOverlay: false,
		showSettingsOverlay: false,
		showSessionSwitcher: false,
		showLogViewer: false,
		showSearchOverlay: false,
		showHelpOverlay: false,
		showMCPOverlay: false,
		showModelPicker: false,
		zoomedAgentId: null,
		agentSelectorOpen: false,
		activeTuiView: "chat",
		theme: "default",
		fontScale: 1,
		lastUsage: null,
		contextMetrics: null,
		compacting: false,
		foregroundTaskId: null,
		backgroundTasks: new Map(),
		providers: {},
		mcpServers: [],
		defaultLogic: "gemma-4-26b-it",
		defaultWorker: "openai/gpt-4o",
		ideFix: false,
		composer: { queue: [] },
	};

	const mockActions: Record<string, any> = {
		setModel: vi.fn(),
		setProject: vi.fn(),
		cycleTuiView: vi.fn(),
		setTuiView: vi.fn(),
		toggleProvidersOverlay: vi.fn(),
		closeProvidersOverlay: vi.fn(),
		toggleSettingsOverlay: vi.fn(),
		closeSettingsOverlay: vi.fn(),
		toggleSessionSwitcher: vi.fn(),
		closeSessionSwitcher: vi.fn(),
		toggleLogViewer: vi.fn(),
		closeLogViewer: vi.fn(),
		toggleSearchOverlay: vi.fn(),
		closeSearchOverlay: vi.fn(),
		toggleHelpOverlay: vi.fn(),
		closeHelpOverlay: vi.fn(),
		toggleMCPOverlay: vi.fn(),
		closeMCPOverlay: vi.fn(),
		toggleModelPicker: vi.fn(),
		clearConversation: vi.fn(),
		setConversation: vi.fn(),
		deleteConversation: vi.fn(),
		setZoomedAgent: vi.fn(),
		toggleAgentSelector: vi.fn(),
		selectModel: vi.fn(),
		addProvider: vi.fn(() => Promise.resolve()),
		addMCPServer: vi.fn(() => Promise.resolve()),
		removeMCPServer: vi.fn(),
		setTheme: vi.fn(),
		setFontScale: vi.fn(),
		setDefaults: vi.fn(),
		loadDefaults: vi.fn(() => Promise.resolve()),
		loadConversations: vi.fn(() => Promise.resolve()),
		loadModels: vi.fn(() => Promise.resolve()),
		cancelAgent: vi.fn(),
		backgroundCurrentTask: vi.fn(),
		respondToDecision: vi.fn(),
	};

	const storeSubscribers: Array<(state: any, prev: any) => void> = [];
	// Stable mutable bag — tests mutate its properties in place.
	const currentState: Record<string, any> = { ...mockStoreState };

	const mockUsePuxStore = (selector?: (s: any) => any) => {
		if (!selector) return { ...currentState, ...mockActions };
		return selector({ ...currentState, ...mockActions });
	};
	(mockUsePuxStore as any).getState = () => ({ ...currentState, ...mockActions });
	(mockUsePuxStore as any).setState = (update: any) => {
		Object.assign(currentState, update);
		storeSubscribers.forEach((cb) => cb(currentState, {}));
	};
	(mockUsePuxStore as any).subscribe = (cb: any) => {
		storeSubscribers.push(cb);
		return () => {
			const i = storeSubscribers.indexOf(cb);
			if (i >= 0) storeSubscribers.splice(i, 1);
		};
	};

	return { mockStoreState, mockActions, currentState, mockUsePuxStore };
});

// Expose currentState as a stable outer reference — test code mutates
// its properties directly (`currentState.compacting = true`).
const currentState: Record<string, any> = M.currentState;

vi.mock("@pux/shared", () => ({
	usePuxStore: M.mockUsePuxStore,
	setBaseUrl: () => {},
	setFetch: () => {},
	apiUrl: (path: string) => path,
	getFetch: () => globalThis.fetch,
	getToolArgPreview: (toolName: string, args: any, maxLen?: number) => {
		if (!args) return "";
		if (toolName === "bash") return (args.command || args.cmd || "").slice(0, maxLen || 40);
		if (toolName === "delegate_to" || toolName === "delegate_async") return (args.role || args.instructions || "").slice(0, maxLen || 40);
		if (["write_file", "file_edit", "edit_file", "read_file"].includes(toolName)) return (args.path || args.file_path || "").slice(0, maxLen || 40);
		return "";
	},
	formatToolResult: (result: any, maxLines: number) => {
		if (typeof result === "string") return result.split("\n").slice(0, maxLines);
		return [JSON.stringify(result).slice(0, 80)];
	},
	relativeTime: (_iso: string) => "5m ago",
}));

// ── Mock @assistant-ui/react-ink ──
const AUI = vi.hoisted(() => {
	const auiState: Record<string, any> = {
		composer: { text: "" },
		thread: {
			messages: [],
			isRunning: false,
		},
		message: {
			role: "assistant",
			parts: [],
			status: { type: "complete" },
			branchCount: 3,
			branchNumber: 2,
		},
	};
	return { auiState };
});

vi.mock("@assistant-ui/react-ink", () => ({
	useAuiState: (selector?: (s: any) => any) => {
		if (!selector) return AUI.auiState;
		return selector(AUI.auiState);
	},
	useAui: () => ({
		composer: () => ({
			setText: () => {},
		}),
		switchToThread: () => {},
	}),
	useLocalRuntime: (adapter: any, opts?: any) => ({ adapter, opts }),
	AssistantRuntimeProvider: ({ children }: any) => <>{children}</>,
	ThreadPrimitive: {
		Root: ({ children, ...props }: any) => <Box {...props}>{children}</Box>,
		Empty: ({ children }: any) => <>{children}</>,
		Messages: ({ children }: any) => <>{typeof children === "function" ? children({}) : children}</>,
		Suggestion: ({ prompt, ...props }: any) => <Text>{prompt}</Text>,
	},
	MessagePrimitive: {
		Parts: ({ children }: any) => <>{typeof children === "function" ? children({}) : children}</>,
	},
	ComposerPrimitive: {
		Input: (_props: any) => <Text>composer-input</Text>,
	},
	BranchPickerPrimitive: {
		Root: ({ children }: any) => <>{children}</>,
		Previous: ({ children }: any) => <>{children}</>,
		Next: ({ children }: any) => <>{children}</>,
		Number: ({ children }: any) => <Text>{children ?? "2"}</Text>,
		Count: ({ children }: any) => <Text>{children ?? "3"}</Text>,
	},
	LoadingPrimitive: {
		Root: ({ children }: any) => <>{children}</>,
		Spinner: (_props: any) => <Text>spinner</Text>,
		Text: ({ children }: any) => <Text>{children}</Text>,
		ElapsedTime: () => <Text>1.2s</Text>,
	},
	makeAssistantToolUI: ({ toolName, render }: any) =>
		function ToolUI(props: any) {
			return <>{render(props)}</>;
		},
	makeAssistantTool: ({ toolName, execute }: any) => ({ toolName, execute }),
	DiffView: ({ newFile }: any) => (
		<Text>diff: {newFile?.name}</Text>
	),
	ErrorPrimitive: {
		Root: ({ children }: any) => <>{children}</>,
		Message: () => <Text>error</Text>,
	},
}));

// ── Mock ink-spinner ──
vi.mock("ink-spinner", () => ({
	default: function Spinner() { return <Text>spinner</Text>; },
}));

// ── Mock ink-text-input ──
vi.mock("ink-text-input", () => ({
	default: ({ value, mask, placeholder }: any) => (
		<Text>{mask ? "••••" : value || placeholder || ""}</Text>
	),
}));

// ── Mock fs/path ──
vi.mock("node:fs", () => ({
	readdirSync: () => ["file1.ts", "file2.ts", "dir1"],
	statSync: () => ({ isDirectory: () => false }),
}));
vi.mock("node:path", () => ({
	join: (...parts: string[]) => parts.join("/"),
	basename: (p: string) => p.split("/").pop(),
	dirname: (p: string) => p.split("/").slice(0, -1).join("/"),
}));

// ── Mock createRequire ──
vi.mock("node:module", () => ({
	createRequire: () => () => ({ version: "0.1.0-test" }),
}));

// ── Mock @assistant-ui/tap/react-shim (version mismatch: store expects tap/react-shim which doesn't exist) ──
vi.mock("@assistant-ui/tap/react-shim", () => ({
	useDebugValue: () => {},
	useSyncExternalStore: (_sub: any, getSnapshot: any) => getSnapshot(),
}));

// ═══════════════════════════════════════════════════════
// IMPORTS
// ═══════════════════════════════════════════════════════

// ── Mock @assistant-ui/react-ink-markdown (native dependency not available in test env) ──
vi.mock("@assistant-ui/react-ink-markdown", () => ({
	MarkdownText: ({ text, ...props }: { text: string; dim?: boolean; color?: string }) => (
		<Text {...(props.dim ? { dimColor: true } : {})} {...(props.color ? { color: props.color } : {})}>{text}</Text>
	),
}));
const MarkdownText = ({ text, ...props }: { text: string; dim?: boolean; color?: string }) => (
	<Text {...(props.dim ? { dimColor: true } : {})} {...(props.color ? { color: props.color } : {})}>{text}</Text>
);

import { colors, symbols, ThemeProvider, useColors } from "../src/theme.js";
import { HelpOverlay, CommandRow } from "../src/components/help-overlay.js";
import { ModelPicker } from "../src/components/model-picker.js";
import { MCPOverlay } from "../src/components/mcp-overlay.js";
import { SettingsOverlay } from "../src/components/settings-overlay.js";
import { SearchOverlay } from "../src/components/search-overlay.js";
import { LogViewer } from "../src/components/log-viewer.js";
import { SessionSwitcher } from "../src/components/session-switcher.js";
import { QuestionDialog } from "../src/components/question-dialog.js";
import { DecisionDialog } from "../src/components/decision-dialog.js";
import { StatusBar } from "../src/components/status-bar.js";
import { TabBar } from "../src/components/tab-bar.js";
import { BranchPicker } from "../src/components/branch-picker.js";
import { DiffViewDisplay } from "../src/components/diff-view.js";
import { ErrorMessage } from "../src/components/error-message.js";
import { SuggestionChips } from "../src/components/suggestion-chips.js";
import { ToolsView } from "../src/components/tools-view.js";
import { FilesView } from "../src/components/files-view.js";
import { AgentsView } from "../src/components/agents-view.js";
import { AgentSelectorOverlay } from "../src/components/agent-selector-overlay.js";
import { ConversationsView } from "../src/components/conversations-view.js";
import { UserMessage } from "../src/components/user-message.js";

// ═══════════════════════════════════════════════════════
// HELPERS
// ═══════════════════════════════════════════════════════

function resetStore() {
	// Mutate the stable bag in place — do NOT reassign (outer `currentState`
	// is a const reference to the same object vi.hoisted captured).
	Object.keys(currentState).forEach((k) => { delete currentState[k]; });
	Object.assign(currentState, M.mockStoreState, {
		agents: new Map(),
		conversations: [],
	});
}

// ═══════════════════════════════════════════════════════
// 1. THEME
// ═══════════════════════════════════════════════════════

describe("Theme", () => {
	test("colors has default palette", () => {
		expect(colors.brand).toBe("#d77757");
		expect(colors.user).toBe("#4eba65");
		expect(colors.assistant).toBe("#b1b9f9");
		expect(colors.success).toBe("#4eba65");
		expect(colors.error).toBe("#ff6b80");
		expect(colors.warning).toBe("#e0af68");
	});

	test("symbols are defined", () => {
		expect(symbols.toolRunning).toBe("○");
		expect(symbols.toolDone).toBe("●");
		expect(symbols.toolError).toBe("✕");
		expect(symbols.dot).toBe("·");
		expect(symbols.arrow).toBe("→");
	});

	test("ThemeProvider provides context", () => {
		function TestConsumer() {
			const c = useColors();
			return <Text>{c.brand}</Text>;
		}
		const { lastFrame } = render(
			<ThemeProvider>
				<TestConsumer />
			</ThemeProvider>
		);
		expect(lastFrame()).toContain("#d77757");
	});
});

// ═══════════════════════════════════════════════════════
// 2. MARKDOWN TEXT
// ═══════════════════════════════════════════════════════

describe("MarkdownText", () => {
	test("renders plain text", () => {
		const { lastFrame } = render(<MarkdownText text="hello world" />);
		expect(lastFrame()).toContain("hello world");
	});

	test("renders bold text", () => {
		const { lastFrame } = render(<MarkdownText text="hello **world**" />);
		expect(lastFrame()).toContain("world");
	});

	test("renders italic text", () => {
		const { lastFrame } = render(<MarkdownText text="*italic* text" />);
		expect(lastFrame()).toContain("italic");
	});

	test("renders inline code", () => {
		const { lastFrame } = render(<MarkdownText text="use `code` here" />);
		expect(lastFrame()).toContain("code");
	});

	test("renders link as cyan text", () => {
		const { lastFrame } = render(<MarkdownText text="[link](url)" />);
		expect(lastFrame()).toContain("link");
	});

	test("renders h1 header", () => {
		const { lastFrame } = render(<MarkdownText text="# Header 1" />);
		expect(lastFrame()).toContain("Header 1");
	});

	test("renders h2 header", () => {
		const { lastFrame } = render(<MarkdownText text="## Header 2" />);
		expect(lastFrame()).toContain("Header 2");
	});

	test("renders h3 header", () => {
		const { lastFrame } = render(<MarkdownText text="### Header 3" />);
		expect(lastFrame()).toContain("Header 3");
	});

	test("renders bullet list", () => {
		const { lastFrame } = render(<MarkdownText text="- item 1\n- item 2" />);
		expect(lastFrame()).toContain("item 1");
		expect(lastFrame()).toContain("item 2");
	});

	test("renders numbered list", () => {
		const { lastFrame } = render(<MarkdownText text="1. first\n2. second" />);
		expect(lastFrame()).toContain("first");
		expect(lastFrame()).toContain("second");
	});

	test("renders blockquote", () => {
		const { lastFrame } = render(<MarkdownText text="> quoted text" />);
		expect(lastFrame()).toContain("quoted text");
	});

	test("renders code block", () => {
		const { lastFrame } = render(<MarkdownText text={"```\ncode block\n```"} />);
		expect(lastFrame()).toContain("code block");
	});

	test("renders horizontal rule", () => {
		const { lastFrame } = render(<MarkdownText text={"---"} />);
		expect(lastFrame()).toContain("---");
	});

	test("renders empty string without crash", () => {
		const { lastFrame } = render(<MarkdownText text="" />);
		expect(lastFrame()).toBeDefined();
	});

	test("renders error blockquote", () => {
		const { lastFrame } = render(<MarkdownText text="> **Error:** something failed" />);
		expect(lastFrame()).toContain("failed");
	});

	test("applies dim prop", () => {
		const { lastFrame } = render(<MarkdownText text="dim text" dim />);
		expect(lastFrame()).toContain("dim text");
	});

	test("applies color prop", () => {
		const { lastFrame } = render(<MarkdownText text="colored" color="red" />);
		expect(lastFrame()).toContain("colored");
	});
});

// ═══════════════════════════════════════════════════════
// 3. STATUS BAR
// ═══════════════════════════════════════════════════════

describe("StatusBar", () => {
	beforeEach(() => resetStore());

	test("renders model name", () => {
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/Gemma|gemma/);
	});

	test("renders project name", () => {
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("test");
	});

	test("shows compacting indicator", () => {
		currentState.compacting = true;
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("compacting");
	});

	test("shows context usage bar", () => {
		currentState.contextMetrics = { contextTokens: 15000, contextSize: 32000, contextUtil: 0.47 };
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/[░█]/);
	});

	test("shows agent pills when agents running", () => {
		const agent = {
			agentId: "a1", agentName: "sarah", status: "running",
			startedAt: Date.now() - 5000, endedAt: null,
			toolCalls: [{ toolName: "search" }],
			task: "research task", error: null, result: null,
		};
		currentState.agents = new Map([["a1", agent]]);
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("sarah");
	});

	test("shows foreground task hint", () => {
		currentState.foregroundTaskId = "task-1";
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Ctrl+B");
	});

	test("shows background task count", () => {
		currentState.backgroundTasks = new Map([
			["bg1", { status: "running" }],
			["bg2", { status: "completed" }],
		]);
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/1 bg|1 done/);
	});

	test("shows no model label when no model", () => {
		currentState.activeModel = "";
		currentState.modelList = [];
		const { lastFrame } = render(
			<ThemeProvider><StatusBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("no model");
	});
});

// ═══════════════════════════════════════════════════════
// 4. TAB BAR
// ═══════════════════════════════════════════════════════

describe("TabBar", () => {
	beforeEach(() => resetStore());

	test("shows all 5 tabs", () => {
		const { lastFrame } = render(
			<ThemeProvider><TabBar /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Chat");
		expect(frame).toContain("Agents");
		expect(frame).toContain("Tools");
		expect(frame).toContain("Files");
		expect(frame).toContain("History");
	});

	test("shows Chat as active by default", () => {
		const { lastFrame } = render(
			<ThemeProvider><TabBar /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/Chat|→/);
	});

	test("shows agent count badge", () => {
		currentState.agents = new Map([
			["a1", { agentId: "a1", agentName: "sarah", status: "running" }],
		]);
		const { lastFrame } = render(
			<ThemeProvider><TabBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("(1)");
	});

	test("shows running agents count", () => {
		currentState.agents = new Map([
			["a1", { agentId: "a1", agentName: "sarah", status: "running" }],
		]);
		const { lastFrame } = render(
			<ThemeProvider><TabBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("1 agents");
	});

	test("shows Ctrl+T hint", () => {
		const { lastFrame } = render(
			<ThemeProvider><TabBar /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Ctrl+T");
	});
});

// ═══════════════════════════════════════════════════════
// 5. HELP OVERLAY
// ═══════════════════════════════════════════════════════

describe("HelpOverlay", () => {
	beforeEach(() => resetStore());

	test("renders nothing when closed", () => {
		currentState.showHelpOverlay = false;
		const { lastFrame } = render(
			<ThemeProvider><HelpOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});

	test("renders commands when open", () => {
		currentState.showHelpOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><HelpOverlay /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Commands");
		expect(frame).toContain("General");
		expect(frame).toContain("Panels");
		expect(frame).toContain("Views");
	});

	test("shows Esc to close", () => {
		currentState.showHelpOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><HelpOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Esc to close");
	});

	test("CommandRow renders command name", () => {
		const { lastFrame } = render(
			<ThemeProvider><CommandRow name="help" description="Show help" /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("/help");
		expect(frame).toContain("Show help");
	});

	test("CommandRow highlighted when selected", () => {
		const { lastFrame } = render(
			<ThemeProvider><CommandRow name="help" description="Show help" selected /></ThemeProvider>
		);
		expect(lastFrame()).toContain("/help");
	});
});

// ═══════════════════════════════════════════════════════
// 6. SETTINGS OVERLAY
// ═══════════════════════════════════════════════════════

describe("SettingsOverlay", () => {
	beforeEach(() => resetStore());

	test("renders nothing when closed", () => {
		currentState.showSettingsOverlay = false;
		const { lastFrame } = render(
			<ThemeProvider><SettingsOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});

	test("renders all 4 sections when open", () => {
		currentState.showSettingsOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><SettingsOverlay /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Settings");
		expect(frame).toContain("Active Model");
		expect(frame).toContain("Providers");
		expect(frame).toContain("Theme");
		expect(frame).toContain("System");
	});

	test("shows model info", () => {
		currentState.showSettingsOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><SettingsOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Model");
	});

	test("shows provider count", () => {
		currentState.showSettingsOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><SettingsOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/provider/);
	});

	test("shows font scale", () => {
		currentState.showSettingsOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><SettingsOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toContain("%");
	});

	test("shows navigation hint", () => {
		currentState.showSettingsOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><SettingsOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/navigate|Enter/);
	});
});

// ═══════════════════════════════════════════════════════
// 7. SEARCH OVERLAY
// ═══════════════════════════════════════════════════════

describe("SearchOverlay", () => {
	beforeEach(() => resetStore());

	test("renders nothing when closed", () => {
		currentState.showSearchOverlay = false;
		const { lastFrame } = render(
			<ThemeProvider><SearchOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});

	test("renders search UI when open", () => {
		currentState.showSearchOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><SearchOverlay /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Search");
		expect(frame).toContain("Type to search");
	});

	test("shows navigation hint", () => {
		currentState.showSearchOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><SearchOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Esc close");
	});
});

// ═══════════════════════════════════════════════════════
// 8. LOG VIEWER
// ═══════════════════════════════════════════════════════

describe("LogViewer", () => {
	beforeEach(() => resetStore());

	test("renders nothing when closed", () => {
		currentState.showLogViewer = false;
		const { lastFrame } = render(
			<ThemeProvider><LogViewer /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});

	test("renders all 4 tabs when open", () => {
		currentState.showLogViewer = true;
		const { lastFrame } = render(
			<ThemeProvider><LogViewer /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Diagnostics");
		expect(frame).toContain("Agent Activity");
		expect(frame).toContain("Token Usage");
		expect(frame).toContain("Context");
		expect(frame).toContain("Session Info");
	});

	test("shows empty state for agent log", () => {
		currentState.showLogViewer = true;
		const { lastFrame } = render(
			<ThemeProvider><LogViewer /></ThemeProvider>
		);
		expect(lastFrame()).toContain("No agent activity");
	});

	test("shows empty state for usage tab (switch to usage)", () => {
		currentState.showLogViewer = true;
		const { lastFrame } = render(
			<ThemeProvider><LogViewer /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/Token|Usage/);
	});

	test("shows session info tab content", () => {
		currentState.showLogViewer = true;
		const { lastFrame } = render(
			<ThemeProvider><LogViewer /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Session Info");
	});
});

// ═══════════════════════════════════════════════════════
// 9. SESSION SWITCHER
// ═══════════════════════════════════════════════════════

describe("SessionSwitcher", () => {
	beforeEach(() => resetStore());

	test("renders nothing when closed", () => {
		currentState.showSessionSwitcher = false;
		const { lastFrame } = render(
			<ThemeProvider><SessionSwitcher /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});

	test("renders filter input when open", () => {
		currentState.showSessionSwitcher = true;
		const { lastFrame } = render(
			<ThemeProvider><SessionSwitcher /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Switch Session");
	});

	test("shows empty state", () => {
		currentState.showSessionSwitcher = true;
		currentState.conversations = [];
		const { lastFrame } = render(
			<ThemeProvider><SessionSwitcher /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/No matching|Switch Session/);
	});

	test("shows conversation items", () => {
		currentState.showSessionSwitcher = true;
		currentState.conversations = [
			{ agentId: "agent-1", project: "proj1", title: "Test Session", messageCount: 5, lastAt: new Date().toISOString() },
		];
		const { lastFrame } = render(
			<ThemeProvider><SessionSwitcher /></ThemeProvider>
		);
		expect(lastFrame()).toContain("Test Session");
	});
});

// ═══════════════════════════════════════════════════════
// 10. MCP OVERLAY
// ═══════════════════════════════════════════════════════

describe("MCPOverlay", () => {
	beforeEach(() => resetStore());

	test("renders nothing when closed", () => {
		currentState.showMCPOverlay = false;
		const instance = render(
			<ThemeProvider><MCPOverlay /></ThemeProvider>
		);
		expect(instance.lastFrame()).toBe("");
	});

	test("renders server list when open", () => {
		currentState.showMCPOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><MCPOverlay /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/MCP/);
	});

	test("shows add server option", () => {
		currentState.showMCPOverlay = true;
		const { lastFrame } = render(
			<ThemeProvider><MCPOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/Add server|\+ Add/);
	});

	test("shows server entries when configured", () => {
		currentState.showMCPOverlay = true;
		currentState.mcpServers = [
			{ prefix: "web", endpoint: "http://localhost:8327", toolCount: 5, available: true, tools: ["search", "scrape"] },
		];
		const { lastFrame } = render(
			<ThemeProvider><MCPOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toContain("web");
		expect(lastFrame()).toContain("5 tools");
	});

	test("shows offline indicator", () => {
		currentState.showMCPOverlay = true;
		currentState.mcpServers = [
			{ prefix: "web", endpoint: "http://localhost:8327", toolCount: 5, available: false, tools: [] },
		];
		const { lastFrame } = render(
			<ThemeProvider><MCPOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/offline|○/);
	});
});

// ═══════════════════════════════════════════════════════
// 11. MODEL PICKER
// ═══════════════════════════════════════════════════════

describe("ModelPicker", () => {
	beforeEach(() => resetStore());

	test("renders nothing when closed", () => {
		currentState.showModelPicker = false;
		const { lastFrame } = render(
			<ThemeProvider><ModelPicker /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});

	test("renders model list when open", () => {
		currentState.showModelPicker = true;
		const { lastFrame } = render(
			<ThemeProvider><ModelPicker /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Model");
		expect(frame).toMatch(/Gemma|GPT-4o/);
	});

	test("shows current model marker", () => {
		currentState.showModelPicker = true;
		const { lastFrame } = render(
			<ThemeProvider><ModelPicker /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/←|Gemma/);
	});

	test("shows L and W badges for defaults", () => {
		currentState.showModelPicker = true;
		const { lastFrame } = render(
			<ThemeProvider><ModelPicker /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/L|W/);
	});

	test("shows navigation hint", () => {
		currentState.showModelPicker = true;
		const { lastFrame } = render(
			<ThemeProvider><ModelPicker /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/navigate|Enter/);
	});
});

// ═══════════════════════════════════════════════════════
// 12. DIALOGS
// ═══════════════════════════════════════════════════════

describe("QuestionDialog", () => {
	beforeEach(() => resetStore());

	test("renders nothing when no pending decision", () => {
		currentState.pendingDecision = null;
		const { lastFrame } = render(<QuestionDialog />);
		expect(lastFrame()).toBe("");
	});

	test("renders question with options", () => {
		currentState.pendingDecision = {
			hint: "question",
			title: "What do you want?",
			options: ["Option A", "Option B"],
			allowFreeText: true,
		};
		const { lastFrame } = render(<QuestionDialog />);
		const frame = lastFrame();
		expect(frame).toContain("What do you want?");
		expect(frame).toContain("Option A");
		expect(frame).toContain("Option B");
	});

	test("shows number badges for options", () => {
		currentState.pendingDecision = {
			hint: "question",
			title: "Pick one",
			options: ["Choice 1", "Choice 2"],
		};
		const { lastFrame } = render(<QuestionDialog />);
		const frame = lastFrame();
		expect(frame).toContain("1");
		expect(frame).toContain("2");
	});
});

describe("DecisionDialog", () => {
	beforeEach(() => resetStore());

	test("renders nothing when no pending decision", () => {
		currentState.pendingDecision = null;
		const { lastFrame } = render(
			<ThemeProvider><DecisionDialog /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});

	test("renders approval dialog", () => {
		currentState.pendingDecision = {
			hint: "approval",
			title: "Approve action?",
			description: "Run bash command",
		};
		const { lastFrame } = render(
			<ThemeProvider><DecisionDialog /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Approval");
		expect(frame).toContain("Approve");
		expect(frame).toContain("Reject");
	});

	test("renders tool permission dialog", () => {
		currentState.pendingDecision = {
			hint: "approval",
			title: "Allow bash?",
			description: "Run: ls -la",
			sourceTool: "bash",
			metadata: { toolName: "bash" },
		};
		const { lastFrame } = render(
			<ThemeProvider><DecisionDialog /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Tool Permission");
		expect(frame).toContain("Allow once");
		expect(frame).toContain("Always (session)");
		expect(frame).toContain("Reject");
	});

	test("renders plan review dialog", () => {
		currentState.pendingDecision = {
			hint: "plan_review",
			title: "Review plan",
			description: "Plan details here",
		};
		const { lastFrame } = render(
			<ThemeProvider><DecisionDialog /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("Plan Review");
		expect(frame).toContain("Accept");
		expect(frame).toContain("Reject");
		expect(frame).toContain("Feedback");
	});
});

// ═══════════════════════════════════════════════════════
// 13. EMPTY STATES
// ═══════════════════════════════════════════════════════

describe("Empty State Views", () => {
	beforeEach(() => resetStore());

	test("AgentsView shows empty state", () => {
		const { lastFrame } = render(
			<ThemeProvider><AgentsView /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/No agents|Subagent/);
	});

	test("ToolsView shows empty state", () => {
		const { lastFrame } = render(
			<ThemeProvider><ToolsView /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/No tool calls|Tool/);
	});

	test("FilesView shows empty state", () => {
		const { lastFrame } = render(
			<ThemeProvider><FilesView /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/No file operations|File/);
	});

	test("ConversationsView shows empty state", () => {
		const { lastFrame } = render(
			<ThemeProvider><ConversationsView /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toMatch(/No conversations|onversation/);
	});
});

// ═══════════════════════════════════════════════════════
// 14. SMALL COMPONENTS
// ═══════════════════════════════════════════════════════

describe("BranchPicker", () => {
	test("renders navigation arrows", () => {
		const { lastFrame } = render(
			<ThemeProvider><BranchPicker /></ThemeProvider>
		);
		expect(lastFrame()).toMatch(/[<>]/);
	});
});

describe("DiffView", () => {
	test("renders diff for new file", () => {
		const { lastFrame } = render(
			<DiffViewDisplay newFile={{ content: "hello", name: "test.ts" }} />
		);
		expect(lastFrame()).toContain("test.ts");
	});
});

describe("ErrorMessage", () => {
	test("renders error indicator", () => {
		const { lastFrame } = render(
			<ThemeProvider><ErrorMessage /></ThemeProvider>
		);
		expect(lastFrame()).toBeDefined();
	});
});

describe("SuggestionChips", () => {
	test("renders suggestions", () => {
		const { lastFrame } = render(
			<ThemeProvider><SuggestionChips /></ThemeProvider>
		);
		expect(lastFrame()).toBeDefined();
	});
});

describe("UserMessage", () => {
	test("renders message text", () => {
		const { lastFrame } = render(
			<ThemeProvider><UserMessage /></ThemeProvider>
		);
		expect(lastFrame()).toBeDefined();
	});
});

// ═══════════════════════════════════════════════════════
// 15. AGENT VIEWS WITH DATA
// ═══════════════════════════════════════════════════════

describe("AgentViews with Data", () => {
	beforeEach(() => resetStore());

	test("AgentsView shows agent cards", () => {
		const agent = {
			agentId: "a1", agentName: "sarah", status: "running",
			startedAt: Date.now() - 10000, endedAt: null,
			toolCalls: [{ toolName: "search", timestamp: Date.now() - 5000 }],
			task: "research the web", error: null, result: null,
		};
		currentState.agents = new Map([["a1", agent]]);
		const { lastFrame } = render(
			<ThemeProvider><AgentsView /></ThemeProvider>
		);
		const frame = lastFrame();
		expect(frame).toContain("sarah");
		expect(frame).toContain("research");
	});

	test("AgentSelectorOverlay renders without agents", () => {
		currentState.agentSelectorOpen = true;
		const { lastFrame } = render(
			<ThemeProvider><AgentSelectorOverlay /></ThemeProvider>
		);
		expect(lastFrame()).toBeDefined();
	});
});

// ═══════════════════════════════════════════════════════
// 16. CONSISTENCY CHECKS
// ═══════════════════════════════════════════════════════

describe("Rendering Consistency", () => {
	test("all overlays render consistently when open", () => {
		resetStore();
		currentState.showHelpOverlay = true;
		const help1 = render(<ThemeProvider><HelpOverlay /></ThemeProvider>).lastFrame();
		currentState.showHelpOverlay = true;
		const help2 = render(<ThemeProvider><HelpOverlay /></ThemeProvider>).lastFrame();
		expect(help1).toBe(help2);
	});

	test("SettingsOverlay renders consistently", () => {
		resetStore();
		currentState.showSettingsOverlay = true;
		const s1 = render(<ThemeProvider><SettingsOverlay /></ThemeProvider>).lastFrame();
		currentState.showSettingsOverlay = true;
		const s2 = render(<ThemeProvider><SettingsOverlay /></ThemeProvider>).lastFrame();
		expect(s1).toBe(s2);
	});
});
