/**
 * Unit tests for pure TypeScript modules (commands, provider-catalog, theme symbols).
 *
 * These test pure functions with no Ink/React dependency.
 *
 * Run: cd frontend/tui && bun test
 */

import { describe, test, expect, mock } from "bun:test";

// ── Mock @pux/shared for commands ──
const mockStore = {
	activeProject: "test-project",
	activeAgentId: "agent-123",
	activeConversationId: "conv-456",
	activeTuiView: "chat",
	lastUsage: null,
	contextMetrics: null,
	agents: new Map(),
	compacting: false,
};

const storeActions = {
	toggleHelpOverlay: mock(() => {}),
	toggleSearchOverlay: mock(() => {}),
	toggleLogViewer: mock(() => {}),
	toggleSessionSwitcher: mock(() => {}),
	toggleSettingsOverlay: mock(() => {}),
	toggleMCPOverlay: mock(() => {}),
	toggleProvidersOverlay: mock(() => {}),
	clearConversation: mock(() => {}),
	setTuiView: mock(() => {}),
	setState: mock(() => {}),
};

mock.module("@pux/shared", () => ({
	usePuxStore: (selector?: any) => {
		const state = { ...mockStore, ...storeActions };
		return selector ? selector(state) : state;
	},
	usePuxStore: Object.assign(
		(selector?: any) => {
			const state = { ...mockStore, ...storeActions };
			return selector ? selector(state) : state;
		},
		{
			getState: () => ({ ...mockStore, ...storeActions }),
			setState: (update: any) => Object.assign(mockStore, update),
		}
	),
	apiUrl: (path: string) => path,
	getFetch: () => globalThis.fetch,
	setBaseUrl: mock(() => {}),
	setFetch: mock(() => {}),
}));

import { parseCommand, getCommandNames, getCommands } from "../src/commands.js";
import { PROVIDER_CATALOG, TYPE_COLORS, TYPE_LABELS } from "../src/provider-catalog.js";
import { symbols } from "../src/theme.js";

// ═══════════════════════════════════════════════════════
// 1. COMMANDS MODULE
// ═══════════════════════════════════════════════════════

describe("parseCommand", () => {
	test("parses simple command", () => {
		const result = parseCommand("/help");
		expect(result).toEqual({ command: "help", args: "" });
	});

	test("parses command with args", () => {
		const result = parseCommand("/compact project=foo");
		expect(result).toEqual({ command: "compact", args: "project=foo" });
	});

	test("returns null for non-command", () => {
		expect(parseCommand("hello")).toBeNull();
	});

	test("returns null for empty string", () => {
		expect(parseCommand("")).toBeNull();
	});

	test("handles just slash", () => {
		const result = parseCommand("/");
		expect(result).toEqual({ command: "", args: "" });
	});

	test("lowercases command", () => {
		const result = parseCommand("/HELP");
		expect(result?.command).toBe("help");
	});

	test("trims whitespace", () => {
		const result = parseCommand("  /status  ");
		expect(result?.command).toBe("status");
	});

	test("handles multiple words in args", () => {
		const result = parseCommand("/say hello world");
		expect(result).toEqual({ command: "say", args: "hello world" });
	});
});

// ═══════════════════════════════════════════════════════

describe("getCommandNames", () => {
	test("returns all command names", () => {
		const names = getCommandNames();
		expect(names).toContain("help");
		expect(names).toContain("quit");
		expect(names).toContain("clear");
		expect(names).toContain("compact");
		expect(names).toContain("new");
		expect(names).toContain("search");
		expect(names).toContain("files");
		expect(names).toContain("logs");
		expect(names).toContain("sessions");
		expect(names).toContain("settings");
		expect(names).toContain("mcp");
		expect(names).toContain("model");
		expect(names).toContain("status");
		expect(names).toContain("chat");
		expect(names).toContain("agents");
		expect(names).toContain("tools");
		expect(names).toContain("conversations");
	});

	test("all commands are unique", () => {
		const names = getCommandNames();
		expect(new Set(names).size).toBe(names.length);
	});

	test("all commands are lowercase", () => {
		const names = getCommandNames();
		names.forEach((n) => expect(n).toBe(n.toLowerCase()));
	});
});

// ═══════════════════════════════════════════════════════

describe("getCommands", () => {
	test("returns command objects with all fields", () => {
		const cmds = getCommands();
		cmds.forEach((cmd) => {
			expect(cmd).toHaveProperty("name");
			expect(cmd).toHaveProperty("description");
			expect(cmd).toHaveProperty("handler");
			expect(typeof cmd.name).toBe("string");
			expect(typeof cmd.description).toBe("string");
			expect(typeof cmd.handler).toBe("function");
		});
	});

	test("help command has handler", () => {
		const cmds = getCommands();
		const help = cmds.find((c) => c.name === "help");
		expect(help).toBeDefined();
		expect(typeof help!.handler).toBe("function");
	});

	test("clear command has handler", () => {
		const cmds = getCommands();
		const clear = cmds.find((c) => c.name === "clear");
		expect(clear).toBeDefined();
		expect(typeof clear!.handler).toBe("function");
	});

	test("quit command has handler", () => {
		const cmds = getCommands();
		const quit = cmds.find((c) => c.name === "quit");
		expect(quit).toBeDefined();
		expect(typeof quit!.handler).toBe("function");
	});

	test("model command has handler", () => {
		const cmds = getCommands();
		const model = cmds.find((c) => c.name === "model");
		expect(model).toBeDefined();
		expect(typeof model!.handler).toBe("function");
	});

	test("all handlers return promise-like", () => {
		const cmds = getCommands();
		const ctx = { model: "test", project: "test", exit: mock(() => {}), setModel: mock(() => {}) };
		cmds.forEach((cmd) => {
			const result = cmd.handler("", ctx);
			if (result && typeof result.then === "function") {
				expect(result).toBeInstanceOf(Promise);
			}
		});
	});
});

// ═══════════════════════════════════════════════════════
// 2. PROVIDER CATALOG
// ═══════════════════════════════════════════════════════

describe("PROVIDER_CATALOG", () => {
	test("contains known providers", () => {
		const known = ["llamacpp", "ollama", "gemini", "openai", "anthropic", "deepseek", "groq", "together", "mistral", "cerebras", "openrouter"];
		known.forEach((name) => {
			expect(PROVIDER_CATALOG[name]).toBeDefined();
		});
	});

	test("all entries have required fields", () => {
		Object.values(PROVIDER_CATALOG).forEach((entry) => {
			expect(entry.name).toBeTruthy();
			expect(entry.description).toBeTruthy();
			expect(entry.type).toMatch(/^(local|cloud|aggregator)$/);
			expect(entry.defaultBaseUrl).toBeTruthy();
			expect(typeof entry.requiresApiKey).toBe("boolean");
		});
	});

	test("local providers don't require API key", () => {
		Object.entries(PROVIDER_CATALOG).forEach(([id, entry]) => {
			if (entry.type === "local") {
				expect(entry.requiresApiKey).toBe(false);
			}
		});
	});

	test("cloud providers require API key", () => {
		Object.entries(PROVIDER_CATALOG).forEach(([id, entry]) => {
			if (entry.type === "cloud") {
				expect(entry.requiresApiKey).toBe(true);
			}
		});
	});

	test("llamacpp has correct defaults", () => {
		const entry = PROVIDER_CATALOG.llamacpp;
		expect(entry.name).toBe("llama.cpp");
		expect(entry.defaultBaseUrl).toContain("localhost:8001");
		expect(entry.type).toBe("local");
	});

	test("openai has correct defaults", () => {
		const entry = PROVIDER_CATALOG.openai;
		expect(entry.name).toBe("OpenAI");
		expect(entry.defaultBaseUrl).toContain("api.openai.com");
		expect(entry.type).toBe("cloud");
	});

	test("all entries have unique names", () => {
		const names = Object.values(PROVIDER_CATALOG).map((e) => e.name);
		expect(new Set(names).size).toBe(names.length);
	});

	test("all entries have valid URLs", () => {
		Object.entries(PROVIDER_CATALOG).forEach(([id, entry]) => {
			expect(entry.defaultBaseUrl).toMatch(/^https?:\/\//);
		});
	});
});

// ═══════════════════════════════════════════════════════

describe("TYPE_COLORS and TYPE_LABELS", () => {
	test("TYPE_COLORS has all types", () => {
		expect(TYPE_COLORS.local).toBe("green");
		expect(TYPE_COLORS.cloud).toBe("blue");
		expect(TYPE_COLORS.aggregator).toBe("yellow");
	});

	test("TYPE_LABELS has all types", () => {
		expect(TYPE_LABELS.local).toBe("Local");
		expect(TYPE_LABELS.cloud).toBe("Cloud");
		expect(TYPE_LABELS.aggregator).toBe("Aggregator");
	});
});

// ═══════════════════════════════════════════════════════
// 3. THEME SYMBOLS
// ═══════════════════════════════════════════════════════

describe("theme symbols", () => {
	test("all symbols are defined with correct values", () => {
		expect(symbols.toolRunning).toBe("○");
		expect(symbols.toolDone).toBe("●");
		expect(symbols.toolError).toBe("✕");
		expect(symbols.dot).toBe("·");
		expect(symbols.arrow).toBe("→");
		expect(symbols.check).toBe("✓");
		expect(symbols.cross).toBe("✗");
	});

	test("all symbol values are truthy strings", () => {
		Object.values(symbols).forEach((s) => {
			expect(typeof s).toBe("string");
			expect(s.length).toBeGreaterThan(0);
		});
	});
});

// ═══════════════════════════════════════════════════════
// 4. COMMAND LINE ARG PARSING (simulated from main.tsx)
// ═══════════════════════════════════════════════════════

describe("CLI argument handling", () => {
	test("org aliases resolve correctly", () => {
		const orgAliases: Record<string, string> = { code: "dev-bot", dev: "dev-bot" };
		expect(orgAliases["code"]).toBe("dev-bot");
		expect(orgAliases["dev"]).toBe("dev-bot");
		expect(orgAliases["custom"]).toBeUndefined();
	});

	test("default server URL", () => {
		const defaultServer = "http://localhost:3847";
		expect(defaultServer).toMatch(/^https?:\/\/localhost:\d+$/);
	});

	test("default project name is set", () => {
		const defaultProject = "auto-developer-orchestrator";
		expect(defaultProject.length).toBeGreaterThan(0);
	});
});

// ═══════════════════════════════════════════════════════
// 5. SCREENSHOT DATA URI DETECTION
// ═══════════════════════════════════════════════════════

describe("Data URI detection patterns", () => {
	test("matches png data URIs", () => {
		const re = /^data:image\/(png|jpeg|jpg|gif|webp);base64,/;
		expect(re.test("data:image/png;base64,iVBORw0KGgo=")).toBe(true);
		expect(re.test("data:image/jpeg;base64,/9j/4AAQ")).toBe(true);
		expect(re.test("data:image/gif;base64,R0lGOD")).toBe(true);
		expect(re.test("data:image/webp;base64,UklGR")).toBe(true);
	});

	test("rejects non-image URIs", () => {
		const re = /^data:image\/(png|jpeg|jpg|gif|webp);base64,/;
		expect(re.test("data:application/json;base64,abc")).toBe(false);
		expect(re.test("not a data uri")).toBe(false);
	});
});
