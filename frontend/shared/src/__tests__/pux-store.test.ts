import { describe, it, expect, beforeEach, vi } from "vitest";
import { usePuxStore } from "../pux-store";
import type {
	AgentState,
	ToolCallRecord,
	PendingDecision,
	Conversation,
	Project,
	WorkbenchTab,
	TuiView,
} from "../types";
import { makeAgent, makeToolCall } from "./helpers";

// ── Mock fetch-provider so API calls never hit a real server ──
// (vi.mock factories are hoisted above imports, so the factory must be inline.)
vi.mock("../fetch-provider", () => {
	const mockFetch = vi.fn().mockResolvedValue({
		ok: true,
		json: () => Promise.resolve([]),
	});
	return { getFetch: () => mockFetch, setFetch: vi.fn() };
});

// Reset store between tests
beforeEach(() => {
	usePuxStore.setState({
		pendingDecision: null,
		lastUsage: null,
		contextMetrics: null,
		compacting: false,
		activeProject: "",
		activeProjectPath: "",
		activeAgentId: "",
		activeModel: "",
		activeConversationId: "",
		conversationKey: "",
		modelList: [],
		defaultLogic: "",
		defaultWorker: "",
		showModelPicker: false,
		providers: {},
		showProvidersOverlay: false,
		showSettingsOverlay: false,
		showSessionSwitcher: false,
		showLogViewer: false,
		showSearchOverlay: false,
		showMCPOverlay: false,
		mcpServers: [],
		theme: "default",
		conversations: [],
		projects: [],
		activeWorkbenchTab: "vnc",
		agents: new Map(),
		activeTuiView: "chat",
		runningAgents: new Map(),
		viewedConversations: new Set(),
		lastError: null,
		activePlan: null,
	});
});

// ── Helpers ──
// makeAgent / makeToolCall are imported from ./helpers (shared across store tests).

// ══════════════════════════════════════════
// 1. Initial State
// ══════════════════════════════════════════

describe("initial state", () => {
	it("has empty string defaults for project/agent/model fields", () => {
		const s = usePuxStore.getState();
		expect(s.activeProject).toBe("");
		expect(s.activeProjectPath).toBe("");
		expect(s.activeAgentId).toBe("");
		expect(s.activeModel).toBe("");
		expect(s.activeConversationId).toBe("");
		expect(s.conversationKey).toBe("");
	});

	it("has null for optional state", () => {
		const s = usePuxStore.getState();
		expect(s.pendingDecision).toBeNull();
		expect(s.lastUsage).toBeNull();
		expect(s.contextMetrics).toBeNull();
		expect(s.lastError).toBeNull();
		expect(s.activePlan).toBeNull();
	});

	it("has empty arrays and maps for collections", () => {
		const s = usePuxStore.getState();
		expect(s.modelList).toEqual([]);
		expect(s.conversations).toEqual([]);
		expect(s.projects).toEqual([]);
		expect(s.mcpServers).toEqual([]);
		expect(s.agents.size).toBe(0);
		expect(s.runningAgents.size).toBe(0);
		expect(s.viewedConversations.size).toBe(0);
	});

	it("has correct defaults for enums/booleans", () => {
		const s = usePuxStore.getState();
		expect(s.compacting).toBe(false);
		expect(s.activeWorkbenchTab).toBe("vnc");
		expect(s.activeTuiView).toBe("chat");
		expect(s.theme).toBe("default");
		expect(s.defaultLogic).toBe("");
		expect(s.defaultWorker).toBe("");
	});

	it("has all overlays closed by default", () => {
		const s = usePuxStore.getState();
		expect(s.showModelPicker).toBe(false);
		expect(s.showProvidersOverlay).toBe(false);
		expect(s.showSettingsOverlay).toBe(false);
		expect(s.showSessionSwitcher).toBe(false);
		expect(s.showLogViewer).toBe(false);
		expect(s.showSearchOverlay).toBe(false);
		expect(s.showMCPOverlay).toBe(false);
	});
});

// ══════════════════════════════════════════
// 2. Sync Actions (pure state setters)
// ══════════════════════════════════════════

describe("setProject", () => {
	it("sets activeProject and resolves path from projects list", () => {
		const projects: Project[] = [
			{ name: "alpha", path: "/tmp/alpha" },
			{ name: "beta", path: "/tmp/beta" },
		];
		usePuxStore.setState({ projects });
		usePuxStore.getState().setProject("beta");

		const s = usePuxStore.getState();
		expect(s.activeProject).toBe("beta");
		expect(s.activeProjectPath).toBe("/tmp/beta");
	});

	it("sets empty path when project not found in list", () => {
		usePuxStore.setState({ projects: [{ name: "alpha", path: "/tmp/alpha" }] });
		usePuxStore.getState().setProject("unknown");

		const s = usePuxStore.getState();
		expect(s.activeProject).toBe("unknown");
		expect(s.activeProjectPath).toBe("");
	});
});

describe("setModel", () => {
	it("sets activeModel and closes model picker", () => {
		usePuxStore.setState({ showModelPicker: true });
		usePuxStore.getState().setModel("gpt-4");

		const s = usePuxStore.getState();
		expect(s.activeModel).toBe("gpt-4");
		expect(s.showModelPicker).toBe(false);
	});
});

describe("setConversation", () => {
	it("sets active conversation fields and marks viewed", () => {
		usePuxStore.setState({
			projects: [{ name: "proj", path: "/p" }],
		});
		usePuxStore.getState().setConversation("proj", "agent-42");

		const s = usePuxStore.getState();
		expect(s.activeProject).toBe("proj");
		expect(s.activeProjectPath).toBe("/p");
		expect(s.activeAgentId).toBe("agent-42");
		expect(s.conversationKey).toBe("proj:agent-42");
		expect(s.viewedConversations.has("proj:agent-42")).toBe(true);
	});

	it("uses 'default' in conversationKey when agentId is empty", () => {
		usePuxStore.getState().setConversation("proj", "");
		expect(usePuxStore.getState().conversationKey).toBe("proj:default");
	});
});

describe("clearConversation", () => {
	it("resets conversation fields and clears metrics", () => {
		usePuxStore.setState({
			activeProject: "p",
			activeAgentId: "a1",
			pendingDecision: { decisionId: "d1" } as PendingDecision,
			lastUsage: { input: 10, output: 5, cache: 0 },
			compacting: true,
			lastError: "boom",
		});
		usePuxStore.getState().clearConversation();

		const s = usePuxStore.getState();
		expect(s.activeAgentId).toBe("");
		expect(s.pendingDecision).toBeNull();
		expect(s.lastUsage).toBeNull();
		expect(s.compacting).toBe(false);
		expect(s.lastError).toBeNull();
		expect(s.conversationKey).toMatch(/^p:clear-\d+$/);
	});
});

describe("clearError", () => {
	it("clears lastError", () => {
		usePuxStore.setState({ lastError: "something broke" });
		usePuxStore.getState().clearError();
		expect(usePuxStore.getState().lastError).toBeNull();
	});
});

describe("setWorkbenchTab", () => {
	const tabs: WorkbenchTab[] = ["vnc", "editor", "scheduler", "workers", "settings"];
	for (const tab of tabs) {
		it(`sets activeWorkbenchTab to "${tab}"`, () => {
			usePuxStore.getState().setWorkbenchTab(tab);
			expect(usePuxStore.getState().activeWorkbenchTab).toBe(tab);
		});
	}
});

describe("setTuiView", () => {
	it("sets activeTuiView", () => {
		usePuxStore.getState().setTuiView("agents");
		expect(usePuxStore.getState().activeTuiView).toBe("agents");
	});
});

describe("cycleTuiView", () => {
	const views: TuiView[] = ["chat", "agents", "tools", "files", "conversations"];

	it("cycles through all views in order and wraps", () => {
		for (let i = 0; i < views.length; i++) {
			expect(usePuxStore.getState().activeTuiView).toBe(views[i % views.length]);
			usePuxStore.getState().cycleTuiView();
		}
		// After full cycle, should be back to chat
		expect(usePuxStore.getState().activeTuiView).toBe("chat");
	});
});

describe("startNewChat", () => {
	it("resets conversation fields preserving activeProject", () => {
		usePuxStore.setState({
			activeProject: "my-project",
			activeAgentId: "old-agent",
			pendingDecision: { decisionId: "x" } as PendingDecision,
			lastUsage: { input: 1, output: 2, cache: 3 },
			compacting: true,
			lastError: "err",
		});
		usePuxStore.getState().startNewChat();

		const s = usePuxStore.getState();
		expect(s.activeProject).toBe("my-project");
		expect(s.activeAgentId).toBe("");
		expect(s.pendingDecision).toBeNull();
		expect(s.lastUsage).toBeNull();
		expect(s.contextMetrics).toBeNull();
		expect(s.compacting).toBe(false);
		expect(s.lastError).toBeNull();
		expect(s.conversationKey).toMatch(/^my-project:new-\d+$/);
	});
});

describe("markViewed", () => {
	it("adds project:agentId to viewedConversations set", () => {
		usePuxStore.getState().markViewed("proj1", "a1");
		usePuxStore.getState().markViewed("proj2", "a2");

		const viewed = usePuxStore.getState().viewedConversations;
		expect(viewed.has("proj1:a1")).toBe(true);
		expect(viewed.has("proj2:a2")).toBe(true);
		expect(viewed.size).toBe(2);
	});

	it("does not duplicate entries", () => {
		usePuxStore.getState().markViewed("p", "a");
		usePuxStore.getState().markViewed("p", "a");
		expect(usePuxStore.getState().viewedConversations.size).toBe(1);
	});
});

describe("setTheme", () => {
	it("updates theme state", () => {
		usePuxStore.getState().setTheme("dark");
		expect(usePuxStore.getState().theme).toBe("dark");
	});
});

// ══════════════════════════════════════════
// 3. Agent Monitoring Actions
// ══════════════════════════════════════════

describe("addAgent", () => {
	it("adds an agent to the agents map", () => {
		const agent = makeAgent();
		usePuxStore.getState().addAgent(agent);

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(1);
		expect(agents.get("agent-1")).toEqual(agent);
	});

	it("replaces an agent with the same agentId", () => {
		usePuxStore.getState().addAgent(makeAgent({ task: "first" }));
		usePuxStore.getState().addAgent(makeAgent({ task: "second" }));

		const agents = usePuxStore.getState().agents;
		expect(agents.size).toBe(1);
		expect(agents.get("agent-1")!.task).toBe("second");
	});
});

describe("updateAgentStatus", () => {
	it("updates status to complete and sets endedAt", () => {
		const agent = makeAgent();
		usePuxStore.getState().addAgent(agent);

		const beforeUpdate = Date.now();
		usePuxStore.getState().updateAgentStatus("agent-1", "complete", "done");

		const updated = usePuxStore.getState().agents.get("agent-1")!;
		expect(updated.status).toBe("complete");
		expect(updated.result).toBe("done");
		expect(updated.endedAt).toBeGreaterThanOrEqual(beforeUpdate);
	});

	it("updates status to error and sets error field", () => {
		usePuxStore.getState().addAgent(makeAgent());
		usePuxStore.getState().updateAgentStatus("agent-1", "error", "timeout");

		const updated = usePuxStore.getState().agents.get("agent-1")!;
		expect(updated.status).toBe("error");
		expect(updated.error).toBe("timeout");
		expect(updated.endedAt).toBeDefined();
	});

	it("keeps running status without setting endedAt", () => {
		usePuxStore.getState().addAgent(makeAgent());
		usePuxStore.getState().updateAgentStatus("agent-1", "running");

		const updated = usePuxStore.getState().agents.get("agent-1")!;
		expect(updated.status).toBe("running");
		expect(updated.endedAt).toBeUndefined();
	});

	it("does nothing for non-existent agent", () => {
		usePuxStore.getState().addAgent(makeAgent());
		usePuxStore.getState().updateAgentStatus("nonexistent", "complete");

		// Original agent unchanged
		expect(usePuxStore.getState().agents.get("agent-1")!.status).toBe("running");
		expect(usePuxStore.getState().agents.size).toBe(1);
	});
});

describe("addAgentToolCall", () => {
	it("appends a tool call to the agent", () => {
		usePuxStore.getState().addAgent(makeAgent());
		const tc1 = makeToolCall({ toolName: "bash" });
		const tc2 = makeToolCall({ toolName: "read_file", timestamp: 3000 });
		usePuxStore.getState().addAgentToolCall("agent-1", tc1);
		usePuxStore.getState().addAgentToolCall("agent-1", tc2);

		const agent = usePuxStore.getState().agents.get("agent-1")!;
		expect(agent.toolCalls).toHaveLength(2);
		expect(agent.toolCalls[0].toolName).toBe("bash");
		expect(agent.toolCalls[1].toolName).toBe("read_file");
	});

	it("does nothing for non-existent agent", () => {
		usePuxStore.getState().addAgent(makeAgent());
		usePuxStore.getState().addAgentToolCall("nonexistent", makeToolCall());

		expect(usePuxStore.getState().agents.get("agent-1")!.toolCalls).toHaveLength(0);
	});
});

describe("clearAgents", () => {
	it("empties the agents map", () => {
		usePuxStore.getState().addAgent(makeAgent());
		usePuxStore.getState().addAgent(makeAgent({ agentId: "agent-2" }));
		expect(usePuxStore.getState().agents.size).toBe(2);

		usePuxStore.getState().clearAgents();
		expect(usePuxStore.getState().agents.size).toBe(0);
	});
});

// ══════════════════════════════════════════
// 4. Overlay Toggle Actions
// ══════════════════════════════════════════

describe("overlay mutual exclusion", () => {
	const overlayTogglePairs: Array<[string, () => void, () => void]> = [
		["showModelPicker", () => usePuxStore.getState().toggleModelPicker(), () => usePuxStore.getState().toggleModelPicker()],
		["showProvidersOverlay", () => usePuxStore.getState().toggleProvidersOverlay(), () => usePuxStore.getState().toggleProvidersOverlay()],
		["showSettingsOverlay", () => usePuxStore.getState().toggleSettingsOverlay(), () => usePuxStore.getState().toggleSettingsOverlay()],
		["showSessionSwitcher", () => usePuxStore.getState().toggleSessionSwitcher(), () => usePuxStore.getState().toggleSessionSwitcher()],
		["showLogViewer", () => usePuxStore.getState().toggleLogViewer(), () => usePuxStore.getState().toggleLogViewer()],
		["showSearchOverlay", () => usePuxStore.getState().toggleSearchOverlay(), () => usePuxStore.getState().toggleSearchOverlay()],
		["showMCPOverlay", () => usePuxStore.getState().toggleMCPOverlay(), () => usePuxStore.getState().toggleMCPOverlay()],
	];

	const allOverlayKeys = [
		"showModelPicker",
		"showProvidersOverlay",
		"showSettingsOverlay",
		"showSessionSwitcher",
		"showLogViewer",
		"showSearchOverlay",
		"showMCPOverlay",
	] as const;

	for (const [key, open, close] of overlayTogglePairs) {
		describe(`${key}`, () => {
			it("opens the overlay and closes all others", () => {
				// Open a different overlay first (pick one that is not the current key)
				const otherKey = key === "showModelPicker" ? "showSessionSwitcher" : "showModelPicker";
				usePuxStore.setState({ [otherKey]: true });

				open();

				const s = usePuxStore.getState();
				for (const k of allOverlayKeys) {
					expect(s[k]).toBe(k === key);
				}
			});

			it("toggles off when called again", () => {
				open(); // open
				close(); // close (toggle again)

				expect(usePuxStore.getState()[key as keyof ReturnType<typeof usePuxStore.getState>]).toBe(false);
			});
		});
	}
});

describe("close overlay actions", () => {
	it("closeProvidersOverlay closes providers overlay", () => {
		usePuxStore.setState({ showProvidersOverlay: true });
		usePuxStore.getState().closeProvidersOverlay();
		expect(usePuxStore.getState().showProvidersOverlay).toBe(false);
	});

	it("closeSettingsOverlay closes settings overlay", () => {
		usePuxStore.setState({ showSettingsOverlay: true });
		usePuxStore.getState().closeSettingsOverlay();
		expect(usePuxStore.getState().showSettingsOverlay).toBe(false);
	});

	it("closeSessionSwitcher closes session switcher", () => {
		usePuxStore.setState({ showSessionSwitcher: true });
		usePuxStore.getState().closeSessionSwitcher();
		expect(usePuxStore.getState().showSessionSwitcher).toBe(false);
	});

	it("closeLogViewer closes log viewer", () => {
		usePuxStore.setState({ showLogViewer: true });
		usePuxStore.getState().closeLogViewer();
		expect(usePuxStore.getState().showLogViewer).toBe(false);
	});

	it("closeSearchOverlay closes search overlay", () => {
		usePuxStore.setState({ showSearchOverlay: true });
		usePuxStore.getState().closeSearchOverlay();
		expect(usePuxStore.getState().showSearchOverlay).toBe(false);
	});

	it("closeMCPOverlay closes MCP overlay", () => {
		usePuxStore.setState({ showMCPOverlay: true });
		usePuxStore.getState().closeMCPOverlay();
		expect(usePuxStore.getState().showMCPOverlay).toBe(false);
	});
});

// ══════════════════════════════════════════
// 5. Async Actions (mocked fetch)
// ══════════════════════════════════════════

describe("loadModels", () => {
	it("sets modelList from API response (array)", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve([{ id: "m1", name: "Model One", provider: "openai" }]),
		} as any);

		await usePuxStore.getState().loadModels();

		const models = usePuxStore.getState().modelList;
		expect(models).toHaveLength(1);
		expect(models[0]).toEqual({ id: "m1", name: "Model One", provider: "openai", contextWindow: 0 });
	});

	it("sets modelList from API response (object with .models)", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve({ models: [{ id: "m2", name: "M2" }] }),
		} as any);

		await usePuxStore.getState().loadModels();
		expect(usePuxStore.getState().modelList).toEqual([{ id: "m2", name: "M2", provider: "", contextWindow: 0 }]);
	});

	it("does not update state when API fails", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({ ok: false } as any);
		usePuxStore.setState({ modelList: [{ id: "existing", name: "E", provider: "" }] });

		await usePuxStore.getState().loadModels();
		expect(usePuxStore.getState().modelList).toEqual([{ id: "existing", name: "E", provider: "" }]);
	});
});

describe("loadConversations", () => {
	it("sets conversations from API response", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		const convs: Conversation[] = [
			{ project: "p1", agentId: "a1", lastMessage: "hi", lastAt: "2024-01-01", messageCount: 5, title: "Chat" },
		];
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve(convs),
		} as any);

		await usePuxStore.getState().loadConversations();
		expect(usePuxStore.getState().conversations).toEqual(convs);
	});
});

describe("loadProjects", () => {
	it("sets projects from API response and auto-selects first when none active", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve([{ name: "proj1", path: "/p1" }]),
		} as any);

		await usePuxStore.getState().loadProjects();

		expect(usePuxStore.getState().projects).toEqual([{ name: "proj1", path: "/p1" }]);
		expect(usePuxStore.getState().activeProject).toBe("proj1");
		expect(usePuxStore.getState().activeProjectPath).toBe("/p1");
	});

	it("does not auto-select when a project is already active", async () => {
		usePuxStore.setState({ activeProject: "existing" });
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve([{ name: "new-proj", path: "/new" }]),
		} as any);

		await usePuxStore.getState().loadProjects();

		expect(usePuxStore.getState().activeProject).toBe("existing");
	});
});

describe("deleteConversation", () => {
	it("removes conversation from state and resets key if it was active", async () => {
		const convs: Conversation[] = [
			{ project: "p1", agentId: "a1", lastMessage: "hi", lastAt: "", messageCount: 1, title: "T" },
			{ project: "p1", agentId: "a2", lastMessage: "yo", lastAt: "", messageCount: 2, title: "T2" },
		];
		usePuxStore.setState({
			conversations: convs,
			activeProject: "p1",
			activeAgentId: "a1",
		});

		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({ ok: true } as any);

		await usePuxStore.getState().deleteConversation("p1", "a1");

		expect(usePuxStore.getState().conversations).toHaveLength(1);
		expect(usePuxStore.getState().conversations[0].agentId).toBe("a2");
		expect(usePuxStore.getState().conversationKey).toBe("default");
	});

	it("does not reset key when deleting a non-active conversation", async () => {
		const convs: Conversation[] = [
			{ project: "p1", agentId: "a1", lastMessage: "hi", lastAt: "", messageCount: 1, title: "T" },
			{ project: "p1", agentId: "a2", lastMessage: "yo", lastAt: "", messageCount: 2, title: "T2" },
		];
		usePuxStore.setState({
			conversations: convs,
			activeProject: "p1",
			activeAgentId: "a1",
			conversationKey: "p1:a1",
		});

		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({ ok: true } as any);

		await usePuxStore.getState().deleteConversation("p1", "a2");

		expect(usePuxStore.getState().conversations).toHaveLength(1);
		expect(usePuxStore.getState().conversationKey).toBe("p1:a1");
	});
});

describe("removeProject", () => {
	it("removes project from state and clears active if it was selected", async () => {
		const projects: Project[] = [
			{ name: "proj1", path: "/p1" },
			{ name: "proj2", path: "/p2" },
		];
		usePuxStore.setState({ projects, activeProject: "proj1" });

		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({ ok: true } as any);

		await usePuxStore.getState().removeProject("proj1");

		expect(usePuxStore.getState().projects).toHaveLength(1);
		expect(usePuxStore.getState().projects[0].name).toBe("proj2");
		expect(usePuxStore.getState().activeProject).toBe("");
	});
});

describe("respondToDecision", () => {
	it("does nothing when no pending decision exists", async () => {
		usePuxStore.setState({ pendingDecision: null });
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockClear();

		await usePuxStore.getState().respondToDecision("approve", "yes");

		expect(mockFetch).not.toHaveBeenCalled();
	});

	it("posts decision and clears pendingDecision", async () => {
		const decision: PendingDecision = {
			decisionId: "dec-1",
			sourceTool: "approval",
			title: "Approve?",
			description: "Please approve",
			hint: "approval",
		};
		usePuxStore.setState({ pendingDecision: decision });

		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({ ok: true } as any);

		await usePuxStore.getState().respondToDecision("approve", "yes");

		expect(mockFetch).toHaveBeenCalledWith(
			"/api/pux/decision",
			expect.objectContaining({ method: "POST" }),
		);
		expect(usePuxStore.getState().pendingDecision).toBeNull();
	});
});

describe("selectModel", () => {
	it("sets activeModel and closes all overlays", async () => {
		usePuxStore.setState({ showModelPicker: true, showSettingsOverlay: true });

		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({ ok: true } as any);

		await usePuxStore.getState().selectModel("openai", "gpt-4");

		const s = usePuxStore.getState();
		expect(s.activeModel).toBe("gpt-4");
		expect(s.showModelPicker).toBe(false);
		expect(s.showSettingsOverlay).toBe(false);
	});
});

describe("setDefaults", () => {
	it("sets defaultLogic and defaultWorker in state", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({ ok: true } as any);

		await usePuxStore.getState().setDefaults("gpt-4", "gpt-3.5");

		expect(usePuxStore.getState().defaultLogic).toBe("gpt-4");
		expect(usePuxStore.getState().defaultWorker).toBe("gpt-3.5");
	});
});

describe("loadDefaults", () => {
	it("sets defaults from API response", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve({ logic: "llama3", worker: "phi3" }),
		} as any);

		await usePuxStore.getState().loadDefaults();

		expect(usePuxStore.getState().defaultLogic).toBe("llama3");
		expect(usePuxStore.getState().defaultWorker).toBe("phi3");
	});
});

describe("loadProviders", () => {
	it("sets providers from API response", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		const providers = {
			openai: { baseUrl: "https://api.openai.com", api: "openai", status: "available" },
		};
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve({ providers }),
		} as any);

		await usePuxStore.getState().loadProviders();
		expect(usePuxStore.getState().providers).toEqual(providers);
	});
});

describe("loadMCPServers", () => {
	it("sets mcpServers from API response", async () => {
		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		const servers = [{ prefix: "web", endpoint: "http://localhost:8327", available: true, toolCount: 5, tools: [] }];
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () => Promise.resolve(servers),
		} as any);

		await usePuxStore.getState().loadMCPServers();
		expect(usePuxStore.getState().mcpServers).toEqual(servers);
	});
});

// ══════════════════════════════════════════
// 6. State Transitions (multi-step)
// ══════════════════════════════════════════

describe("agent lifecycle state transitions", () => {
	it("tracks a full agent lifecycle: add -> tool calls -> complete", () => {
		const store = usePuxStore.getState();

		// 1. Add agent
		store.addAgent(makeAgent({ agentId: "lifecycle-agent", task: "build feature" }));
		let agent = usePuxStore.getState().agents.get("lifecycle-agent")!;
		expect(agent.status).toBe("running");
		expect(agent.toolCalls).toHaveLength(0);

		// 2. Add tool calls
		usePuxStore.getState().addAgentToolCall("lifecycle-agent", makeToolCall({ toolName: "write_file" }));
		usePuxStore.getState().addAgentToolCall("lifecycle-agent", makeToolCall({ toolName: "bash", args: { command: "npm test" } }));
		agent = usePuxStore.getState().agents.get("lifecycle-agent")!;
		expect(agent.toolCalls).toHaveLength(2);

		// 3. Mark complete
		usePuxStore.getState().updateAgentStatus("lifecycle-agent", "complete", "all tests pass");
		agent = usePuxStore.getState().agents.get("lifecycle-agent")!;
		expect(agent.status).toBe("complete");
		expect(agent.result).toBe("all tests pass");
		expect(agent.endedAt).toBeDefined();
		expect(agent.toolCalls).toHaveLength(2); // tool calls preserved
	});

	it("tracks error lifecycle", () => {
		usePuxStore.getState().addAgent(makeAgent({ agentId: "err-agent" }));
		usePuxStore.getState().addAgentToolCall("err-agent", makeToolCall({ toolName: "bash", isError: true, result: "exit code 1" }));
		usePuxStore.getState().updateAgentStatus("err-agent", "error", "command failed");

		const agent = usePuxStore.getState().agents.get("err-agent")!;
		expect(agent.status).toBe("error");
		expect(agent.error).toBe("command failed");
		expect(agent.endedAt).toBeDefined();
	});
});

describe("multiple agents simultaneously", () => {
	it("tracks multiple agents independently", () => {
		usePuxStore.getState().addAgent(makeAgent({ agentId: "a1", task: "task 1" }));
		usePuxStore.getState().addAgent(makeAgent({ agentId: "a2", task: "task 2" }));

		usePuxStore.getState().addAgentToolCall("a1", makeToolCall({ toolName: "bash" }));
		usePuxStore.getState().updateAgentStatus("a1", "complete", "done");

		// a2 unaffected
		const a2 = usePuxStore.getState().agents.get("a2")!;
		expect(a2.status).toBe("running");
		expect(a2.toolCalls).toHaveLength(0);

		// a1 updated
		const a1 = usePuxStore.getState().agents.get("a1")!;
		expect(a1.status).toBe("complete");
		expect(a1.toolCalls).toHaveLength(1);
	});
});

describe("viewedConversations with loadConversations auto-mark", () => {
	it("auto-marks active conversation as viewed after loading", async () => {
		usePuxStore.setState({
			activeProject: "p1",
			activeAgentId: "a1",
			viewedConversations: new Set(),
		});

		const { getFetch } = await import("../fetch-provider");
		const mockFetch = getFetch();
		vi.mocked(mockFetch).mockResolvedValueOnce({
			ok: true,
			json: () =>
				Promise.resolve([
					{ project: "p1", agentId: "a1", lastMessage: "hi", lastAt: "2024-01-01", messageCount: 1, title: "Chat" },
				]),
		} as any);

		await usePuxStore.getState().loadConversations();

		expect(usePuxStore.getState().viewedConversations.has("p1:a1")).toBe(true);
	});
});
