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
	Conversation,
	Project,
	WorkbenchTab,
	AgentState,
	ToolCallRecord,
	TuiView,
} from "./types";

// ── State ──

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

	// Error
	lastError: string | null;

	// ── Actions ──
	respondToDecision: (action: string, value: string) => Promise<void>;
	loadModels: () => Promise<void>;
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
	conversations: [],
	projects: [],
	activeWorkbenchTab: "vnc",
	agents: new Map(),
	activeTuiView: "chat",
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

	setModel: (model) => set({ activeModel: model }),

	setProject: (project) => {
		const projects = get().projects as Array<{ name: string; path?: string }>;
		const p = projects.find((pr) => pr.name === project);
		set({ activeProject: project, activeProjectPath: p?.path || "" });
	},

	setConversation: (project, agentId) => {
		const projects = get().projects as Array<{ name: string; path?: string }>;
		const p = projects.find((pr) => pr.name === project);
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
			pendingQuestion: null,
			pendingApproval: null,
			pendingPlan: null,
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
}));

// Re-export types for convenience
export type { TokenUsage, ContextMetrics, PendingQuestion, PendingApproval, PendingPlan, Conversation, Project, WorkbenchTab, AgentState, ToolCallRecord, TuiView };
