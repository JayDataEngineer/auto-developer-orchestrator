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
	toolCallId: string;
	toolName: string;
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

type ThreadLike =
	| { role: "user"; content: string }
	| { role: "assistant"; content: string | any[] };

/**
 * Converts an array of StoredMessages into ThreadMessageLike objects.
 *
 * The backend stores messages in 3 shapes:
 *   - role="user"     → user messages (content field)
 *   - role="assistant" → assistant messages (text + thinking + toolCalls fields)
 *   - role="tool"     → tool results (content = result, toolCallId links to parent)
 *
 * Tool results are re-attached to their parent tool-call parts in the preceding
 * assistant message. Empty assistant messages (from streaming mid-saves) are skipped.
 */
function storedMessagesToThreadLikes(data: StoredMessage[]): ThreadLike[] {
	// First pass: build a toolCallId → result lookup from tool role messages
	const toolResults = new Map<string, { result: string; isError: boolean }>();
	for (const msg of data) {
		if (msg.role === "tool" && msg.toolCallId) {
			let result = msg.content || "";
			let isError = false;
			// Try to parse as JSON and check for error indicators
			try {
				const parsed = JSON.parse(result);
				if (parsed.error) isError = true;
			} catch {
				// Not JSON — use raw content
			}
			toolResults.set(msg.toolCallId, { result, isError });
		}
	}

	const messages: ThreadLike[] = [];

	for (const msg of data) {
		if (msg.role === "user") {
			messages.push({
				role: "user" as const,
				content: msg.content || "",
			});
			continue;
		}

		// Skip tool role messages — their results are attached to tool-call parts
		if (msg.role === "tool") {
			continue;
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
					const callId = tc.id || `tc_${Math.random().toString(36).slice(2)}`;
					// Re-attach tool result from the tool role message
					const toolResult = toolResults.get(callId);
					parts.push({
						type: "tool-call" as const,
						toolCallId: callId,
						toolName: tc.name || "unknown",
						args: tc.args || {},
						argsText:
							tc.argsText || JSON.stringify(tc.args || {}, null, 2),
						...(toolResult
							? {
									result: toolResult.result,
									isError: toolResult.isError,
								}
							: {}),
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

		// Skip empty assistant messages (from streaming mid-saves or placeholders)
		if (parts.length === 0 && !msg.text) {
			continue;
		}

		messages.push({
			role: "assistant" as const,
			content: parts.length > 0 ? parts : msg.text || "",
		});
	}

	return messages;
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
				if (!resp.ok) {
					console.warn(
						`[history] API returned ${resp.status} for ${project}/${store.activeAgentId}`,
					);
					return { messages: [] };
				}

				const data: StoredMessage[] = await resp.json();
				if (!Array.isArray(data) || data.length === 0) {
					return { messages: [] };
				}

				const messages = storedMessagesToThreadLikes(data);

				// Import dynamically to avoid circular deps
				const { ExportedMessageRepository } = await import(
					"@assistant-ui/react"
				);
				return ExportedMessageRepository.fromArray(messages);
			} catch (err) {
				console.error("[history] Failed to load:", err);
				return { messages: [] };
			}
		},

		async append() {
			// Backend persists messages during SSE stream — no frontend save needed
		},
	};
}
