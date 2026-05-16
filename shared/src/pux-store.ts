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

// ── State ──

export interface RunningAgentInfo {
	project: string;
	agentId: string;
	startedAt: number;
	lastEventAt: number;
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

	// Theme
	theme: string;

	// Error
	lastError: string | null;

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
	theme: (() => {
		try {
			return typeof localStorage !== "undefined" ? localStorage.getItem("pux:theme") || "default" : "default";
		} catch { return "default"; }
	})(),
	conversations: [],
	projects: [],
	activeWorkbenchTab: "vnc",
	agents: new Map(),
	activeTuiView: "chat",
	runningAgents: new Map(),
	viewedConversations: (() => {
		try {
			const saved = typeof localStorage !== "undefined" ? localStorage.getItem("pux:viewedConversations") : null;
			return saved ? new Set(JSON.parse(saved) as string[]) : new Set<string>();
		} catch { return new Set<string>(); }
	})(),
	lastError: null,

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
		try {
			const fetch = getFetch();
			const resp = await fetch(apiUrl("/api/pux/models"));
			if (!resp.ok) return;
			const data = await resp.json();
			const models = Array.isArray(data) ? data : data.models || [];
			set({
				modelList: models.map((m: Record<string, string>) => ({
					id: m.id || m.name,
					name: m.name || m.id,
					provider: m.provider || "",
				})),
			});
		} catch {
			// ignore
		}
	},

	setModel: (model) => set({ activeModel: model, showModelPicker: false }),

	toggleModelPicker: () => {
		const show = !get().showModelPicker;
		if (show) get().loadModels();
		set({ showModelPicker: show });
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
		try {
			const fetch = getFetch();
			const resp = await fetch(apiUrl("/api/pux/conversations"));
			if (!resp.ok) return;
			const data = await resp.json();
			set({ conversations: Array.isArray(data) ? data : [] });
		} catch {
			// ignore
		}
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
		try {
			const fetch = getFetch();
			const resp = await fetch(apiUrl("/api/projects"));
			if (!resp.ok) return;
			const data = await resp.json();
			const projects = Array.isArray(data) ? data : (data.projects || []);
			set({ projects });
			// Auto-select first project if none active
			if (!get().activeProject && projects.length > 0) {
				const first = projects[0];
				set({ activeProject: first.name || first.path, activeProjectPath: first.path || "" });
			}
		} catch {
			// ignore
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
		try { localStorage.setItem("pux:viewedConversations", JSON.stringify([...viewed])); } catch {}
		set({ viewedConversations: viewed });
	},

	updateRunningAgents: async () => {
		try {
			const fetch = getFetch();
			const resp = await fetch(apiUrl("/api/pux/agent-status"));
			if (!resp.ok) return;
			const data = await resp.json();
			const map = new Map<string, RunningAgentInfo>();
			const entries = Array.isArray(data) ? data : [];
			for (const entry of entries) {
				const key = `${entry.project}:${entry.agentId}`;
				map.set(key, {
					project: entry.project,
					agentId: entry.agentId,
					startedAt: entry.startedAt ? new Date(entry.startedAt).getTime() : 0,
					lastEventAt: entry.lastEventAt ? new Date(entry.lastEventAt).getTime() : 0,
				});
			}
			set({ runningAgents: map });
		} catch {
			// ignore
		}
	},

	loadProviders: async () => {
		try {
			const fetch = getFetch();
			const resp = await fetch(apiUrl("/api/pux/providers"));
			if (!resp.ok) return;
			const data = await resp.json();
			set({ providers: data.providers || {} });
		} catch {
			// ignore
		}
	},

	toggleProvidersOverlay: () => {
		const show = !get().showProvidersOverlay;
		if (show) {
			get().loadProviders();
			get().loadModels();
		}
		set({ showProvidersOverlay: show, showModelPicker: false });
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
			showProvidersOverlay: false,
			showModelPicker: false,
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
		set({ showSessionSwitcher: show, showModelPicker: false, showProvidersOverlay: false, showSettingsOverlay: false });
	},

	closeSessionSwitcher: () => set({ showSessionSwitcher: false }),

	toggleLogViewer: () => {
		const show = !get().showLogViewer;
		set({ showLogViewer: show, showModelPicker: false, showProvidersOverlay: false, showSettingsOverlay: false, showSessionSwitcher: false });
	},

	closeLogViewer: () => set({ showLogViewer: false }),

	toggleFilePicker: () => {
		const show = !get().showFilePicker;
		set({ showFilePicker: show, showModelPicker: false, showProvidersOverlay: false, showSettingsOverlay: false, showSessionSwitcher: false, showLogViewer: false });
	},

	closeFilePicker: () => set({ showFilePicker: false }),

	toggleSettingsOverlay: () => {
		const show = !get().showSettingsOverlay;
		if (show) {
			get().loadProviders();
			get().loadModels();
		}
		set({ showSettingsOverlay: show, showProvidersOverlay: false });
	},

	closeSettingsOverlay: () => set({ showSettingsOverlay: false }),

	setTheme: (theme) => {
		try { localStorage.setItem("pux:theme", theme); } catch {}
		set({ theme });
	},
}));

// Re-export types for convenience
export type { TokenUsage, ContextMetrics, PendingDecision, DecisionHint, Conversation, Project, WorkbenchTab, AgentState, ToolCallRecord, TuiView, ModelCost, ModelInfo, ProviderInfo, ProvidersMap } from "./types";
// RunningAgentInfo is defined in this file, just re-export it directly
