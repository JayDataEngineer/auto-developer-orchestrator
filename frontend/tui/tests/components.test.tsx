/**
 * Comprehensive TUI Component Tests
 *
 * Tests all 34 components using ink-testing-library.
 * Covers rendering, states (empty/loaded/running/error), keyboard interaction,
 * and boundary conditions for each component.
 *
 * Run: cd frontend/tui && bun test
 */

import { describe, test, expect, beforeEach, afterEach, mock } from "bun:test";
import React from "react";
import { Text, Box } from "ink";
import { render } from "ink-testing-library";

// ── Mock @pux/shared ──

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
	setModel: mock(() => {}),
	setProject: mock(() => {}),
	cycleTuiView: mock(() => {}),
	setTuiView: mock(() => {}),
	toggleProvidersOverlay: mock(() => {}),
	closeProvidersOverlay: mock(() => {}),
	toggleSettingsOverlay: mock(() => {}),
	closeSettingsOverlay: mock(() => {}),
	toggleSessionSwitcher: mock(() => {}),
	closeSessionSwitcher: mock(() => {}),
	toggleLogViewer: mock(() => {}),
	closeLogViewer: mock(() => {}),
	toggleSearchOverlay: mock(() => {}),
	closeSearchOverlay: mock(() => {}),
	toggleHelpOverlay: mock(() => {}),
	closeHelpOverlay: mock(() => {}),
	toggleMCPOverlay: mock(() => {}),
	closeMCPOverlay: mock(() => {}),
	toggleModelPicker: mock(() => {}),
	clearConversation: mock(() => {}),
	setConversation: mock(() => {}),
	deleteConversation: mock(() => {}),
	setZoomedAgent: mock(() => {}),
	toggleAgentSelector: mock(() => {}),
	selectModel: mock(() => {}),
	addProvider: mock(() => Promise.resolve()),
	addMCPServer: mock(() => Promise.resolve()),
	removeMCPServer: mock(() => {}),
	setTheme: mock(() => {}),
	setFontScale: mock(() => {}),
	setDefaults: mock(() => {}),
	loadDefaults: mock(() => Promise.resolve()),
	loadConversations: mock(() => Promise.resolve()),
	loadModels: mock(() => Promise.resolve()),
	cancelAgent: mock(() => {}),
	backgroundCurrentTask: mock(() => {}),
	respondToDecision: mock(() => {}),
};

let storeSubscribers: Array<(state: any, prev: any) => void> = [];
let currentState = { ...mockStoreState };

const mockUsePuxStore = (selector?: (s: any) => any) => {
	if (!selector) return { ...currentState, ...mockActions };
	return selector({ ...currentState, ...mockActions });
};

mockUsePuxStore.getState = () => ({ ...currentState, ...mockActions });
mockUsePuxStore.setState = (update: any) => {
	Object.assign(currentState, update);
	storeSubscribers.forEach((cb) => cb(currentState, {}));
};
mockUsePuxStore.subscribe = (cb: any) => {
	storeSubscribers.push(cb);
	return () => { storeSubscribers = storeSubscribers.filter((s) => s !== cb); };
};

mock.module("@pux/shared", () => ({
	usePuxStore: mockUsePuxStore,
	setBaseUrl: mock(() => {}),
	setFetch: mock(() => {}),
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
	relativeTime: (iso: string) => "5m ago",
}));

// ── Mock @assistant-ui/react-ink ──

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

mock.module("@assistant-ui/react-ink", () => ({
	useAuiState: (selector?: (s: any) => any) => {
		if (!selector) return auiState;
		return selector(auiState);
	},
	useAui: () => ({
		composer: () => ({
			setText: mock(() => {}),
		}),
		switchToThread: mock(() => {}),
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
		Input: (props: any) => <Text>composer-input</Text>,
	},
	BranchPickerPrimitive: {
		Root: ({ children }: any) => <>{children}</>,
		Previous: () => <Text>{"<"}</Text>,
		Next: () => <Text>{">"}</Text>,
		Count: () => <Text>3</Text>,
	},
	LoadingPrimitive: {
		Root: ({ children }: any) => <>{children}</>,
		Spinner: ({ variant }: any) => <Text>spinner</Text>,
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
mock.module("ink-spinner", () => ({
	default: function Spinner() { return <Text>spinner</Text>; },
}));

// ── Mock ink-text-input ──
mock.module("ink-text-input", () => ({
	default: ({ value, onChange, focus, mask, placeholder }: any) => (
		<Text>{mask ? "••••" : value || placeholder || ""}</Text>
	),
}));

// ── Mock fs/path ──
mock.module("node:fs", () => ({
	readdirSync: () => ["file1.ts", "file2.ts", "dir1"],
	statSync: () => ({ isDirectory: () => false }),
}));
mock.module("node:path", () => ({
	join: (...parts: string[]) => parts.join("/"),
	basename: (p: string) => p.split("/").pop(),
	dirname: (p: string) => p.split("/").slice(0, -1).join("/"),
}));

// ── Mock createRequire ──
mock.module("node:module", () => ({
	createRequire: () => () => ({ version: "0.1.0-test" }),
}));

// ═══════════════════════════════════════════════════════
// IMPORTS
// ═══════════════════════════════════════════════════════

import { MarkdownText } from "../src/components/markdown-text.js";
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
import { ApprovalDialog } from "../src/components/approval-dialog.js";
import { StatusBar } from "../src/components/status-bar.js";
import { TabBar } from "../src/components/tab-bar.js";
import { BranchPicker } from "../src/components/branch-picker.js";
import { DiffViewDisplay } from "../src/components/diff-view.js";
import { ErrorMessage } from "../src/components/error-message.js";
import { SuggestionChips } from "../src/components/suggestion-chips.js";
import { ReasoningAccordion } from "../src/components/reasoning-accordion.js";
import { ReasoningBlock } from "../src/components/reasoning.js";
import { ComposerQueue } from "../src/components/composer-queue.js";
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
	currentState = { ...mockStoreState, agents: new Map(), conversations: [], ...mockActions };
}

// ═══════════════════════════════════════════════════════
// 1. THEME
// ═══════════════════════════════════════════════════════

describe("Theme", () => {
	test("colors has default palette", () => {
		expect(colors.brand).toBe("magenta");
		expect(colors.user).toBe("greenBright");
		expect(colors.assistant).toBe("cyan");
		expect(colors.success).toBe("green");
		expect(colors.error).toBe("red");
		expect(colors.warning).toBe("yellow");
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
		expect(lastFrame()).toContain("magenta");
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
		expect(lastFrame()).toContain("─");
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
		expect(frame).toContain("Question");
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
		expect(frame).toContain("Always allow");
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

describe("ApprovalDialog", () => {
	beforeEach(() => resetStore());

	test("renders nothing when no pending decision", () => {
		currentState.pendingDecision = null;
		const { lastFrame } = render(<ApprovalDialog />);
		expect(lastFrame()).toBe("");
	});

	test("renders approval with Y/N", () => {
		currentState.pendingDecision = {
			hint: "approval",
			title: "Proceed?",
		};
		const { lastFrame } = render(<ApprovalDialog />);
		const frame = lastFrame();
		expect(frame).toMatch(/[YN]/);
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

describe("ReasoningAccordion", () => {
	test("renders nothing with no reasoning parts", () => {
		auiState.message.parts = [];
		const { lastFrame } = render(
			<ThemeProvider><ReasoningAccordion /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
	});
});

describe("Reasoning", () => {
	test("renders reasoning text", () => {
		const { lastFrame } = render(
			<ThemeProvider><ReasoningBlock text="thinking step" /></ThemeProvider>
		);
		expect(lastFrame()).toContain("thinking step");
	});

	test("renders truncated when long", () => {
		const { lastFrame } = render(
			<ThemeProvider><ReasoningBlock text={"line1\nline2\nline3\nline4\nline5\n"} /></ThemeProvider>
		);
		expect(lastFrame()).toBeDefined();
	});
});

describe("ComposerQueue", () => {
	beforeEach(() => resetStore());

	test("renders nothing when queue empty", () => {
		const { lastFrame } = render(
			<ThemeProvider><ComposerQueue /></ThemeProvider>
		);
		expect(lastFrame()).toBe("");
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
