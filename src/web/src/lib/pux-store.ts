/**
 * Pux Zustand Store — non-message state.
 *
 * Message/tool/subagent state is managed by @assistant-ui/react runtime
 * via PuxChatAdapter. This store handles HITL dialogs, metrics, models,
 * and other Pux-specific state that assistant-ui doesn't cover.
 */

import { create } from "zustand";

// ── Types ──

export interface TokenUsage {
	input: number;
	output: number;
	cache: number;
	model?: string;
}

export interface ContextMetrics {
	contextTokens: number;
	contextSize: number;
	contextUtil: number;
	compactionType: string;
}

export interface PendingQuestion {
	questionId: string;
	question: string;
	options: string[];
	allowFreeText: boolean;
}

export interface PendingApproval {
	requestId: string;
	title: string;
	description: string;
}

export interface PendingPlan {
	planId: string;
	name: string;
	content: string;
}

export interface Conversation {
	project: string;
	agentId: string;
	lastMessage: string;
	lastAt: string;
	messageCount: number;
	title: string;
}

export interface Project {
	name: string;
	path: string;
	description?: string;
	version?: string;
	hasManifest?: boolean;
}

export type WorkbenchTab = "vnc" | "editor" | "scheduler";

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

	// Project
	activeProject: string;
	activeAgentId: string;

	// Model
	modelList: Array<{ id: string; name: string; provider: string }>;

	// Conversations
	conversations: Conversation[];

	// Projects
	projects: Project[];

	// Workbench (auto-driven by SSE tool events)
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
	modelList: [],
	conversations: [],
	projects: [],
	activeWorkbenchTab: "vnc",
	lastError: null,

	respondToQuestion: async (response) => {
		const { pendingQuestion } = get();
		if (!pendingQuestion) return;
		await fetch("/api/pux/respond", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ requestId: pendingQuestion.questionId, response }),
		});
		set({ pendingQuestion: null });
	},

	respondToApproval: async (approved) => {
		const { pendingApproval } = get();
		if (!pendingApproval) return;
		await fetch("/api/pux/respond", {
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
		await fetch("/api/pux/plan-response", {
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
			const resp = await fetch("/api/pux/models");
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

	loadConversations: async () => {
		try {
			const resp = await fetch("/api/pux/conversations");
			if (!resp.ok) return;
			const data = await resp.json();
			set({ conversations: Array.isArray(data) ? data : [] });
		} catch {
			// ignore
		}
	},

	loadProjects: async () => {
		try {
			const resp = await fetch("/api/pux/projects");
			if (!resp.ok) return;
			const data = await resp.json();
			const projects = Array.isArray(data) ? data : [];
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
