/**
 * PuxHistoryAdapter — loads conversation history from backend into @assistant-ui/react.
 *
 * Uses the ThreadHistoryAdapter contract: load() fetches from GET /api/pux/history,
 * append() is a no-op (backend already persists messages via SSE handler).
 *
 * Shared between Web and TUI.
 */

import type { ThreadHistoryAdapter } from "@assistant-ui/react";
import { usePuxStore } from "./pux-store";
import { getFetch } from "./fetch-provider";
import { apiUrl } from "./server-url";

// ── Backend StoredMessage shape ──

interface StoredMessage {
	id: number;
	project: string;
	agentId: string;
	role: string;
	content: string;
	text: string;
	thinking: string;
	toolCalls: string;
	createdAt: string;
}

// ── Message conversion ──

interface ToolCallRecord {
	id?: string;
	name?: string;
	args?: Record<string, unknown>;
	argsText?: string;
	result?: unknown;
	error?: string;
}

function storedToThreadLike(msg: StoredMessage) {
	if (msg.role === "user") {
		return {
			role: "user" as const,
			content: msg.content || "",
		};
	}

	// Assistant message — build parts from thinking, toolCalls, text
	const parts: any[] = [];

	if (msg.thinking) {
		parts.push({ type: "reasoning" as const, text: msg.thinking });
	}

	if (msg.toolCalls && msg.toolCalls !== "[]") {
		try {
			const calls: ToolCallRecord[] = JSON.parse(msg.toolCalls);
			for (const tc of calls) {
				parts.push({
					type: "tool-call" as const,
					toolCallId: tc.id || `tc_${Math.random().toString(36).slice(2)}`,
					toolName: tc.name || "unknown",
					args: tc.args || {},
					argsText:
						tc.argsText || JSON.stringify(tc.args || {}, null, 2),
					...(tc.result !== undefined ? { result: tc.result } : {}),
					...(tc.error ? { isError: true } : {}),
				});
			}
		} catch {
			// skip malformed tool calls
		}
	}

	if (msg.text) {
		parts.push({ type: "text" as const, text: msg.text });
	}

	return {
		role: "assistant" as const,
		content: parts.length > 0 ? parts : msg.text || "",
	};
}

// ── Adapter ──

export function createPuxHistoryAdapter(): ThreadHistoryAdapter {
	return {
		async load() {
			const store = usePuxStore.getState();
			const project = store.activeProject;
			if (!project) return { messages: [] };

			try {
				const params = new URLSearchParams({ project, limit: "200" });
				if (store.activeAgentId) {
					params.set("agentId", store.activeAgentId);
				}

				const fetch = getFetch();
				const resp = await fetch(apiUrl(`/api/pux/history?${params}`));
				if (!resp.ok) return { messages: [] };

				const data: StoredMessage[] = await resp.json();
				if (!Array.isArray(data) || data.length === 0) {
					return { messages: [] };
				}

				const messages = data.map(storedToThreadLike);

				// Import dynamically to avoid circular deps
				const { ExportedMessageRepository } = await import(
					"@assistant-ui/react"
				);
				return ExportedMessageRepository.fromArray(messages);
			} catch {
				return { messages: [] };
			}
		},

		async append() {
			// Backend persists messages during SSE stream — no frontend save needed
		},
	};
}
