/**
 * PuxChatAdapter — bridges Contract 2 SSE events to @assistant-ui/react runtime.
 *
 * This is our "Unsloth pattern": a custom ChatModelAdapter whose run() method
 * POSTs to /api/pux/prompt, parses the SSE stream, and yields ChatModelRunResult
 * snapshots that assistant-ui's useLocalRuntime renders automatically.
 *
 * Non-message events (HITL, metrics, compaction) are dispatched to Zustand store.
 *
 * Contract 4 compliance: uses ChatState as the canonical event accumulator.
 * Raw SSE deltas are translated to ChatState events and all content is read
 * from ChatState.messages when building snapshots.
 */

import type {
	ChatModelAdapter,
	ChatModelRunResult,
	TextMessagePart,
	ReasoningMessagePart,
	ToolCallMessagePart,
} from "@assistant-ui/react";
import { ChatState } from "@tui-core/chat-state";
import { usePuxStore } from "./pux-store";

// ── Meta event dispatcher (non-message events → Zustand) ──

function handleMetaEvent(eventType: string, data: Record<string, unknown>) {
	switch (eventType) {
		case "agent_spawned": {
			const agentId = data.agentId as string | undefined;
			if (agentId) usePuxStore.setState({ activeAgentId: agentId });
			break;
		}
		case "user_question": {
			usePuxStore.setState({
				pendingQuestion: {
					questionId: data.questionId as string,
					question: data.question as string,
					options: (data.options as string[]) || [],
					allowFreeText: (data.allowFreeText as boolean) ?? true,
				},
			});
			break;
		}
		case "approval_request": {
			usePuxStore.setState({
				pendingApproval: {
					requestId: data.requestId as string,
					title: (data.title as string) || "",
					description: (data.description as string) || "",
				},
			});
			break;
		}
		case "plan_created": {
			const toolArgs = data.toolArgs as Record<string, unknown> | undefined;
			if (toolArgs) {
				usePuxStore.setState({
					pendingPlan: {
						planId: toolArgs.planId as string,
						name: toolArgs.name as string,
						content: toolArgs.content as string,
					},
				});
			}
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

// ── ChatState-based content builder ──

type Part = TextMessagePart | ReasoningMessagePart | ToolCallMessagePart;

function* buildContent(chatState: ChatState): Generator<ChatModelRunResult, void, undefined> {
	const lastMsg = chatState.messages[chatState.messages.length - 1];
	if (!lastMsg) {
		yield { content: [], status: { type: "running" as const } };
		return;
	}

	const parts: Part[] = [];

	if (lastMsg.thinking) {
		parts.push({ type: "reasoning" as const, text: lastMsg.thinking });
	}

	for (const tc of lastMsg.tools) {
		parts.push({
			type: "tool-call" as const,
			toolCallId: tc.id,
			toolName: tc.name,
			args: (tc.args as Record<string, string>) || {},
			argsText: JSON.stringify(tc.args || {}),
			...(tc.result !== undefined ? { result: tc.result } : {}),
			...(tc.status === "error" ? { isError: true } : {}),
		} as ToolCallMessagePart);
	}

	if (lastMsg.text) {
		parts.push({ type: "text" as const, text: lastMsg.text });
	}

	yield { content: parts, status: { type: "running" as const } };
}

function flushToChatState(
	chatState: ChatState,
	text: string,
	thinking: string,
): void {
	const content: { type: string; text?: string; thinking?: string }[] = [];
	if (thinking) content.push({ type: "thinking", thinking });
	if (text) content.push({ type: "text", text });
	if (content.length > 0) {
		chatState.handleEvent({ type: "message_update", message: { content } });
	}
}

// ── ChatModelAdapter ──

export const puxChatAdapter: ChatModelAdapter = {
	async *run({ messages, abortSignal }) {
		const store = usePuxStore.getState();
		const lastMsg = messages[messages.length - 1];

		if (!lastMsg || lastMsg.role !== "user") return;

		// Extract text from user message
		const content = lastMsg.content;
		const text =
			typeof content === "string"
				? content
				: Array.isArray(content)
					? content
							.filter((p: { type: string }) => p.type === "text")
							.map((p: { text: string }) => p.text)
							.join("")
					: "";

		const project = store.activeProject || "auto-developer-orchestrator";

		// POST to Pux backend
		const resp = await fetch("/api/pux/prompt", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				message: text,
				project,
				agentId: store.activeAgentId || undefined,
			}),
			signal: abortSignal,
		});

		if (!resp.ok) {
			const errText = await resp.text().catch(() => "");
			const result: ChatModelRunResult = {
				content: [{ type: "text" as const, text: `Error: ${resp.status} ${errText}` }],
				status: { type: "incomplete" as const, reason: "error" as const },
			};
			yield result;
			return;
		}

		const reader = resp.body?.getReader();
		if (!reader) return;

		const decoder = new TextDecoder();
		let buffer = "";
		let currentEvent = "";

		// ChatState is the canonical event accumulator (Contract 4).
		// Raw SSE deltas are translated to ChatState events via flushToChatState.
		const chatState = new ChatState();

		// Accumulator for building complete text before flushing to ChatState.
		let accText = "";
		let accThinking = "";

		// Initial yield — running
		yield { content: [], status: { type: "running" as const } };

		try {
			while (true) {
				const { done, value } = await reader.read();
				if (done) break;

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split("\n");
				buffer = lines.pop() || "";

				for (const line of lines) {
					if (line.startsWith("event: ")) {
						currentEvent = line.slice(7).trim();
					} else if (line.startsWith("data: ")) {
						const raw = line.slice(6);
						if (raw === "[DONE]") {
							flushToChatState(chatState, accText, accThinking);
							yield* buildContent(chatState);
							return;
						}
						if (currentEvent === "keepalive") {
							currentEvent = "";
							continue;
						}

						try {
							const data = JSON.parse(raw);

							switch (currentEvent) {
								case "agent_start": {
									chatState.handleEvent({
										type: "message_start",
										message: { role: "assistant", content: [] },
									});
									break;
								}
								case "thinking_delta": {
									accThinking += (data.text as string) || "";
									flushToChatState(chatState, accText, accThinking);
									yield* buildContent(chatState);
									break;
								}
								case "text_delta": {
									if (!data.agentName) {
										accText += (data.text as string) || "";
										flushToChatState(chatState, accText, accThinking);
										yield* buildContent(chatState);
									}
									break;
								}
								case "tool_execution_start": {
									flushToChatState(chatState, accText, accThinking);
									chatState.handleEvent({
										type: "tool_execution_start",
										toolCallId: (data.toolId as string) || `tc_${Date.now()}`,
										toolName: (data.toolName as string) || "unknown",
										args: data.toolArgs || data.args || {},
									});
									yield* buildContent(chatState);
									break;
								}
								case "tool_execution_end": {
									flushToChatState(chatState, accText, accThinking);
									chatState.handleEvent({
										type: "tool_execution_end",
										toolCallId: (data.toolId as string),
										result: data.result,
										isError: !!data.error,
									});
									yield* buildContent(chatState);
									break;
								}
								case "agent_end": {
									flushToChatState(chatState, accText, accThinking);
									chatState.handleEvent({ type: "agent_end" });
									chatState.handleEvent({
										type: "message_end",
										message: { stopReason: "stop" },
									});
									break;
								}
								default: {
									// Meta events → Zustand
									handleMetaEvent(currentEvent, data);
									break;
								}
							}
						} catch {
							// Malformed JSON — skip
						}

						currentEvent = "";
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

		// Stream ended without [DONE] — final yield
		flushToChatState(chatState, accText, accThinking);
		yield* buildContent(chatState);
	},
};
