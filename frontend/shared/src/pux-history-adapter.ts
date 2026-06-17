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
import type { AgentState, ToolCallRecord as StoreToolCall } from "./types";

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
	subAgent?: {
		name: string;
		status: string;
		toolCalls: Array<{
			id?: string;
			name: string;
			args?: Record<string, unknown>;
			result?: string;
			error?: string;
		}>;
		thinking?: string;
		text?: string;
		result?: string;
		error?: string;
	};
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
export function storedMessagesToThreadLikes(data: StoredMessage[]): ThreadLike[] {
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

					// Build args, injecting subAgent for delegate tools
					const args = { ...(tc.args || {}) };
					if (tc.subAgent) {
						(args as Record<string, unknown>).__subAgent = tc.subAgent;
					}

					parts.push({
						type: "tool-call" as const,
						toolCallId: callId,
						toolName: tc.name || "unknown",
						args,
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

// ── Reconstruct agents from persisted subAgent traces ──

/**
 * Scans StoredMessages for delegate_to/delegate_async tool calls that
 * contain subAgent traces (persisted by the Go backend). Reconstructs
 * AgentState entries and adds them to the Zustand store — but only if
 * the agent isn't already tracked (avoids overwriting live agents).
 */
export function restoreAgentsFromHistory(data: StoredMessage[]) {
	const store = usePuxStore.getState();
	const existing = store.agents;

	for (const msg of data) {
		if (msg.role !== "assistant" || !msg.toolCalls || msg.toolCalls === "[]") continue;
		try {
			const calls: ToolCallRecord[] = JSON.parse(msg.toolCalls);
			for (const tc of calls) {
				if (!tc.subAgent) continue;
				if (tc.name !== "delegate_to" && tc.name !== "delegate_async") continue;

				const sa = tc.subAgent;
				// Build a stable agentId from tool call id or name+timestamp
				const agentId = tc.id
					? `hist_${tc.id}`
					: `hist_${sa.name}_${msg.id}`;

				// Skip if already tracked (live agent or previously restored)
				if (existing.has(agentId)) continue;

				const toolCalls: StoreToolCall[] = sa.toolCalls.map((stc, i) => ({
					toolName: stc.name,
					args: stc.args,
					result: stc.result,
					isError: !!stc.error,
					timestamp: new Date(msg.createdAt).getTime() + i,
					endedAt: new Date(msg.createdAt).getTime() + i + 1,
				}));

				const agent: AgentState = {
					agentId,
					agentName: sa.name,
					task: (tc.args?.task as string) || (tc.args?.prompt as string) || (tc.args?.role as string) || "",
					status: sa.status === "error" ? "error" : "complete",
					startedAt: new Date(msg.createdAt).getTime(),
					endedAt: new Date(msg.createdAt).getTime() + 1,
					rounds: [{
						thinking: sa.thinking,
						toolCalls: toolCalls,
						text: sa.text,
					}],
					toolCalls,
					thinkingText: sa.thinking,
					text: sa.text,
					result: sa.result,
					error: sa.error,
				};

				existing.set(agentId, agent);
			}
		} catch {
			// Skip malformed tool calls
		}
	}

	// Batch update — only if we added anything
	if (existing.size !== store.agents.size) {
		usePuxStore.setState({ agents: new Map(existing) });
	}
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
					return { messages: [] };
				}

				const data: StoredMessage[] = await resp.json();
				if (!Array.isArray(data) || data.length === 0) {
					return { messages: [] };
				}

				// Reconstruct sub-agent state from persisted traces
				restoreAgentsFromHistory(data);

				// Estimate context metrics from loaded history so the status bar
				// shows context usage immediately (not just after next agent_end)
				estimateContextFromHistory(data);

				const messages = storedMessagesToThreadLikes(data);

				// Import dynamically to avoid circular deps
				const { ExportedMessageRepository } = await import(
					"@assistant-ui/react"
				);
				return ExportedMessageRepository.fromArray(messages);
			} catch {
				// Silently return empty — Ink is running, console.error causes a flash
				return { messages: [] };
			}
		},

		async append() {
			// Backend persists messages during SSE stream — no frontend save needed
		},
	};
}

// ── Estimate context metrics from history ──

/**
 * Estimates context token usage from loaded history messages using
 * the same char-to-token heuristic as the Go backend (chars * 0.3).
 * Sets contextMetrics in the store so the status bar shows context
 * usage immediately on resume, not just after the next agent_end.
 */
export function estimateContextFromHistory(data: StoredMessage[]) {
	if (data.length === 0) return;

	// Sum characters from all message content fields
	let totalChars = 0;
	for (const msg of data) {
		totalChars += (msg.content || "").length;
		totalChars += (msg.text || "").length;
		totalChars += (msg.thinking || "").length;
		if (msg.toolCalls && msg.toolCalls !== "[]") {
			totalChars += msg.toolCalls.length;
		}
	}

	const estimatedTokens = Math.round(totalChars * 0.3);
	if (estimatedTokens === 0) return;

	// Look up context window from model list
	const store = usePuxStore.getState();
	const activeModel = store.activeModel || store.lastUsage?.model || "";
	const modelEntry = store.modelList?.find((m) => m.id === activeModel);
	const contextWindow = modelEntry?.contextWindow || 0;

	// Only set if we have a context window (otherwise wait for agent_end)
	if (contextWindow > 0) {
		usePuxStore.setState({
			contextMetrics: {
				contextTokens: estimatedTokens,
				contextSize: contextWindow,
				contextUtil: estimatedTokens / contextWindow,
				compactionType: "",
			},
		});
	}
}
