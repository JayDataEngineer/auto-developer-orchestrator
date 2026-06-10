/**
 * PuxChatAdapter — native SSE → @assistant-ui runtime.
 *
 * POSTs to /api/pux/prompt, parses SSE events, yields ChatModelRunResult
 * snapshots that useLocalRuntime renders via the styled Thread component.
 *
 * Uses a segment model: SSE events append to an ordered `parts[]` timeline
 * in arrival order. Adjacent same-type events (thinking, text) merge into
 * one segment. Tool calls are separate segments updated in-place by ID.
 *
 * Content events (text_delta, thinking_delta, tool_execution_*) → message segments.
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

/** Extended tool-call segment that carries extra fields during streaming */
type ToolCallSegment = ToolCallMessagePart & {
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
};

type Segment = TextMessagePart | ReasoningMessagePart | ToolCallSegment | SourceMessagePart;

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

// ── Segment helpers ──

/** Append thinking text — merge into last segment if it's reasoning, else new segment */
function appendThinking(parts: Segment[], text: string): void {
	const last = parts[parts.length - 1];
	if (last?.type === "reasoning") {
		// Replace element — ReasoningMessagePart.text is readonly
		parts[parts.length - 1] = { type: "reasoning", text: last.text + text };
	} else {
		parts.push({ type: "reasoning", text });
	}
}

/** Append response text — merge into last segment if it's text, else new segment */
function appendText(parts: Segment[], text: string): void {
	const last = parts[parts.length - 1];
	if (last?.type === "text") {
		// Replace element — TextMessagePart.text is readonly
		parts[parts.length - 1] = { type: "text", text: last.text + text };
	} else {
		parts.push({ type: "text", text });
	}
}

/** Find a tool-call segment by its toolCallId */
function findToolPart(parts: Segment[], toolCallId: string): ToolCallSegment | undefined {
	return parts.find((p): p is ToolCallSegment => p.type === "tool-call" && p.toolCallId === toolCallId);
}

/** Update a tool-call segment immutably (properties are readonly on ToolCallMessagePart) */
function updateToolPart(parts: Segment[], toolCallId: string, updates: Record<string, unknown>): ToolCallSegment | undefined {
	const idx = parts.findIndex((p): p is ToolCallSegment => p.type === "tool-call" && p.toolCallId === toolCallId);
	if (idx === -1) return undefined;
	const updated = { ...parts[idx], ...updates } as ToolCallSegment;
	parts[idx] = updated;
	return updated;
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
	parts: Segment[],
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

			// Set interrupt on the tool segment that triggered this decision
			const sourceToolName = data.sourceTool as string;
			if (sourceToolName) {
				for (let i = 0; i < parts.length; i++) {
					const part = parts[i];
					if (part.type === "tool-call" && part.toolName === sourceToolName && !("result" in part)) {
						parts[i] = { ...part, interrupt: { type: "human" as const, payload: data } };
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
			const inputTokens = (data.input as number) || 0;
			const outputTokens = (data.output as number) || 0;
			stepsRef.push({ inputTokens, outputTokens });

			// Use actual context tokens from last API call (not cumulative total)
			const contextTokens = (data.contextTokens as number) || 0;
			const contextWindow = (data.contextWindow as number) || 0;
			const contextUtil = contextWindow > 0 && contextTokens > 0
				? contextTokens / contextWindow
				: 0;

			const actualModel = (data.model as string) || undefined;

			usePuxStore.setState({
				lastUsage: {
					input: inputTokens,
					output: outputTokens,
					cache: (data.cache as number) || 0,
					model: actualModel,
				},
				contextMetrics: {
					contextTokens: contextTokens || inputTokens,
					contextSize: contextWindow,
					contextUtil,
					compactionType: "",
				},
				...(actualModel ? { activeModel: actualModel } : {}),
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
		case "task_started": {
			const taskId = data.taskId as string;
			const command = data.command as string;
			usePuxStore.getState().addBackgroundTask({
				id: taskId,
				command,
				status: "running",
				output: "",
				startTime: Date.now(),
			});
			usePuxStore.getState().setForegroundTask(taskId);
			break;
		}
		case "task_completed": {
			const taskId = data.taskId as string;
			const status = (data.status as string) === "failed" ? "failed" : "completed";
			usePuxStore.getState().updateBackgroundTask(taskId, {
				status,
				output: (data.text as string) || "",
				exitCode: (data.exitCode as number) || 0,
				error: (data.error as string) || "",
				endTime: Date.now(),
			});
			if (usePuxStore.getState().foregroundTaskId === taskId) {
				usePuxStore.getState().setForegroundTask(null);
			}
			break;
		}
		case "task_background": {
			const taskId = data.taskId as string;
			usePuxStore.getState().updateBackgroundTask(taskId, {
				status: "backgrounded",
			});
			if (usePuxStore.getState().foregroundTaskId === taskId) {
				usePuxStore.getState().setForegroundTask(null);
			}
			break;
		}
	}
}

// ── Snapshot builder ──

type SnapshotStatus = "running" | "complete" | "requires-action";

// Reorder parts so all reasoning comes first, then tool calls, then text.
// This ensures the library's grouping algorithm sees consecutive reasoning
// parts and groups them into a single block at the top of the message.
function reorderParts(parts: Segment[]): Segment[] {
	const reasoning = parts.filter(p => p.type === "reasoning");
	const tools = parts.filter(p => p.type === "tool-call");
	const rest = parts.filter(p => p.type !== "reasoning" && p.type !== "tool-call");
	return [...reasoning, ...tools, ...rest];
}

function buildSnapshot(
	parts: Segment[],
	sources: SourceMessagePart[],
	status: SnapshotStatus,
	timing: TimingAccum,
	steps: UsageStep[],
): ChatModelRunResult {
	// Reorder: reasoning first → tools → text, then append sources at end
	const content: Segment[] = reorderParts(parts);
	for (const src of sources) content.push(src);

	const toolCallCount = parts.filter(p => p.type === "tool-call").length;

	// Determine status
	let runStatus: ChatModelRunResult["status"];
	if (status === "complete") {
		runStatus = { type: "complete", reason: "stop" };
	} else if (status === "requires-action") {
		runStatus = { type: "requires-action", reason: "tool-calls" };
	} else {
		runStatus = { type: "running" };
	}

	// Build metadata.timing
	const now = Date.now();
	const totalStreamTime = timing.streamStartTime ? now - timing.streamStartTime : 0;
	const tokenCount = timing.tokenCount;
	const tokensPerSecond = totalStreamTime > 0 && tokenCount
		? Math.round((tokenCount / totalStreamTime) * 1000)
		: undefined;

	// Build metadata.steps from accumulated usage
	const stepsMeta = steps.length > 0
		? steps.map((s) => ({ usage: { inputTokens: s.inputTokens, outputTokens: s.outputTokens } }))
		: undefined;

	// Build metadata.custom
	const store = usePuxStore.getState();
	const customMeta: Record<string, unknown> = {};
	if (store.activeModel) customMeta.model = store.activeModel;
	if (store.activeAgentId) customMeta.agentId = store.activeAgentId;
	if (store.activeProject) customMeta.project = store.activeProject;

	return {
		content,
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

// ── Read timeout ──

const STREAM_STALL_TIMEOUT = 30_000; // 30s — 2x the 15s keepalive interval

function readWithTimeout(
	reader: ReadableStreamDefaultReader<Uint8Array>,
	ms: number,
): Promise<ReadableStreamReadResult<Uint8Array>> {
	return new Promise((resolve, reject) => {
		const timer = setTimeout(
			() => reject(new DOMException("Stream stalled", "TimeoutError")),
			ms,
		);
		reader.read().then(
			(r) => { clearTimeout(timer); resolve(r); },
			(e) => { clearTimeout(timer); reject(e); },
		);
	});
}

// ── ChatModelAdapter ──

function debug(...args: unknown[]) {
	try {
		if (typeof process === "undefined" || !process.env) return;
		// Only log when DEBUG_PUX is set — stderr writes corrupt the TUI
		// in alt-screen mode (debug text appears on screen mid-render).
		if (!process.env.DEBUG_PUX) return;
		const msg = args.map(a => String(a)).join(" ") + "\n";
		try {
			const fs = require("node:fs") as typeof import("node:fs");
			fs.appendFileSync("/tmp/pux-run-debug.log", msg);
		} catch {}
	} catch {}
}

export const puxChatAdapter: ChatModelAdapter = {
	async *run({ messages, abortSignal, runConfig }) {
		const store = usePuxStore.getState();
		const lastMsg = messages[messages.length - 1];
		const stack = new Error().stack?.split('\n').slice(2, 6).join(' | ') || 'no stack';

		debug("[run] ENTERED. count:", messages.length, "lastRole:", lastMsg?.role, "lastText:", typeof lastMsg?.content === 'string' ? String(lastMsg.content).slice(0, 50) : 'non-string', "agentId:", store.activeAgentId, "stack:", stack);

		if (!lastMsg || lastMsg.role !== "user") {
			debug("[run] SKIPPED: not user msg");
			return;
		}
		debug("[run] PROCEEDING TO POST. message:", typeof lastMsg.content === 'string' ? String(lastMsg.content).slice(0, 100) : 'non-string');

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

		// Ordered segment timeline — events append in arrival order
		const parts: Segment[] = [];
		const sources: SourceMessagePart[] = [];

		// Sub-agent message tracking
		let activeSubAgentToolId: string | null = null;
		let subAgentMessageAccum: NonNullable<ToolCallSegment["messages"]> = [];

		// Mutable status ref so handleMetaEvent can update it
		const statusRef: SnapshotStatus[] = ["running"];

		// Timing accumulator
		const timing: TimingAccum = {
			streamStartTime: Date.now(),
			firstTokenTime: null,
			totalStreamTime: null,
			chunkCount: 0,
			tokenCount: null,
		};

		// Steps accumulator
		const stepsRef: UsageStep[] = [];

		// Sub-agent tracking
		let activeSubAgentName: string | null = null;

		// Initial yield — empty, running.
		// ctoRunning is now derived from assistant-ui's isRunning via
		// CtoRunningBridge component (in app.tsx), so the adapter does NOT
		// set it directly. This eliminates the cross-store timing issue that
		// caused the Enter flash (Zustand-triggered renders reading stale
		// assistant-ui state).
		yield buildSnapshot(parts, sources, "running", timing, stepsRef);

		try {
			while (true) {
				let readResult: ReadableStreamReadResult<Uint8Array>;
				try {
					readResult = await readWithTimeout(reader, STREAM_STALL_TIMEOUT);
				} catch (readErr) {
					// Stream stalled — no data for 30s. Yield complete and return.
					if (readErr instanceof DOMException && readErr.name === "TimeoutError") {
						timing.totalStreamTime = Date.now() - timing.streamStartTime;
						yield buildSnapshot(parts, sources, "complete", timing, stepsRef);
						return;
					}
					throw readErr;
				}

				if (readResult.done) break;
				const value = readResult.value!;

				buffer += decoder.decode(value, { stream: true });
				const { events, remaining } = parseSSE(buffer);
				buffer = remaining;

				for (const { event, data } of events) {
					if (data === "[DONE]") {
						timing.totalStreamTime = Date.now() - timing.streamStartTime;
						yield buildSnapshot(parts, sources, "complete", timing, stepsRef);
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
						// ── Content events → timeline segments ──

						case "thinking_delta": {
							const thinkingText = (parsed.text as string) || "";
							// Sub-agent thinking → round-based Zustand store
							if (parsed.agentName) {
								const agents = usePuxStore.getState().agents;
								const agent = [...agents.values()].find(
									(a) => a.agentName === parsed.agentName,
								);
								if (agent && thinkingText) {
									usePuxStore.getState().appendAgentRoundThinking(agent.agentId, thinkingText);
								}
								break;
							}
							if (timing.firstTokenTime === null) {
								timing.firstTokenTime = Date.now();
							}
							appendThinking(parts, thinkingText);
							yield buildSnapshot(parts, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "text_delta": {
							// Sub-agent text → round-based Zustand store
							if (parsed.agentName) {
								const text = (parsed.text as string) || "";
								if (text) {
									const agents = usePuxStore.getState().agents;
									const agent = [...agents.values()].find(
										(a) => a.agentName === parsed.agentName,
									);
									if (agent) {
										usePuxStore.getState().appendAgentRoundText(agent.agentId, text);
									}
								}
								break;
							}
							if (timing.firstTokenTime === null) {
								timing.firstTokenTime = Date.now();
							}
							appendText(parts, (parsed.text as string) || "");
							yield buildSnapshot(parts, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "tool_execution_start": {
							let toolId = (parsed.toolId as string) || `tc_${Date.now()}`;
							// Ensure unique toolCallId
							if (findToolPart(parts, toolId) !== undefined) {
								const toolCount = parts.filter(p => p.type === "tool-call").length;
								toolId = `${toolId}_${toolCount}`;
							}
							const toolName = (parsed.toolName as string) || "unknown";
							const toolArgs = (parsed.toolArgs || parsed.args || {}) as Record<string, unknown>;
							const toolAgentName = parsed.agentName as string | undefined;

							// Route sub-agent tool calls to Zustand store (round-based)
							if (toolAgentName) {
								const agents = usePuxStore.getState().agents;
								const agent = [...agents.values()].find(
									(a) => a.agentName === toolAgentName && a.status === "running",
								);
								if (agent) {
									usePuxStore.getState().appendAgentRoundToolCall(agent.agentId, {
										toolName,
										args: toolArgs,
										timestamp: Date.now(),
									});
									break;
								}
							}

							inferTabFromTool(toolName, toolArgs);
							parts.push({
								type: "tool-call",
								toolCallId: toolId,
								toolName,
								args: toolArgs as any,
								argsText: JSON.stringify(toolArgs, null, 2),
							});
							yield buildSnapshot(parts, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "tool_execution_end": {
							const toolId = parsed.toolId as string;
							const toolAgentName = parsed.agentName as string | undefined;

							// Sub-agent tool completion — update round-based Zustand store
							if (toolAgentName) {
								const agents = usePuxStore.getState().agents;
								const agent = [...agents.values()].find(
									(a) => a.agentName === toolAgentName && a.status === "running",
								);
								if (agent) {
									const updates: Record<string, unknown> = { endedAt: Date.now() };
									if (parsed.result !== undefined) updates.result = parsed.result;
									if (parsed.error) updates.isError = true;
									usePuxStore.getState().updateAgentRoundToolCall(agent.agentId, updates);
									break;
								}
							}

							if (toolId) {
								const updates: Record<string, unknown> = {};
								const errMsg = parsed.error as string | undefined;
								if (errMsg) {
									// Tool failed — store error as result so rendering can show it.
									// If result is also present, prepend it before the error.
									const existing = parsed.result;
									updates.isError = true;
									updates.result = existing
										? `${typeof existing === "string" ? existing : JSON.stringify(existing)}\n\nError: ${errMsg}`
										: `Error: ${errMsg}`;
								} else if (parsed.result !== undefined && parsed.result !== null) {
									updates.result = parsed.result;
								}
								if (parsed.artifact !== undefined) updates.artifact = parsed.artifact;
								if (parsed.modelContent) {
									updates.modelContent = [{ type: "text" as const, text: parsed.modelContent as string }];
								}
								const tool = updateToolPart(parts, toolId, updates);
								// Flush accumulated sub-agent messages into delegate tools
								if (tool && activeSubAgentToolId === toolId && subAgentMessageAccum.length > 0) {
									updateToolPart(parts, toolId, { messages: subAgentMessageAccum });
									subAgentMessageAccum = [];
									activeSubAgentToolId = null;
								}
							}
							yield buildSnapshot(parts, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "tool_update": {
							const toolId = parsed.toolId as string;
							const updateAgentName = parsed.agentName as string | undefined;
							// Sub-agent tool updates: skip
							if (updateAgentName) {
								break;
							}
							if (toolId) {
								const updated = updateToolPart(parts, toolId, {
									progress: (parsed.text as string) || "",
								});
								if (updated) {
									yield buildSnapshot(parts, sources, statusRef[0], timing, stepsRef);
								}
							}
							break;
						}

						// Source/citation events
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
							yield buildSnapshot(parts, sources, statusRef[0], timing, stepsRef);
							break;
						}

						case "subagent_start": {
							const agentName = parsed.agentName as string | undefined;
							const agentId = parsed.agentId as string | undefined;
							const task = parsed.task as string || parsed.prompt as string || "";
							const transcriptId = parsed.transcriptId as string | undefined;
							subAgentMessageAccum = [];
							if (agentName) {
								activeSubAgentName = agentName;
								if (["jake", "ryan", "browser_ops", "desktop_ops"].includes(agentName)) {
									usePuxStore.getState().setWorkbenchTab("vnc");
								} else if (["marcus", "code_ops"].includes(agentName)) {
									usePuxStore.getState().setWorkbenchTab("editor");
								}
								const storeAgentId = agentId || `${agentName}_${Date.now()}`;
								usePuxStore.getState().addAgent({
									agentId: storeAgentId,
									agentName,
									task,
									status: "running",
									startedAt: Date.now(),
									rounds: [],
									toolCalls: [],
									transcriptId,
								});
								// Inject agentId into the running delegate tool's args
								if (agentId) {
									for (const part of parts) {
										if (part.type === "tool-call" &&
											(part.toolName === "delegate_to" || part.toolName === "delegate_async") &&
											!("result" in part)) {
											(part.args as Record<string, unknown>).__agentId = agentId;
											break;
										}
									}
								}
							}
							break;
						}
						case "subagent_end": {
							const agentId = parsed.agentId as string | undefined;
							const agentName = parsed.agentName as string | undefined;
							const endStatus = parsed.status as string;
							activeSubAgentName = null;
							const result = typeof parsed.result === "string"
								? parsed.result
								: parsed.result ? JSON.stringify(parsed.result) : undefined;

							const agents = usePuxStore.getState().agents;
							const match = agentId
								? agents.get(agentId)
								: [...agents.values()].find(
										(a) => a.agentName === agentName && a.status === "running",
									);
							if (match) {
								// Close any open tool calls in rounds
								const hasOpenTools = match.toolCalls.some((t) => !t.endedAt);
								if (hasOpenTools) {
									const now = Date.now();
									const newCalls = match.toolCalls.map((t) =>
										t.endedAt ? t : { ...t, endedAt: now },
									);
									// Also close in rounds
									const newRounds = match.rounds.map(r => ({
										...r,
										toolCalls: r.toolCalls.map(t =>
											t.endedAt ? t : { ...t, endedAt: now },
										),
									}));
									const newAgents = new Map(agents);
									newAgents.set(match.agentId, { ...match, toolCalls: newCalls, rounds: newRounds });
									usePuxStore.setState({ agents: newAgents });
								}
								const finalStatus = endStatus === "error" ? "error" : "complete";
								usePuxStore.getState().updateAgentStatus(match.agentId, finalStatus, result || parsed.error as string);
							}
							break;
						}
						// ── Error events → visible error text ──

						case "error": {
							const errMsg = (parsed.error as string) || "Unknown error";
							appendText(parts, `\n\n> **Error:** ${errMsg}`);
							statusRef[0] = "complete";
							usePuxStore.setState({ lastError: errMsg });
							break;
						}

						// ── Meta events → Zustand + timing + steps ──

						default: {
							handleMetaEvent(event, parsed, statusRef, parts, stepsRef);
							// Yield snapshot after decision_request so runtime sees requires-action
							if (event === "decision_request") {
								yield buildSnapshot(parts, sources, statusRef[0], timing, stepsRef);
							}
							break;
						}
					}
				}
			}
		} catch (err) {
			if (err instanceof Error && err.name === "AbortError") {
				yield {
					...buildSnapshot(parts, sources, "running", timing, stepsRef),
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
		yield buildSnapshot(parts, sources, "complete", timing, stepsRef);
	},
};
