/**
 * Chat panel — uses ChatState from TUI core (shared code).
 *
 * ChatState is the shared event→message accumulator used by both TUI and web.
 * The SSE transport is browser-native fetch (vs Node.js in TUI), but the
 * state model and event types are identical.
 *
 * What's shared with TUI:
 *   - ChatState (event accumulator)
 *   - ChatMessage/ChatToolCall types
 *   - AgentSessionEvent types
 *
 * What's browser-specific:
 *   - SSE transport (fetch + ReadableStream)
 */

import { LitElement, html, css, nothing } from "lit";
import { customElement, property, state, query } from "lit/decorators.js";
import { ChatState } from "../../../ts-tui-pi/src/core/chat-state.js";
import type { ChatMessage, ChatToolCall } from "../../../ts-tui-pi/src/core/chat-state.js";
import { SSEParser } from "../../../ts-tui-pi/src/core/sse-parser.js";

@customElement("chat-panel")
export class ChatPanel extends LitElement {
	static styles = css`
		:host { display: flex; flex-direction: column; height: 100%; background: var(--bg); }
		.messages { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
		.msg { max-width: 85%; }
		.msg.user { align-self: flex-end; background: var(--surface); border: 1px solid var(--border); border-radius: 12px; padding: 8px 14px; }
		.msg.assistant { align-self: flex-start; }
		.msg .role { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; color: var(--dim); margin-bottom: 4px; }
		.msg .text { font-size: 14px; line-height: 1.5; white-space: pre-wrap; word-break: break-word; }
		.msg .text code { background: var(--surface); padding: 2px 5px; border-radius: 3px; font-size: 13px; }
		.msg .text pre { background: var(--surface); padding: 10px; border-radius: 6px; overflow-x: auto; margin: 8px 0; }
		.msg .text pre code { background: none; padding: 0; }
		.thinking { font-size: 12px; color: var(--dim); font-style: italic; border-left: 2px solid var(--border); padding-left: 8px; margin: 4px 0; }
		.tool { background: var(--surface); border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; margin: 4px 0; font-size: 12px; }
		.tool .name { font-weight: 600; color: var(--accent); }
		.tool .status { float: right; }
		.tool .status.running { color: var(--warn); }
		.tool .status.done { color: var(--success); }
		.tool .status.error { color: var(--error); }
		.tool .args { color: var(--dim); margin-top: 2px; max-height: 60px; overflow: hidden; }
		.input-bar { display: flex; padding: 12px; border-top: 1px solid var(--border); background: var(--surface); gap: 8px; }
		.input-bar input { flex: 1; background: var(--bg); border: 1px solid var(--border); border-radius: 8px; padding: 8px 12px; color: var(--text); font-size: 14px; outline: none; }
		.input-bar input:focus { border-color: var(--accent); }
		.input-bar button { background: var(--accent); color: white; border: none; border-radius: 8px; padding: 8px 16px; cursor: pointer; font-size: 13px; font-weight: 600; }
		.input-bar button:disabled { opacity: 0.5; cursor: not-allowed; }
		.streaming-cursor { display: inline-block; width: 6px; height: 14px; background: var(--accent); animation: blink 1s infinite; vertical-align: text-bottom; margin-left: 2px; }
		@keyframes blink { 50% { opacity: 0; } }
		.empty { flex:1; display:flex; align-items:center; justify-content:center; color:var(--dim); font-size:13px; }
		.subagent { font-size: 11px; color: var(--dim); padding: 4px 0 4px 12px; border-left: 2px solid var(--accent); margin: 4px 0; }
		.subagent .name { color: var(--accent); font-weight: 600; }
		.subagent .task { color: var(--text); }
		.compaction { font-size: 11px; color: var(--dim); font-style: italic; padding: 2px 8px; }
	`;

	@property() serverUrl = "";
	@property() project = "ts-tui-pi";
	@state() private messages: ChatMessage[] = [];
	@state() private streaming = false;
	@state() private inputText = "";
	@query("input") private inputEl!: HTMLInputElement;

	private chatState = new ChatState();
	private abortCtrl: AbortController | undefined;

	render() {
		return html`
			<div class="messages">
				${this.messages.length === 0
					? html`<div class="empty">Send a message to start</div>`
					: this.messages.map(m => this.renderMessage(m))
				}
			</div>
			<div class="input-bar">
				<input
					type="text"
					placeholder="Ask Pux anything..."
					.value=${this.inputText}
					@input=${(e: Event) => { this.inputText = (e.target as HTMLInputElement).value; }}
					@keydown=${(e: KeyboardEvent) => { if (e.key === "Enter" && !e.shiftKey) this.send(); }}
					?disabled=${this.streaming}
				/>
				<button @click=${this.send} ?disabled=${this.streaming || !this.inputText.trim()}>
					${this.streaming ? "..." : "Send"}
				</button>
			</div>
		`;
	}

	private renderMessage(m: ChatMessage) {
		return html`
			<div class="msg ${m.role}">
				${m.thinking ? html`<div class="thinking">${m.thinking}</div>` : nothing}
				<div class="text">${m.text}${m.role === "assistant" && this.streaming ? html`<span class="streaming-cursor"></span>` : nothing}</div>
				${m.tools.length > 0 ? m.tools.map(t => this.renderTool(t)) : nothing}
			</div>
		`;
	}

	private renderTool(t: ChatToolCall) {
		return html`
			<div class="tool">
				<span class="name">${t.name}</span>
				<span class="status ${t.status}">${t.status === "running" ? "⟳" : t.status === "done" ? "✓" : "✗"}</span>
				${t.args ? html`<div class="args">${JSON.stringify(t.args).slice(0, 120)}</div>` : nothing}
			</div>
		`;
	}

	private accText = "";
	private accThinking = "";
	@state() private subAgents: Array<{ name: string; task: string; status: string }> = [];
	@state() private compacting = false;

	private async send() {
		const text = this.inputText.trim();
		if (!text || this.streaming) return;
		this.inputText = "";
		this.requestUpdate();

		// Emit user message into ChatState (same lifecycle as PuxAgentSession)
		this.chatState.handleEvent({
			type: "message_start",
			message: { role: "user", content: [{ type: "text", text }] },
		} as any);
		// Emit assistant message start
		this.chatState.handleEvent({
			type: "message_start",
			message: { role: "assistant", content: [] },
		} as any);
		this.syncFromState();

		this.accText = "";
		this.accThinking = "";
		this.abortCtrl = new AbortController();

		try {
			const resp = await fetch(`${this.serverUrl}/api/pux/prompt`, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ message: text, project: this.project }),
				signal: this.abortCtrl.signal,
			});

			if (!resp.ok) {
				const errText = await resp.text().catch(() => "");
				throw new Error(`Backend ${resp.status}${errText ? ": " + errText.slice(0, 200) : ""}`);
			}
			if (!resp.body) throw new Error("No response body");

			// Shared SSE parser — same code as TUI
			const reader = resp.body.getReader();
			const decoder = new TextDecoder();
			const parser = new SSEParser();

			while (true) {
				const { done, value } = await reader.read();
				if (done) break;
				const events = parser.feed(decoder.decode(value, { stream: true }));
				for (const evt of events) {
					this.handleSSE(evt.event, evt.data);
				}
			}
		} catch (err: any) {
			if (err.name !== "AbortError") {
				let msg = err.message || String(err);
				if (err.cause?.code === "ECONNREFUSED" || msg.includes("Connection refused")) {
					msg = `Backend not running at ${this.serverUrl}. Start with: task dev`;
				}
				this.chatState.handleEvent({
					type: "message_end",
					message: {
						role: "assistant",
						content: [{ type: "text", text: `Error: ${msg}` }],
						stopReason: "error",
						errorMessage: msg,
					},
				} as any);
			}
		} finally {
			this.chatState.handleEvent({ type: "agent_end", messages: [] } as any);
			this.syncFromState();
		}
	}

	/**
	 * Map SSE events to ChatState — shared event handling logic.
	 * Covers the same events as PuxAgentSession.handleSSE() in the TUI.
	 */
	private handleSSE(eventType: string, payload: any) {
		switch (eventType) {
			case "agent_spawned":
				// Agent ID from backend — track for session continuity
				break;

			case "text_delta":
				this.accText += payload.text || "";
				this.chatState.handleEvent({
					type: "message_update",
					message: { role: "assistant", content: [{ type: "text", text: this.accText }] },
				} as any);
				break;

			case "thinking_delta":
				this.accThinking += payload.text || "";
				this.chatState.handleEvent({
					type: "message_update",
					message: { role: "assistant", content: [{ type: "thinking", thinking: this.accThinking }] },
				} as any);
				break;

			case "tool_execution_start":
				this.chatState.handleEvent({
					type: "tool_execution_start",
					toolCallId: payload.toolId || `t_${Date.now()}`,
					toolName: payload.toolName || "tool",
					args: payload.args,
				} as any);
				break;

			case "tool_execution_end":
				this.chatState.handleEvent({
					type: "tool_execution_end",
					toolCallId: payload.toolId || "",
					isError: !!payload.error,
					result: payload.result,
				} as any);
				break;

			case "tool_update":
				// Partial tool result — update status in place
				break;

			case "agent_end":
				this.chatState.handleEvent({
					type: "message_end",
					message: {
						role: "assistant",
						content: [],
						stopReason: "stop",
						usage: { input: Number(payload.input) || 0, output: Number(payload.output) || 0 },
					},
				} as any);
				break;

			case "error":
				this.chatState.handleEvent({
					type: "message_end",
					message: {
						role: "assistant",
						content: [],
						stopReason: "error",
						errorMessage: payload.error || payload.message || "Unknown error",
					},
				} as any);
				break;

			case "compaction_start":
				this.compacting = true;
				this.requestUpdate();
				break;

			case "compaction_end":
				this.compacting = false;
				this.requestUpdate();
				break;

			case "subagent_start": {
				const name = payload.agentName || "agent";
				this.subAgents = [...this.subAgents, { name, task: payload.task || "", status: "running" }];
				this.requestUpdate();
				break;
			}

			case "subagent_end": {
				const endName = payload.agentName || "agent";
				this.subAgents = this.subAgents.map(sa =>
					sa.name === endName ? { ...sa, status: payload.status || "completed" } : sa
				);
				this.requestUpdate();
				break;
			}

			case "user_question":
			case "hook_request":
			case "grind_attempt":
			case "grind_verify":
			case "grind_end":
			case "step_start":
			case "step_end":
			case "artifact_created":
			case "artifact_updated":
			case "plan_created":
			case "plan_updated":
			case "approval_request":
				// Forward-compatible: acknowledge without crashing on new event types
				break;
		}
		this.syncFromState();
	}

	private syncFromState() {
		this.messages = [...this.chatState.messages];
		this.streaming = this.chatState.streaming;
		this.requestUpdate();
		this.scrollToBottom();
	}

	private scrollToBottom() {
		requestAnimationFrame(() => {
			const el = this.shadowRoot?.querySelector(".messages");
			if (el) el.scrollTop = el.scrollHeight;
		});
	}

	/** Load conversation history from backend (called by parent on sidebar click) */
	loadHistory(msgs: any[]) {
		// Reset state and rebuild from history
		this.chatState = new ChatState();
		this.accText = "";
		this.accThinking = "";

		for (const m of msgs) {
			const role = m.role === "user" ? "user" : "assistant";
			const text = m.content || "";
			this.chatState.messages.push({
				role,
				text,
				tools: [],
			});
		}
		this.syncFromState();
	}
}
