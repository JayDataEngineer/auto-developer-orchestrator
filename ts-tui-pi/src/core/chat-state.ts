/**
 * ChatState — shared message model accumulator for AgentSessionEvents.
 *
 * Both TUI and web panels subscribe to PuxAgentSession events and
 * use this to track the same chat state. Only rendering differs
 * (terminal components vs HTML elements).
 *
 * Usage:
 *   const state = new ChatState();
 *   session.subscribe(e => state.handleEvent(e));
 *   // Read state.messages, state.streaming, etc.
 */

import type { AgentSessionEvent } from "./agent-session.js";

export interface ChatToolCall {
	id: string;
	name: string;
	args?: any;
	status: "running" | "done" | "error";
	result?: any;
}

export interface ChatMessage {
	role: "user" | "assistant" | "error";
	text: string;
	thinking?: string;
	tools: ChatToolCall[];
	errorMessage?: string;
}

export class ChatState {
	messages: ChatMessage[] = [];
	streaming = false;

	private currentAssistant: ChatMessage | null = null;
	private accText = "";
	private accThinking = "";
	private toolIndex = new Map<string, ChatToolCall>();

	handleEvent(event: AgentSessionEvent): void {
		switch (event.type) {
			case "message_start": {
				const msg = (event as any).message;
				if (msg?.role === "user") {
					const text = msg.content?.[0]?.text || "";
					this.messages = [...this.messages, { role: "user", text, tools: [] }];
				} else if (msg?.role === "assistant") {
					const assistant: ChatMessage = { role: "assistant", text: "", tools: [] };
					this.messages = [...this.messages, assistant];
					this.currentAssistant = assistant;
					this.streaming = true;
					this.accText = "";
					this.accThinking = "";
				}
				break;
			}

			case "message_update": {
				if (!this.currentAssistant) break;
				const content = (event as any).message?.content || [];
				for (const c of content) {
					if (c.type === "text") this.accText = c.text;
					if (c.type === "thinking") this.accThinking = c.thinking;
					if (c.type === "toolCall") {
						if (!this.toolIndex.has(c.id)) {
							const tool: ChatToolCall = {
								id: c.id,
								name: c.name || "tool",
								args: c.arguments,
								status: "running",
							};
							this.currentAssistant.tools = [...this.currentAssistant.tools, tool];
							this.toolIndex.set(c.id, tool);
						}
					}
				}
				this.syncAssistant();
				break;
			}

			case "message_end": {
				const msg = (event as any).message;
				if (msg?.content) {
					for (const c of msg.content) {
						if (c.type === "text") this.accText = c.text;
						if (c.type === "thinking") this.accThinking = c.thinking;
					}
					this.syncAssistant();
				}
				if (msg?.errorMessage && this.currentAssistant) {
					this.currentAssistant.errorMessage = msg.errorMessage;
				}
				if (msg?.stopReason === "error" && this.currentAssistant) {
					this.currentAssistant.role = "error";
					this.currentAssistant.errorMessage = msg.errorMessage || "Unknown error";
				}
				this.currentAssistant = null;
				this.toolIndex.clear();
				break;
			}

			case "tool_execution_start": {
				// Sub-agent tool events (with agentName) come outside message_update
				const e = event as any;
				if (!this.currentAssistant) break;
				const id = e.toolCallId || `ext_${Date.now()}`;
				if (!this.toolIndex.has(id)) {
					const tool: ChatToolCall = {
						id,
						name: e.toolName || "tool",
						args: e.args,
						status: "running",
					};
					this.currentAssistant.tools = [...this.currentAssistant.tools, tool];
					this.toolIndex.set(id, tool);
					this.messages = [...this.messages];
				}
				break;
			}

			case "tool_execution_end": {
				const e = event as any;
				const id = e.toolCallId;
				const tool = id ? this.toolIndex.get(id) : null;
				if (tool) {
					tool.status = e.isError ? "error" : "done";
					tool.result = e.result;
					this.messages = [...this.messages];
				}
				break;
			}

			case "agent_end": {
				this.streaming = false;
				break;
			}
		}
	}

	private syncAssistant() {
		if (!this.currentAssistant) return;
		this.currentAssistant.text = this.accText;
		this.currentAssistant.thinking = this.accThinking || undefined;
		this.messages = [...this.messages];
	}
}
