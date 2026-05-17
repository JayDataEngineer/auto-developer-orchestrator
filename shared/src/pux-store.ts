/**
 * Pux Zustand Store — non-message state shared between web and TUI.
 *
 * Message/tool/subagent state is managed by @assistant-ui runtime
 * via PuxChatAdapter. This store handles HITL dialogs, metrics, models,
 * and other Pux-specific state that assistant-ui doesn't cover.
 */

import { create } from "zustand";
import { getFetch } from "./fetch-provider";
import { apiUrl } from "./server-url";
import type {
	TokenUsage,
	ContextMetrics,
	PendingDecision,
	DecisionHint,
	Conversation,
	Project,
	WorkbenchTab,
	AgentState,
	ToolCallRecord,
	TuiView,
	ProvidersMap,
} from "./types";

// ── Safe localStorage access ──

const storage = {
	get(key: string, fallback: string = ""): string {
		try {
			return typeof localStorage !== "undefined" ? localStorage.getItem(key) || fallback : fallback;
		} catch { return fallback; }
	},
	set(key: string, value: string): void {
		try { if (typeof localStorage !== "undefined") localStorage.setItem(key, value); } catch {}
	},
	getJSON<T>(key: string, fallback: T): T {
		try {
			if (typeof localStorage === "undefined") return fallback;
			const raw = localStorage.getItem(key);
			return raw ? JSON.parse(raw) as T : fallback;
		} catch { return fallback; }
	},
	setJSON(key: string, value: unknown): void {
		storage.set(key, JSON.stringify(value));
	},
};

// ── State ──

export interface RunningAgentInfo {
	project: string;
	agentId: string;
	startedAt: number;
	lastEventAt: number;
}

export interface MCPServerInfo {
	prefix: string;
	endpoint: string;
	available: boolean;
	toolCount: number;
	tools: string[];
}

interface PuxState {
	// HITL (unified decision protocol)
	pendingDecision: PendingDecision | null;

	// Metrics (Contract 2.2 agent_end)
	lastUsage: TokenUsage | null;
	contextMetrics: ContextMetrics | null;

	// Compaction (Contract 2.5)
	compacting: boolean;

	// Conversation
	activeProject: string;
	activeProjectPath: string;
	activeAgentId: string;
	activeConversationId: string;
	conversationKey: string;

	// Model
	activeModel: string;
	modelList: Array<{ id: string; name: string; provider: string }>;

	// Conversations
	conversations: Conversation[];

	// Projects
	projects: Project[];

	// Workbench (web only — auto-driven by SSE tool events)
	activeWorkbenchTab: WorkbenchTab;

	// Agent monitoring (TUI + web)
	agents: Map<string, AgentState>;
	activeTuiView: TuiView;

	// Multi-conversation tracking
	runningAgents: Map<string, RunningAgentInfo>;
	viewedConversations: Set<string>;

	// Model picker overlay
	showModelPicker: boolean;

	// Providers overlay (fullscreen browser)
	providers: ProvidersMap;
	showProvidersOverlay: boolean;

	// Settings overlay
	showSettingsOverlay: boolean;

	// Session switcher
	showSessionSwitcher: boolean;

	// Log viewer
	showLogViewer: boolean;

	// File picker
	showFilePicker: boolean;

	// Search overlay
	showSearchOverlay: boolean;

	// MCP server overlay
	showMCPOverlay: boolean;
	mcpServers: MCPServerInfo[];

	// Theme
	theme: string;

	// Error
	lastError: string | null;

	// Active plan (Contract 2.7)
	activePlan: { planId: string; name: string; filePath: string } | null;

	// ── Actions ──
	respondToDecision: (action: string, value: string) => Promise<void>;
	loadModels: () => Promise<void>;
	toggleModelPicker: () => void;
	loadConversations: () => Promise<void>;
	loadProjects: () => Promise<void>;
	setModel: (model: string) => void;
	setProject: (project: string) => void;
	setConversation: (project: string, agentId: string) => void;
	deleteConversation: (project: string, agentId: string) => Promise<void>;
	clearConversation: () => void;
	clearError: () => void;
	setWorkbenchTab: (tab: WorkbenchTab) => void;
	setTuiView: (view: TuiView) => void;
	cycleTuiView: () => void;
	addAgent: (agent: AgentState) => void;
	updateAgentStatus: (agentId: string, status: AgentState["status"], result?: string) => void;
	addAgentToolCall: (agentId: string, toolCall: ToolCallRecord) => void;
	clearAgents: () => void;
	startNewChat: () => void;
	markViewed: (project: string, agentId: string) => void;
	updateRunningAgents: () => Promise<void>;
	loadProviders: () => Promise<void>;
	toggleProvidersOverlay: () => void;
	closeProvidersOverlay: () => void;
	selectModel: (provider: string, modelId: string) => Promise<void>;
	addProvider: (provider: { id: string; baseUrl: string; apiKey: string; models: Array<{ id: string; name: string; contextWindow: number; maxTokens: number }> }) => Promise<void>;
	toggleSettingsOverlay: () => void;
	closeSettingsOverlay: () => void;
	setTheme: (theme: string) => void;
	toggleSessionSwitcher: () => void;
	closeSessionSwitcher: () => void;
	toggleLogViewer: () => void;
	closeLogViewer: () => void;
	toggleFilePicker: () => void;
	closeFilePicker: () => void;
	toggleSearchOverlay: () => void;
	closeSearchOverlay: () => void;
	toggleMCPOverlay: () => void;
	closeMCPOverlay: () => void;
	loadMCPServers: () => Promise<void>;
	addMCPServer: (prefix: string, endpoint: string) => Promise<void>;
	removeMCPServer: (prefix: string) => Promise<void>;
}

// ── Overlay helpers ──

const overlayKeys = [
	"showModelPicker",
	"showProvidersOverlay",
	"showSettingsOverlay",
	"showSessionSwitcher",
	"showLogViewer",
	"showFilePicker",
	"showSearchOverlay",
	"showMCPOverlay",
] as const;

function closeAllOverlays(): Partial<PuxState> {
	const reset: Record<string, boolean> = {};
	for (const k of overlayKeys) reset[k] = false;
	return reset as unknown as Partial<PuxState>;
}

function openOverlay(key: (typeof overlayKeys)[number], extra?: Partial<PuxState>): Partial<PuxState> {
	return { ...closeAllOverlays(), [key]: true, ...extra };
}

// ── API loader helper ──

async function apiLoad<T>(path: string, transform: (data: unknown) => Partial<PuxState> | null): Promise<Partial<PuxState> | null> {
	try {
		const fetch = getFetch();
		const resp = await fetch(apiUrl(path));
		if (!resp.ok) return null;
		const data = await resp.json();
		return transform(data);
	} catch {
		return null;
	}
}

// ── Store ──

export const usePuxStore = create<PuxState>((set, get) => ({
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
	showModelPicker: false,
	providers: {},
	showProvidersOverlay: false,
	showSettingsOverlay: false,
	showSessionSwitcher: false,
	showLogViewer: false,
	showFilePicker: false,
	showSearchOverlay: false,
	showMCPOverlay: false,
	mcpServers: [],
	theme: storage.get("pux:theme", "default"),
	conversations: [],
	projects: [],
	activeWorkbenchTab: "vnc",
	agents: new Map(),
	activeTuiView: "chat",
	runningAgents: new Map(),
	viewedConversations: new Set(storage.getJSON<string[]>("pux:viewedConversations", [])),
	lastError: null,
	activePlan: null,

	respondToDecision: async (action, value) => {
		const { pendingDecision } = get();
		if (!pendingDecision) return;
		const fetch = getFetch();
		await fetch(apiUrl("/api/pux/decision"), {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				decisionId: pendingDecision.decisionId,
				action,
				value: value || "",
			}),
		});
		set({ pendingDecision: null });
	},

	loadModels: async () => {
		const update = await apiLoad("/api/pux/models", (data: unknown) => {
			const raw = Array.isArray(data) ? data : (data as Record<string, unknown>)?.models || [];
			const models = (raw as Record<string, string>[]).map((m) => ({
				id: m.id || m.name,
				name: m.name || m.id,
				provider: m.provider || "",
			}));
			return { modelList: models };
		});
		if (update) set(update);
	},

	setModel: (model) => set({ activeModel: model, showModelPicker: false }),

	toggleModelPicker: () => {
		const show = !get().showModelPicker;
		if (show) get().loadModels();
		set(show ? openOverlay("showModelPicker") : { showModelPicker: false });
	},

	setProject: (project) => {
		const projects = get().projects as Array<{ name: string; path?: string }>;
		const p = projects.find((pr) => pr.name === project);
		set({ activeProject: project, activeProjectPath: p?.path || "" });
	},

	setConversation: (project, agentId) => {
		const projects = get().projects as Array<{ name: string; path?: string }>;
		const p = projects.find((pr) => pr.name === project);
		get().markViewed(project, agentId);
		set({
			activeProject: project,
			activeProjectPath: p?.path || "",
			activeAgentId: agentId || "",
			conversationKey: `${project}:${agentId || "default"}`,
		});
	},

	clearConversation: () =>
		set({
			activeAgentId: "",
			conversationKey: `${get().activeProject}:default`,
			pendingDecision: null,
			lastUsage: null,
			contextMetrics: null,
			compacting: false,
			lastError: null,
		}),

	loadConversations: async () => {
		const update = await apiLoad("/api/pux/conversations", (data: unknown) => {
			const convs = Array.isArray(data) ? data as Conversation[] : [];
			const current = get().conversations;
			if (convs.length === current.length && convs.every((c, i) =>
				c.project === current[i].project && c.agentId === current[i].agentId && c.lastAt === current[i].lastAt && c.messageCount === current[i].messageCount
			)) return null;
			return { conversations: convs };
		});
		if (update) set(update);
	},

	deleteConversation: async (project, agentId) => {
		try {
			const fetch = getFetch();
			const resp = await fetch(
				apiUrl(`/api/pux/conversation?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`),
				{ method: "DELETE" },
			);
			if (!resp.ok) return;
			// Remove from local state
			const { activeProject, activeAgentId } = get();
			set((state) => ({
				conversations: state.conversations.filter(
					(c) => !(c.project === project && c.agentId === agentId),
				),
			}));
			// If we deleted the active conversation, clear it
			if (activeProject === project && activeAgentId === agentId) {
				set({ conversationKey: `default` });
			}
		} catch {
			// ignore
		}
	},

	loadProjects: async () => {
		const update = await apiLoad("/api/projects", (data: unknown) => {
			const projects = Array.isArray(data) ? data : ((data as Record<string, unknown>)?.projects || []) as Project[];
			return { projects };
		});
		if (update) {
			set(update);
			// Auto-select first project if none active
			if (!get().activeProject && (update.projects as Project[]).length > 0) {
				const first = (update.projects as Project[])[0];
				set({ activeProject: first.name || first.path, activeProjectPath: first.path || "" });
			}
		}
	},

	clearError: () => set({ lastError: null }),

	setWorkbenchTab: (tab) => set({ activeWorkbenchTab: tab }),

	setTuiView: (view) => set({ activeTuiView: view }),

	cycleTuiView: () => {
		const views: TuiView[] = ["chat", "agents", "tools", "files", "conversations"];
		const current = get().activeTuiView;
		const idx = views.indexOf(current);
		set({ activeTuiView: views[(idx + 1) % views.length] });
	},

	addAgent: (agent) => {
		const agents = new Map(get().agents);
		agents.set(agent.agentId, agent);
		set({ agents });
	},

	updateAgentStatus: (agentId, status, result) => {
		const agents = new Map(get().agents);
		const existing = agents.get(agentId);
		if (existing) {
			agents.set(agentId, {
				...existing,
				status,
				endedAt: status !== "running" ? Date.now() : undefined,
				result: result || existing.result,
				error: status === "error" ? result : existing.error,
			});
			set({ agents });
		}
	},

	addAgentToolCall: (agentId, toolCall) => {
		const agents = new Map(get().agents);
		const existing = agents.get(agentId);
		if (existing) {
			agents.set(agentId, {
				...existing,
				toolCalls: [...existing.toolCalls, toolCall],
			});
			set({ agents });
		}
	},

	clearAgents: () => set({ agents: new Map() }),

	startNewChat: () => {
		const { activeProject } = get();
		set({
			activeAgentId: "",
			conversationKey: `${activeProject}:new-${Date.now()}`,
			pendingDecision: null,
			lastUsage: null,
			contextMetrics: null,
			compacting: false,
			lastError: null,
		});
	},

	markViewed: (project, agentId) => {
		const key = `${project}:${agentId}`;
		const viewed = new Set(get().viewedConversations);
		viewed.add(key);
		storage.setJSON("pux:viewedConversations", [...viewed]);
		set({ viewedConversations: viewed });
	},

	updateRunningAgents: async () => {
		const update = await apiLoad("/api/pux/agent-status", (data: unknown) => {
			const entries = Array.isArray(data) ? data : [];
			const current = get().runningAgents;
			// Quick size check — if same count and same keys/values, skip
			if (entries.length === current.size) {
				let same = true;
				for (const entry of entries as Array<Record<string, string>>) {
					const key = `${entry.project}:${entry.agentId}`;
					const existing = current.get(key);
					if (!existing || existing.lastEventAt !== (entry.lastEventAt ? new Date(entry.lastEventAt).getTime() : 0)) {
						same = false;
						break;
					}
				}
				if (same) return null;
			}
			const map = new Map<string, RunningAgentInfo>();
			for (const entry of entries as Array<Record<string, string>>) {
				const key = `${entry.project}:${entry.agentId}`;
				map.set(key, {
					project: entry.project,
					agentId: entry.agentId,
					startedAt: entry.startedAt ? new Date(entry.startedAt).getTime() : 0,
					lastEventAt: entry.lastEventAt ? new Date(entry.lastEventAt).getTime() : 0,
				});
			}
			return { runningAgents: map };
		});
		if (update) set(update);
	},

	loadProviders: async () => {
		const update = await apiLoad("/api/pux/providers", (data: unknown) => ({
			providers: ((data as Record<string, unknown>)?.providers || {}) as ProvidersMap,
		}));
		if (update) set(update);
	},

	toggleProvidersOverlay: () => {
		const show = !get().showProvidersOverlay;
		if (show) {
			get().loadProviders();
			get().loadModels();
		}
		set(show ? openOverlay("showProvidersOverlay") : { showProvidersOverlay: false });
	},

	closeProvidersOverlay: () => set({ showProvidersOverlay: false }),

	selectModel: async (provider, modelId) => {
		const store = get();
		try {
			const fetch = getFetch();
			await fetch(apiUrl("/api/pux/model"), {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					project: store.activeProject,
					provider,
					modelId,
					agentId: store.activeAgentId || "default",
				}),
			});
		} catch {
			// fire and forget
		}
		set({
			activeModel: modelId,
			...closeAllOverlays(),
		});
	},

		addProvider: async (provider) => {
		try {
			const fetch = getFetch();
			const resp = await fetch(apiUrl("/api/pux/providers"), {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(provider),
			});
			if (resp.ok) {
				await get().loadProviders();
			}
		} catch {
			// ignore
		}
	},

	toggleSessionSwitcher: () => {
		const show = !get().showSessionSwitcher;
		set(show ? openOverlay("showSessionSwitcher") : { showSessionSwitcher: false });
	},

	closeSessionSwitcher: () => set({ showSessionSwitcher: false }),

	toggleLogViewer: () => {
		const show = !get().showLogViewer;
		set(show ? openOverlay("showLogViewer") : { showLogViewer: false });
	},

	closeLogViewer: () => set({ showLogViewer: false }),

	toggleFilePicker: () => {
		const show = !get().showFilePicker;
		set(show ? openOverlay("showFilePicker") : { showFilePicker: false });
	},

	closeFilePicker: () => set({ showFilePicker: false }),

	toggleSearchOverlay: () => {
		const show = !get().showSearchOverlay;
		set(show ? openOverlay("showSearchOverlay") : { showSearchOverlay: false });
	},

	closeSearchOverlay: () => set({ showSearchOverlay: false }),

	toggleMCPOverlay: () => {
		const show = !get().showMCPOverlay;
		if (show) get().loadMCPServers();
		set(show ? openOverlay("showMCPOverlay") : { showMCPOverlay: false });
	},

	closeMCPOverlay: () => set({ showMCPOverlay: false }),

	loadMCPServers: async () => {
		const update = await apiLoad("/api/pux/mcp-servers", (data: unknown) => ({
			mcpServers: Array.isArray(data) ? data : [],
		}));
		if (update) set(update);
	},

	addMCPServer: async (prefix, endpoint) => {
		try {
			const fetch = getFetch();
			await fetch(apiUrl("/api/pux/mcp-servers"), {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ prefix, endpoint }),
			});
			await get().loadMCPServers();
		} catch {
			// ignore
		}
	},

	removeMCPServer: async (prefix) => {
		try {
			const fetch = getFetch();
			await fetch(apiUrl("/api/pux/mcp-servers"), {
				method: "DELETE",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ prefix }),
			});
			await get().loadMCPServers();
		} catch {
			// ignore
		}
	},

	toggleSettingsOverlay: () => {
		const show = !get().showSettingsOverlay;
		if (show) {
			get().loadProviders();
			get().loadModels();
		}
		set(show ? openOverlay("showSettingsOverlay") : { showSettingsOverlay: false });
	},

	closeSettingsOverlay: () => set({ showSettingsOverlay: false }),

	setTheme: (theme) => {
		storage.set("pux:theme", theme);
		set({ theme });
	},
}));

// Re-export types for convenience
export type { TokenUsage, ContextMetrics, PendingDecision, DecisionHint, Conversation, Project, WorkbenchTab, AgentState, ToolCallRecord, TuiView, ModelCost, ModelInfo, ProviderInfo, ProvidersMap } from "./types";
// RunningAgentInfo is defined in this file, just re-export it directly
