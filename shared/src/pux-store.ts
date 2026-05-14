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
	PendingQuestion,
	PendingApproval,
	PendingPlan,
	Conversation,
	Project,
	WorkbenchTab,
} from "./types";

// ── State ──

interface PuxState {
	// HITL (Contract 2.6 / 2.7)
	pendingQuestion: PendingQuestion | null;
	pendingApproval: PendingApproval | null;
	pendingPlan: PendingPlan | null;

	// Metrics (Contract 2.2 agent_end)
	lastUsage: TokenUsage | null;
	contextMetrics: ContextMetrics | null;

	// Compaction (Contract 2.5)
	compacting: boolean;

	// Conversation
	activeProject: string;
	activeAgentId: string;
	conversationKey: string;

	// Model
	modelList: Array<{ id: string; name: string; provider: string }>;

	// Conversations
	conversations: Conversation[];

	// Projects
	projects: Project[];

	// Workbench (web only — auto-driven by SSE tool events)
	activeWorkbenchTab: WorkbenchTab;

	// Error
	lastError: string | null;

	// ── Actions ──
	respondToQuestion: (response: string) => Promise<void>;
	respondToApproval: (approved: boolean) => Promise<void>;
	respondToPlan: (action: string, feedback?: string) => Promise<void>;
	loadModels: () => Promise<void>;
	loadConversations: () => Promise<void>;
	loadProjects: () => Promise<void>;
	setProject: (project: string) => void;
	setConversation: (project: string, agentId: string) => void;
	deleteConversation: (project: string, agentId: string) => Promise<void>;
	clearError: () => void;
	setWorkbenchTab: (tab: WorkbenchTab) => void;
}

// ── Store ──

export const usePuxStore = create<PuxState>((set, get) => ({
	pendingQuestion: null,
	pendingApproval: null,
	pendingPlan: null,
	lastUsage: null,
	contextMetrics: null,
	compacting: false,
	activeProject: "",
	activeAgentId: "",
	conversationKey: "",
	modelList: [],
	conversations: [],
	projects: [],
	activeWorkbenchTab: "vnc",
	lastError: null,

	respondToQuestion: async (response) => {
		const { pendingQuestion } = get();
		if (!pendingQuestion) return;
		const fetch = getFetch();
		await fetch(apiUrl("/api/pux/respond"), {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ requestId: pendingQuestion.questionId, response }),
		});
		set({ pendingQuestion: null });
	},

	respondToApproval: async (approved) => {
		const { pendingApproval } = get();
		if (!pendingApproval) return;
		const fetch = getFetch();
		await fetch(apiUrl("/api/pux/respond"), {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				requestId: pendingApproval.requestId,
				response: approved ? "approve" : "reject",
			}),
		});
		set({ pendingApproval: null });
	},

	respondToPlan: async (action, feedback) => {
		const { pendingPlan } = get();
		if (!pendingPlan) return;
		const fetch = getFetch();
		await fetch(apiUrl("/api/pux/plan-response"), {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				planId: pendingPlan.planId,
				action,
				feedback: feedback || "",
			}),
		});
		set({ pendingPlan: null });
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

	setProject: (project) => set({ activeProject: project }),

	setConversation: (project, agentId) =>
		set({
			activeProject: project,
			activeAgentId: agentId || "",
			conversationKey: `${project}:${agentId || "default"}`,
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
				set({ activeProject: projects[0].name || projects[0].path });
			}
		} catch {
			// ignore
		}
	},

	clearError: () => set({ lastError: null }),

	setWorkbenchTab: (tab) => set({ activeWorkbenchTab: tab }),
}));

// Re-export types for convenience
export type { TokenUsage, ContextMetrics, PendingQuestion, PendingApproval, PendingPlan, Conversation, Project, WorkbenchTab };
