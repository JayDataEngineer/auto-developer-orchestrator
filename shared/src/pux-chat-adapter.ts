/**
 * PuxChatAdapter — native SSE → @assistant-ui runtime.
 *
 * POSTs to /api/pux/prompt, parses SSE events, yields ChatModelRunResult
 * snapshots that useLocalRuntime renders via the styled Thread component.
 *
 * Content events (text_delta, thinking_delta, tool_execution_*) → message parts.
 * Meta events (decision_request, compaction, agent_end) → Zustand + snapshot metadata.
 *
 * Populates native assistant-ui fields:
 *   - metadata.timing: streamStartTime, firstTokenTime, totalStreamTime, etc.
 *   - metadata.steps: per-turn token usage from agent_end
 *   - metadata.custom: model, contextUtil, agentId
 *   - ToolCallMessagePart.interrupt: set on tools that trigger decision_request
 *   - Status: incomplete/cancelled on abort, requires-action on decisions
 *
 * Shared between Web (React DOM) and TUI (Ink).
 */

import type {
	ChatModelAdapter,
	ChatModelRunResult,
	TextMessagePart,
	ReasoningMessagePart,
	ToolCallMessagePart,
	SourceMessagePart,
} from "@assistant-ui/react";
import { usePuxStore } from "./pux-store";
import { getFetch } from "./fetch-provider";
import { apiUrl } from "./server-url";

// ── Types ──

type Part = TextMessagePart | ReasoningMessagePart | ToolCallMessagePart | SourceMessagePart;

interface RunningTool {
	toolCallId: string;
	toolName: string;
	args: Record<string, any>;
	argsText: string;
	interrupt?: { type: "human"; payload: unknown };
	progress?: string;
	artifact?: unknown;
	modelContent?: readonly { type: "text"; text: string }[];
	messages?: Array<{
		id: string;
		role: "assistant";
		content: Array<{ type: "text"; text: string } | { type: "reasoning"; text: string }>;
		createdAt: Date;
		status: { type: "complete"; reason: "stop" };
		metadata: Record<string, unknown>;
	}>;
}

/** Timing accumulator — populated during streaming for metadata.timing */
interface TimingAccum {
	streamStartTime: number;
	firstTokenTime: number | null;
	totalStreamTime: number | null;
	chunkCount: number;
	tokenCount: number | null;
}

/** Usage step — populated from agent_end for metadata.steps */
interface UsageStep {
	inputTokens: number;
	outputTokens: number;
}

// ── Meta event dispatcher → Zustand + timing + steps ──

/** Map tool/agent signals to workbench tabs */
function inferTabFromTool(toolName: string, toolArgs: Record<string, unknown>): void {
	const store = usePuxStore.getState();

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
		return;
	}

	if (toolName === "scheduler") {
		store.setWorkbenchTab("scheduler");
		return;
	}

	if (["file_read", "file_write", "file_edit"].includes(toolName)) {
		store.setWorkbenchTab("editor");
		return;
	}

	if (toolName === "bash") {
		const cmd = (toolArgs.command as string) || "";
		if (/\b(vim|nano|cat|head|tail|sed|awk)\b/.test(cmd) || /\.(ts|tsx|go|py|rs|js|jsx|md|json|yaml|yml|css|html)\b/.test(cmd)) {
			store.setWorkbenchTab("editor");
		}
		return;
	}
}

function handleMetaEvent(
	eventType: string,
	data: Record<string, unknown>,
	statusRef: string[],
	tools: Map<string, RunningTool & { result?: unknown; isError?: boolean }>,
	stepsRef: UsageStep[],
): void {
	switch (eventType) {
		case "agent_spawned": {
			const agentId = data.agentId as string | undefined;
			if (agentId) {
				usePuxStore.setState({ activeAgentId: agentId });
				// Auto-mark as viewed — user is already on this conversation
				const project = usePuxStore.getState().activeProject;
				if (project) usePuxStore.getState().markViewed(project, agentId);
			}
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

			// Gap 6: Set interrupt on the tool that triggered this decision.
			// The backend sends sourceTool — find it in the running tools map.
			const sourceToolName = data.sourceTool as string;
			if (sourceToolName) {
				for (const tool of tools.values()) {
					if (tool.toolName === sourceToolName && tool.result === undefined) {
						(tool as any).interrupt = { type: "human" as const, payload: data };
						break;
					}
				}
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
			// Gap 2: Record per-step usage
			const inputTokens = (data.input as number) || 0;
			const outputTokens = (data.output as number) || 0;
			stepsRef.push({ inputTokens, outputTokens });

			const contextWindow = (data.contextWindow as number) || 0;
			const contextUtil = contextWindow > 0 ? inputTokens / contextWindow : 0;

			usePuxStore.setState({
				lastUsage: {
					input: inputTokens,
					output: outputTokens,
					cache: (data.cache as number) || 0,
					model: data.model as string | undefined,
				},
				// Update context metrics so status bar shows usage after each turn
				contextMetrics: {
					contextTokens: inputTokens,
					contextSize: contextWindow,
					contextUtil,
					compactionType: "",
				},
			});
			break;
		}
		case "error": {
			usePuxStore.setState({ lastError: data.error as string });
			break;
		}
		case "plan_created": {
			usePuxStore.setState({
				activePlan: {
					planId: data.planId as string,
					name: data.name as string,
					filePath: data.filePath as string,
				},
			});
			break;
		}
	}
}

// ── Snapshot builder ──

type SnapshotStatus = "running" | "complete" | "requires-action";

function buildSnapshot(
	accText: string,
	accThinking: string,
	tools: Map<string, RunningTool & { result?: unknown; isError?: boolean }>,
	sources: SourceMessagePart[],
	status: SnapshotStatus,
	timing: TimingAccum,
	steps: UsageStep[],
): ChatModelRunResult {
	const parts: Part[] = [];

	// Thinking always first (if present)
	if (accThinking) {
		parts.push({ type: "reasoning", text: accThinking });
	}

	// Tool calls in order they were started
	let toolCallCount = 0;
	for (const tool of tools.values()) {
		toolCallCount++;
		const part: ToolCallMessagePart = {
			type: "tool-call",
			toolCallId: tool.toolCallId,
			toolName: tool.toolName,
			args: tool.args,
			argsText: tool.argsText,
			...(tool.result !== undefined ? { result: tool.result } : {}),
			...(tool.progress ? { progress: tool.progress } : {}),
			...(tool.isError ? { isError: true } : {}),
			...(tool.interrupt ? { interrupt: tool.interrupt } : {}),
			...(tool.artifact !== undefined ? { artifact: tool.artifact } : {}),
			...(tool.modelContent ? { modelContent: tool.modelContent } : {}),
			...(tool.messages && tool.messages.length > 0 ? { messages: tool.messages as any } : {}),
		};
		parts.push(part);
	}

	// Source/citation parts after tools
	for (const src of sources) {
		parts.push(src);
	}

	// Text last (matches ordering: thinking → tools → sources → text)
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

	// Gap 1: Build metadata.timing
	const now = Date.now();
	const totalStreamTime = timing.streamStartTime ? now - timing.streamStartTime : 0;
	const tokenCount = timing.tokenCount;
	const tokensPerSecond = totalStreamTime > 0 && tokenCount
		? Math.round((tokenCount / totalStreamTime) * 1000)
		: undefined;

	// Gap 2: Build metadata.steps from accumulated usage
	const stepsMeta = steps.length > 0
		? steps.map((s) => ({ usage: { inputTokens: s.inputTokens, outputTokens: s.outputTokens } }))
		: undefined;

	// Gap 4: Build metadata.custom
	const store = usePuxStore.getState();
	const customMeta: Record<string, unknown> = {};
	if (store.activeModel) customMeta.model = store.activeModel;
	if (store.activeAgentId) customMeta.agentId = store.activeAgentId;
	if (store.activeProject) customMeta.project = store.activeProject;

	return {
		content: parts,
		status: runStatus,
		metadata: {
			timing: {
				streamStartTime: timing.streamStartTime,
				firstTokenTime: timing.firstTokenTime ?? undefined,
				totalStreamTime: status === "complete" ? totalStreamTime : undefined,
				tokenCount: tokenCount ?? undefined,
				tokensPerSecond,
				totalChunks: timing.chunkCount,
				toolCallCount,
			},
			...(stepsMeta ? { steps: stepsMeta } : {}),
			...(Object.keys(customMeta).length > 0 ? { custom: customMeta } : {}),
		},
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
			// SSE boundary
		}
	}

	return { events, remaining };
}

// ── ChatModelAdapter ──

export const puxChatAdapter: ChatModelAdapter = {
	async *run({ messages, abortSignal, runConfig }) {
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

		// Extract image attachments
		const images: string[] =
			Array.isArray(content)
				? content
						.filter((p: { type: string }) => p.type === "image")
						.map((p: { image: string }) => p.image)
				: [];

		if (!userText && images.length === 0) return;

		// Gap 8: Read model from runConfig.custom first, fall back to Zustand store
		const custom = runConfig?.custom as Record<string, unknown> | undefined;
		const model = (custom?.model as string) || store.activeModel || undefined;
		const temperature = (custom?.temperature as number) || undefined;

		const project = store.activeProject || "auto-developer-orchestrator";
		const fetch = getFetch();

		// POST to Pux backend
		const resp = await fetch(apiUrl("/api/pux/prompt"), {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				message: userText || "(image attached)",
				project,
				agentId: store.activeAgentId || undefined,
				...(model ? { model } : {}),
				...(images.length > 0 ? { images } : {}),
				temperature,
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
		const tools = new Map<string, RunningTool & { result?: unknown; isError?: boolean }>();
		const sources: SourceMessagePart[] = [];

		// Sub-agent message tracking — accumulates into the current delegate tool's messages
		let activeSubAgentToolId: string | null = null;
		let subAgentMessageAccum: RunningTool["messages"] extends infer T | undefined ? NonNullable<T> : never = [];

		// Mutable status ref so handleMetaEvent can update it
		const statusRef: SnapshotStatus[] = ["running"];

		// Gap 1: Timing accumulator
		const timing: TimingAccum = {
			streamStartTime: Date.now(),
			firstTokenTime: null,
			totalStreamTime: null,
			chunkCount: 0,
			tokenCount: null,
		};

		// Gap 2: Steps accumulator
		const stepsRef: UsageStep[] = [];

		// Sub-agent tracking: when a sub-agent is active, its tool events
		// are routed to the Zustand store (for nested rendering) instead of
		// the tools map (which becomes flat message parts).
		let activeSubAgentName: string | null = null;

		// Initial yield — empty, running
		yield buildSnapshot(accText, accThinking, tools, sources, "running", timing, stepsRef);

		try {
			while (true) {
				const { done, value } = await reader.read();
				if (done) break;

				buffer += decoder.decode(value, { stream: true });
				const { events, remaining } = parseSSE(buffer);
				buffer = remaining;

				for (const { event, data } of events) {
					if (data === "[DONE]") {
						timing.totalStreamTime = Date.now() - timing.streamStartTime;
						yield buildSnapshot(accText, accThinking, tools, sources, "complete", timing, stepsRef);
						return;
					}

					if (event === "keepalive") continue;

					timing.chunkCount++;

					let parsed: Record<string, unknown>;
					try {
						parsed = JSON.parse(data);
					} catch {
						continue;
					}

					switch (event) {
						// ── Content events → message parts ──

						case "thinking_delta": {
							const thinkingText = (parsed.text as string) || "";
							// Sub-agent thinking → accumulate into sub-agent messages (Gap 9)
							if (parsed.agentName) {
								if (thinkingText && subAgentMessageAccum.length > 0) {
									const last = subAgentMessageAccum[subAgentMessageAccum.length - 1];
									last.content.push({ type: "reasoning" as const, text: thinkingText });
								}
								break;
							}
							// Track first token time
							if (timing.firstTokenTime === null) {
								timing.firstTokenTime = Date.now();
							}
							accThinking += thinkingText;
							yield buildSnapshot(accText, accThinking, tools, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "text_delta": {
							// Sub-agent text → accumulate into sub-agent messages (Gap 9)
							if (parsed.agentName) {
								const text = (parsed.text as string) || "";
								if (text && subAgentMessageAccum.length > 0) {
									const last = subAgentMessageAccum[subAgentMessageAccum.length - 1];
									const lastPart = last.content[last.content.length - 1];
									if (lastPart && lastPart.type === "text") {
										lastPart.text += text;
									} else {
										last.content.push({ type: "text" as const, text });
									}
								} else if (text) {
									subAgentMessageAccum.push({
										id: `submsg_${Date.now()}_${subAgentMessageAccum.length}`,
										role: "assistant",
										content: [{ type: "text" as const, text }],
										createdAt: new Date(),
										status: { type: "complete" as const, reason: "stop" as const },
										metadata: {},
									});
								}
								break;
							}
							// Track first token time
							if (timing.firstTokenTime === null) {
								timing.firstTokenTime = Date.now();
							}
							accText += (parsed.text as string) || "";
							yield buildSnapshot(accText, accThinking, tools, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "tool_execution_start": {
							const toolId = (parsed.toolId as string) || `tc_${Date.now()}`;
							const toolName = (parsed.toolName as string) || "unknown";
							const toolArgs = (parsed.toolArgs || parsed.args || {}) as Record<string, unknown>;
							const toolAgentName = parsed.agentName as string | undefined;

							// Route sub-agent tool calls to Zustand store (for nested
							// rendering) instead of the tools map (flat message parts).
							if (toolAgentName && activeSubAgentName && toolAgentName === activeSubAgentName) {
								const agents = usePuxStore.getState().agents;
								const agent = [...agents.values()].find(
									(a) => a.agentName === toolAgentName && a.status === "running",
								);
								if (agent) {
									usePuxStore.getState().addAgentToolCall(agent.agentId, {
										toolName,
										args: toolArgs,
										timestamp: Date.now(),
									});
								}
								break;
							}

							inferTabFromTool(toolName, toolArgs);
							tools.set(toolId, {
								toolCallId: toolId,
								toolName,
								args: toolArgs as any,
								argsText: JSON.stringify(toolArgs, null, 2),
							});
							yield buildSnapshot(accText, accThinking, tools, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "tool_execution_end": {
							const toolId = parsed.toolId as string;
							const toolAgentName = parsed.agentName as string | undefined;

							// Sub-agent tool completion — update store record, skip tools map
							if (toolAgentName && activeSubAgentName && toolAgentName === activeSubAgentName) {
								const agents = usePuxStore.getState().agents;
								const agent = [...agents.values()].find(
									(a) => a.agentName === toolAgentName && a.status === "running",
								);
								if (agent) {
									const lastTool = agent.toolCalls[agent.toolCalls.length - 1];
									if (lastTool && !lastTool.endedAt) {
										const updated = { ...lastTool, endedAt: Date.now() };
										if (parsed.result !== undefined) updated.result = parsed.result;
										if (parsed.error) updated.isError = true;
										// Replace the last entry immutably
										const newCalls = [...agent.toolCalls];
										newCalls[newCalls.length - 1] = updated;
										const newAgents = new Map(agents);
										newAgents.set(agent.agentId, { ...agent, toolCalls: newCalls });
										usePuxStore.setState({ agents: newAgents });
									}
								}
								break;
							}

							const existing = toolId ? tools.get(toolId) : null;
							if (existing) {
								existing.result = parsed.result;
								existing.isError = !!parsed.error;
								// Gap 11: Artifact data
								if (parsed.artifact !== undefined) {
									existing.artifact = parsed.artifact;
								}
								// Gap 12: Model content separation
								if (parsed.modelContent) {
									existing.modelContent = [{ type: "text" as const, text: parsed.modelContent as string }];
								}
								// Flush accumulated sub-agent messages into delegate tools
								if (activeSubAgentToolId === toolId && subAgentMessageAccum.length > 0) {
									existing.messages = subAgentMessageAccum;
									subAgentMessageAccum = [];
									activeSubAgentToolId = null;
								}
							}
							yield buildSnapshot(accText, accThinking, tools, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "tool_update": {
							const toolId = parsed.toolId as string;
							if (toolId) {
								const existing = tools.get(toolId);
								if (existing) {
									existing.progress = (parsed.text as string) || "";
									yield buildSnapshot(accText, accThinking, tools, sources, statusRef[0], timing, stepsRef);
								}
							}
							break;
						}

						// Gap 10: Source/citation events
						case "source": {
							const sourceType = (parsed.sourceType as string) || "url";
							const sourceId = (parsed.id as string) || `src_${Date.now()}`;
							if (sourceType === "url") {
								sources.push({
									type: "source",
									sourceType: "url",
									id: sourceId,
									url: (parsed.url as string) || "",
									title: (parsed.title as string) || undefined,
								});
							} else {
								sources.push({
									type: "source",
									sourceType: "document",
									id: sourceId,
									title: (parsed.title as string) || "",
									mediaType: (parsed.mediaType as string) || "text/plain",
									filename: (parsed.filename as string) || undefined,
								});
							}
							yield buildSnapshot(accText, accThinking, tools, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "subagent_start": {
							const agentName = parsed.agentName as string | undefined;
							const agentId = parsed.agentId as string | undefined;
							const task = parsed.task as string || parsed.prompt as string || "";
							// Gap 9: Track which delegate tool this sub-agent belongs to
							// so we can collect its messages
							subAgentMessageAccum = [];
							if (agentName) {
								activeSubAgentName = agentName;
								if (["jake", "ryan", "browser_ops", "desktop_ops"].includes(agentName)) {
									usePuxStore.getState().setWorkbenchTab("vnc");
								} else if (["marcus", "code_ops"].includes(agentName)) {
									usePuxStore.getState().setWorkbenchTab("editor");
								}
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
							const agentId = parsed.agentId as string | undefined;
							const agentName = parsed.agentName as string | undefined;
							activeSubAgentName = null;
							const result = typeof parsed.result === "string"
								? parsed.result
								: parsed.result ? JSON.stringify(parsed.result) : undefined;

							// Close any tool calls that never got a tool_execution_end
							// (race: subagent_end may arrive before final tool_execution_end)
							const agents = usePuxStore.getState().agents;
							const match = agentId
								? agents.get(agentId)
								: [...agents.values()].find(
										(a) => a.agentName === agentName && a.status === "running",
									);
							if (match) {
								const hasOpenTools = match.toolCalls.some((t) => !t.endedAt);
								if (hasOpenTools) {
									const now = Date.now();
									const newCalls = match.toolCalls.map((t) =>
										t.endedAt ? t : { ...t, endedAt: now },
									);
									const newAgents = new Map(agents);
									newAgents.set(match.agentId, { ...match, toolCalls: newCalls });
									usePuxStore.setState({ agents: newAgents });
								}
								usePuxStore.getState().updateAgentStatus(match.agentId, "complete", result);
							}
							break;
						}
						case "subagent_thinking_delta": {
							const text = parsed.text as string | undefined;
							const agentName = parsed.agentName as string | undefined;
							if (text && agentName && activeSubAgentName === agentName) {
								const agents = usePuxStore.getState().agents;
								const agent = [...agents.values()].find(
									(a) => a.agentName === agentName && a.status === "running",
								);
								if (agent) {
									usePuxStore.getState().updateAgentThinking(agent.agentId, text);
								}
							}
							break;
						}
						case "subagent_text_delta": {
							// Sub-agent text output — not rendered in the trace,
							// but we could store it for future use
							break;
						}

						// ── Error events → visible error text ──

						case "error": {
							const errMsg = (parsed.error as string) || "Unknown error";
							accText += `\n\n> **Error:** ${errMsg}`;
							statusRef[0] = "complete";
							usePuxStore.setState({ lastError: errMsg });
							break;
						}

						// ── Meta events → Zustand + timing + steps ──

						default: {
							handleMetaEvent(event, parsed, statusRef, tools, stepsRef);
							break;
						}
					}
				}
			}
		} catch (err) {
			if (err instanceof Error && err.name === "AbortError") {
				// Gap 5: Yield cancelled status instead of silent return
				timing.totalStreamTime = Date.now() - timing.streamStartTime;
				yield {
					...buildSnapshot(accText, accThinking, tools, sources, "running", timing, stepsRef),
					status: { type: "incomplete" as const, reason: "cancelled" as const },
				};
				return;
			}
			yield {
				content: [{ type: "text" as const, text: `Stream error: ${err}` }],
				status: { type: "incomplete" as const, reason: "error" as const },
			};
			return;
		}

		// Stream ended without [DONE] — yield final snapshot
		timing.totalStreamTime = Date.now() - timing.streamStartTime;
		yield buildSnapshot(accText, accThinking, tools, sources, "complete", timing, stepsRef);
	},
};
