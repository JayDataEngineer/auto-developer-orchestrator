/**
 * PuxChatAdapter — Contract 2 SSE → @assistant-ui runtime.
 *
 * POSTs to /api/pux/prompt, parses SSE events, yields ChatModelRunResult
 * snapshots that useLocalRuntime renders via the styled Thread component.
 *
 * Content events (text_delta, thinking_delta, tool_execution_*) → message parts.
 * Meta events (decision_request, compaction, agent_end) → Zustand.
 *
 * Shared between Web (React DOM) and TUI (Ink).
 */

import type {
	ChatModelAdapter,
	ChatModelRunResult,
	TextMessagePart,
	ReasoningMessagePart,
	ToolCallMessagePart,
} from "@assistant-ui/react";
import { usePuxStore } from "./pux-store";
import { getFetch } from "./fetch-provider";
import { apiUrl } from "./server-url";

// ── Types ──

type Part = TextMessagePart | ReasoningMessagePart | ToolCallMessagePart;

interface RunningTool {
	toolCallId: string;
	toolName: string;
	args: Record<string, any>;
	argsText: string;
	interrupt?: { type: "human"; payload: unknown };
}

// ── Meta event dispatcher → Zustand ──

/** Map tool/agent signals to workbench tabs */
function inferTabFromTool(toolName: string, toolArgs: Record<string, unknown>): void {
	const store = usePuxStore.getState();

	// Delegate tools — check which employee
	if (toolName === "delegate_to" || toolName === "delegate_async") {
		const agent = (toolArgs.agent as string) || "";
		if (["jake", "ryan", "browser_ops", "desktop_ops"].includes(agent)) {
			store.setWorkbenchTab("vnc");
			return;
		}
		if (["marcus", "code_ops"].includes(agent)) {
			store.setWorkbenchTab("editor");
			return;
		}
		// Sarah, Alex, Elena, researcher, etc — don't switch
		return;
	}

	// Scheduler tool
	if (toolName === "scheduler") {
		store.setWorkbenchTab("scheduler");
		return;
	}

	// File/code tools
	if (["file_read", "file_write", "file_edit"].includes(toolName)) {
		store.setWorkbenchTab("editor");
		return;
	}

	// Bash with file-like args — could be editing
	if (toolName === "bash") {
		const cmd = (toolArgs.command as string) || "";
		if (/\b(vim|nano|cat|head|tail|sed|awk)\b/.test(cmd) || /\.(ts|tsx|go|py|rs|js|jsx|md|json|yaml|yml|css|html)\b/.test(cmd)) {
			store.setWorkbenchTab("editor");
		}
		return;
	}
}

function handleMetaEvent(eventType: string, data: Record<string, unknown>, statusRef: string[]) {
	switch (eventType) {
		case "agent_spawned": {
			const agentId = data.agentId as string | undefined;
			if (agentId) usePuxStore.setState({ activeAgentId: agentId });
			break;
		}
		case "decision_request": {
			const metadata = data.metadata;
			usePuxStore.setState({
				pendingDecision: {
					decisionId: data.decisionId as string,
					sourceTool: data.sourceTool as string,
					title: data.title as string,
					description: (data.description as string) || "",
					hint: data.hint as "question" | "approval" | "plan_review",
					options: (data.options as string[]) || undefined,
					allowFreeText: (data.allowFreeText as boolean) || undefined,
					metadata: (typeof metadata === "object" && metadata !== null ? metadata : undefined) as Record<string, unknown> | undefined,
				},
			});
			statusRef[0] = "requires-action";
			break;
		}
		case "compaction_start": {
			usePuxStore.setState({ compacting: true });
			break;
		}
		case "compaction_end": {
			usePuxStore.setState({
				compacting: false,
				contextMetrics: {
					contextTokens: (data.contextTokens as number) || 0,
					contextSize: (data.contextSize as number) || 0,
					contextUtil: (data.contextUtil as number) || 0,
					compactionType: (data.compactionType as string) || "",
				},
			});
			break;
		}
		case "agent_end": {
			usePuxStore.setState({
				lastUsage: {
					input: (data.input as number) || 0,
					output: (data.output as number) || 0,
					cache: (data.cache as number) || 0,
					model: data.model as string | undefined,
				},
			});
			break;
		}
		case "error": {
			usePuxStore.setState({ lastError: data.error as string });
			break;
		}
	}
}

// ── Snapshot builder ──

function buildSnapshot(
	accText: string,
	accThinking: string,
	tools: Map<string, RunningTool & { result?: unknown; isError?: boolean; interrupt?: string }>,
	status: "running" | "complete" | "requires-action",
): ChatModelRunResult {
	const parts: Part[] = [];

	// Thinking always first (if present)
	if (accThinking) {
		parts.push({ type: "reasoning", text: accThinking });
	}

	// Tool calls in order they were started
	for (const tool of tools.values()) {
		const part: ToolCallMessagePart = {
			type: "tool-call",
			toolCallId: tool.toolCallId,
			toolName: tool.toolName,
			args: tool.args,
			argsText: tool.argsText,
			...(tool.result !== undefined ? { result: tool.result } : {}),
			...(tool.isError ? { isError: true } : {}),
			...(tool.interrupt ? { interrupt: tool.interrupt } : {}),
		};
		parts.push(part);
	}

	// Text last (matches Contract 2 ordering: thinking → tools → text)
	if (accText) {
		parts.push({ type: "text", text: accText });
	}

	// Determine status natively
	let runStatus: ChatModelRunResult["status"];
	if (status === "complete") {
		runStatus = { type: "complete", reason: "stop" };
	} else if (status === "requires-action") {
		runStatus = { type: "requires-action", reason: "tool-calls" };
	} else {
		runStatus = { type: "running" };
	}

	return {
		content: parts,
		status: runStatus,
	};
}

// ── SSE Parser ──

function parseSSE(
	buffer: string,
): { events: Array<{ event: string; data: string }>; remaining: string } {
	const events: Array<{ event: string; data: string }> = [];
	const lines = buffer.split("\n");
	const remaining = lines.pop() || "";

	let currentEvent = "";
	let currentData = "";

	for (const line of lines) {
		if (line.startsWith("event: ")) {
			currentEvent = line.slice(7).trim();
		} else if (line.startsWith("data: ")) {
			currentData = line.slice(6);
			events.push({ event: currentEvent, data: currentData });
			currentEvent = "";
			currentData = "";
		} else if (line === "") {
			// SSE boundary — already handled above
		}
	}

	return { events, remaining };
}

// ── ChatModelAdapter ──

export const puxChatAdapter: ChatModelAdapter = {
	async *run({ messages, abortSignal }) {
		const store = usePuxStore.getState();
		const lastMsg = messages[messages.length - 1];

		if (!lastMsg || lastMsg.role !== "user") return;

		// Extract text from user message
		const content = lastMsg.content;
		const userText =
			typeof content === "string"
				? content
				: Array.isArray(content)
					? content
							.filter((p: { type: string }) => p.type === "text")
							.map((p: { text: string }) => p.text)
							.join("")
					: "";

		if (!userText) return;

		const project = store.activeProject || "auto-developer-orchestrator";
		const fetch = getFetch();

		// POST to Pux backend
		const resp = await fetch(apiUrl("/api/pux/prompt"), {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				message: userText,
				project,
				agentId: store.activeAgentId || undefined,
				model: store.activeModel || undefined,
			}),
			signal: abortSignal,
		});

		if (!resp.ok) {
			const errText = await resp.text().catch(() => "");
			yield {
				content: [{ type: "text" as const, text: `Error: ${resp.status} ${errText}` }],
				status: { type: "incomplete" as const, reason: "error" as const },
			};
			return;
		}

		const reader = resp.body?.getReader();
		if (!reader) return;

		const decoder = new TextDecoder();
		let buffer = "";

		// Accumulators — the source of truth for building snapshots
		let accText = "";
		let accThinking = "";
		const tools = new Map<string, RunningTool & { result?: unknown; isError?: boolean; interrupt?: string }>();
		// Mutable status ref so handleMetaEvent can update it
		const snapshotStatusRef: ("running" | "complete" | "requires-action")[] = ["running"];

		// Initial yield — empty, running
		yield { content: [], status: { type: "running" as const } };

		try {
			while (true) {
				const { done, value } = await reader.read();
				if (done) break;

				buffer += decoder.decode(value, { stream: true });
				const { events, remaining } = parseSSE(buffer);
				buffer = remaining;

				for (const { event, data } of events) {
					if (data === "[DONE]") {
						yield buildSnapshot(accText, accThinking, tools, "complete");
						return;
					}

					if (event === "keepalive") continue;

					let parsed: Record<string, unknown>;
					try {
						parsed = JSON.parse(data);
					} catch {
						continue;
					}

					switch (event) {
						// ── Content events → message parts ──

						case "thinking_delta": {
							accThinking += (parsed.text as string) || "";
							yield buildSnapshot(accText, accThinking, tools, snapshotStatusRef[0]);
							break;
						}

						case "text_delta": {
							// Skip sub-agent text (we only show CTO text)
							if (parsed.agentName) break;
							accText += (parsed.text as string) || "";
							yield buildSnapshot(accText, accThinking, tools, snapshotStatusRef[0]);
							break;
						}

						case "tool_execution_start": {
							const toolId = (parsed.toolId as string) || `tc_${Date.now()}`;
							const toolName = (parsed.toolName as string) || "unknown";
							const toolArgs = (parsed.toolArgs || parsed.args || {}) as Record<string, unknown>;
							inferTabFromTool(toolName, toolArgs);
							tools.set(toolId, {
								toolCallId: toolId,
								toolName,
								args: toolArgs as any,
								argsText: JSON.stringify(toolArgs, null, 2),
							});
							yield buildSnapshot(accText, accThinking, tools, snapshotStatusRef[0]);
							break;
						}

						case "tool_execution_end": {
							const toolId = parsed.toolId as string;
							const existing = toolId ? tools.get(toolId) : null;
							if (existing) {
								existing.result = parsed.result;
								existing.isError = !!parsed.error;
							}
							yield buildSnapshot(accText, accThinking, tools, snapshotStatusRef[0]);
							break;
						}

						case "tool_execution_update": {
							// Live progress text from a running tool — not a separate part
							// Could be used for partial result display in future
							break;
						}

						case "subagent_start": {
							const agentName = (parsed as Record<string, unknown>).agentName as string | undefined;
							const agentId = (parsed as Record<string, unknown>).agentId as string | undefined;
							const task = (parsed as Record<string, unknown>).task as string || (parsed as Record<string, unknown>).prompt as string || "";
							if (agentName) {
								if (["jake", "ryan", "browser_ops", "desktop_ops"].includes(agentName)) {
									usePuxStore.getState().setWorkbenchTab("vnc");
								} else if (["marcus", "code_ops"].includes(agentName)) {
									usePuxStore.getState().setWorkbenchTab("editor");
								}
								// Phase 3: Track subagent in store
								usePuxStore.getState().addAgent({
									agentId: agentId || `${agentName}_${Date.now()}`,
									agentName,
									task,
									status: "running",
									startedAt: Date.now(),
									toolCalls: [],
								});
							}
							break;
						}
						case "subagent_end": {
							const agentId = (parsed as Record<string, unknown>).agentId as string | undefined;
							const agentName = (parsed as Record<string, unknown>).agentName as string | undefined;
							if (agentId) {
								const result = typeof parsed.result === "string"
									? parsed.result
									: parsed.result ? JSON.stringify(parsed.result) : undefined;
								usePuxStore.getState().updateAgentStatus(agentId, "complete", result);
							} else if (agentName) {
								// Fallback: find by name
								const agents = usePuxStore.getState().agents;
								const match = [...agents.values()].find(
									(a) => a.agentName === agentName && a.status === "running"
								);
								if (match) {
									const result = typeof parsed.result === "string"
										? parsed.result
										: parsed.result ? JSON.stringify(parsed.result) : undefined;
									usePuxStore.getState().updateAgentStatus(match.agentId, "complete", result);
								}
							}
							break;
						}
						case "subagent_tool_start": {
							const agentId = (parsed as Record<string, unknown>).agentId as string | undefined;
							const agentName = (parsed as Record<string, unknown>).agentName as string | undefined;
							const toolName = (parsed as Record<string, unknown>).toolName as string;
							const toolArgs = (parsed as Record<string, unknown>).toolArgs || (parsed as Record<string, unknown>).args;
							if (agentId && toolName) {
								usePuxStore.getState().addAgentToolCall(agentId, {
									toolName,
									args: toolArgs,
									timestamp: Date.now(),
								});
							} else if (agentName && toolName) {
								// Fallback: find by name
								const agents = usePuxStore.getState().agents;
								const match = [...agents.values()].find(
									(a) => a.agentName === agentName && a.status === "running"
								);
								if (match) {
									usePuxStore.getState().addAgentToolCall(match.agentId, {
										toolName,
										args: toolArgs,
										timestamp: Date.now(),
									});
								}
							}
							break;
						}
						case "subagent_thinking_delta":
						case "subagent_text_delta": {
							// Sub-agent text events — tracked in store for agents view
							break;
						}

						// ── Meta events → Zustand ──

						default: {
							handleMetaEvent(event, parsed, snapshotStatusRef);
							break;
						}
					}
				}
			}
		} catch (err) {
			if (err instanceof Error && err.name === "AbortError") return;
			yield {
				content: [{ type: "text" as const, text: `Stream error: ${err}` }],
				status: { type: "incomplete" as const, reason: "error" as const },
			};
			return;
		}

		// Stream ended without [DONE] — yield final snapshot
		yield buildSnapshot(accText, accThinking, tools, "complete");
	},
};
