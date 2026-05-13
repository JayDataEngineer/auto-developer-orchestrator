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

		/* Single scroll container for messages + sticky input */
		.scroll-area {
			flex: 1; overflow-y: auto; display: flex; flex-direction: column;
			padding: 0 16px;
		}

		/* Messages column — grows to fill space, pushes input to bottom */
		.messages { flex: 1; display: flex; flex-direction: column; gap: 12px; padding-top: 16px; }

		/* Centered empty state */
		.empty-state {
			flex: 1; display: flex; flex-direction: column;
			align-items: center; justify-content: center; gap: 8px;
			padding-bottom: 48px;
		}
		.empty-state h2 { font-size: 22px; font-weight: 600; color: var(--text); letter-spacing: -0.01em; }
		.empty-state p { font-size: 14px; color: var(--dim); }

		/* Message bubbles — fixed max-width for readability */
		.msg { max-width: 48rem; width: 100%; margin: 0 auto; }
		.msg.user { background: var(--surface); border: 1px solid var(--border); border-radius: 12px; padding: 8px 14px; }
		.msg.assistant {}
		.msg .role { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; color: var(--dim); margin-bottom: 4px; }
		.msg .text { font-size: 14px; line-height: 1.5; white-space: pre-wrap; word-break: break-word; }
		.msg .text code { background: var(--surface); padding: 2px 5px; border-radius: 3px; font-size: 13px; }
		.msg .text pre { background: var(--surface); padding: 10px; border-radius: 6px; overflow-x: auto; margin: 8px 0; }
		.msg .text pre code { background: none; padding: 0; }
		/* Collapsible thinking block */
		.thinking-block { margin: 4px 0; }
		.thinking-toggle {
			display: inline-flex; align-items: center; gap: 4px;
			background: none; border: 1px solid var(--border); border-radius: 4px;
			color: var(--dim); font-size: 11px; padding: 2px 8px; cursor: pointer;
			transition: color 0.15s, border-color 0.15s;
		}
		.thinking-toggle:hover { color: var(--text); border-color: var(--dim); }
		.thinking-toggle .chevron { display: inline-block; transition: transform 0.15s; font-size: 10px; }
		.thinking-toggle .chevron.open { transform: rotate(90deg); }
		.thinking-content {
			font-size: 12px; color: var(--dim); font-style: italic;
			border-left: 2px solid var(--border); padding-left: 8px; margin: 6px 0 2px;
			max-height: 300px; overflow-y: auto; white-space: pre-wrap; word-break: break-word;
		}
		.tool { background: var(--surface); border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; margin: 4px 0; font-size: 12px; }
		.tool .name { font-weight: 600; color: var(--accent); }
		.tool .status { float: right; }
		.tool .status.running { color: var(--warn); }
		.tool .status.done { color: var(--success); }
		.tool .status.error { color: var(--error); }
		.tool .args { color: var(--dim); margin-top: 2px; max-height: 60px; overflow: hidden; }

		/* Sticky input — floats at bottom of scroll container */
		.input-dock {
			position: sticky; bottom: 0; padding: 8px 0 16px;
			background: linear-gradient(transparent 0%, var(--bg) 24px);
		}
		.input-dock-inner { position: relative; max-width: 48rem; margin: 0 auto; }
		.input-box {
			display: flex; align-items: flex-end; gap: 0;
			background: var(--surface); border: 1px solid var(--border);
			border-radius: 16px; padding: 8px 8px 8px 16px;
			transition: border-color 0.15s;
		}
		.input-box:focus-within { border-color: var(--dim); }
		.input-box textarea {
			flex: 1; background: none; border: none; outline: none;
			color: var(--text); font-size: 14px; line-height: 1.5;
			resize: none; overflow-y: auto; max-height: 200px;
			padding: 4px 0; font-family: inherit;
		}
		.input-box textarea::placeholder { color: var(--dim); }
		.send-btn {
			background: var(--accent); color: white; border: none;
			border-radius: 10px; width: 32px; height: 32px; min-width: 32px;
			display: flex; align-items: center; justify-content: center;
			cursor: pointer; transition: opacity 0.15s;
		}
		.send-btn:disabled { opacity: 0.3; cursor: not-allowed; }
		.send-btn svg { width: 16px; height: 16px; }
		.streaming-cursor { display: inline-block; width: 6px; height: 14px; background: var(--accent); animation: blink 1s infinite; vertical-align: text-bottom; margin-left: 2px; }
		@keyframes blink { 50% { opacity: 0; } }

		/* Slash command popup */
		.slash-list {
			position: absolute; bottom: 100%; left: 0; right: 0;
			background: var(--surface); border: 1px solid var(--border); border-radius: 8px;
			max-height: 240px; overflow-y: auto; z-index: 10;
			box-shadow: 0 -4px 12px rgba(0,0,0,0.3);
			margin-bottom: 4px;
		}
		.slash-item { padding: 6px 12px; cursor: pointer; display: flex; gap: 10px; font-size: 13px; }
		.slash-item:hover, .slash-item.active { background: var(--border); }
		.slash-item .cmd { color: var(--accent); font-weight: 600; min-width: 90px; }
		.slash-item .desc { color: var(--dim); }

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
	@state() private slashOpen = false;
	@state() private slashIndex = 0;
	@state() private thinkingExpanded = new Set<number>();
	@query("textarea") private textareaEl!: HTMLTextAreaElement;

	private slashFilter = "";

	private chatState = new ChatState();
	private abortCtrl: AbortController | undefined;

	private static COMMANDS = [
		{ name: "scheduler", desc: "Open scheduler panel" },
		{ name: "new", desc: "Start a new session" },
		{ name: "compact", desc: "Compact the session context" },
		{ name: "session", desc: "Show session info and stats" },
		{ name: "export", desc: "Export session as HTML" },
		{ name: "model", desc: "Show current model" },
		{ name: "help", desc: "Show available commands" },
	];

	render() {
		const filtered = this.getFilteredCommands();
		const isEmpty = this.messages.length === 0 && !this.streaming;
		return html`
			<div class="scroll-area">
				${isEmpty ? html`
					<div class="empty-state">
						<h2>What can I help with?</h2>
						<p>Type a message to start</p>
					</div>
				` : html`
					<div class="messages">
						${this.messages.map((m, i) => this.renderMessage(m, i))}
					</div>
				`}

				<div class="input-dock">
					<div class="input-dock-inner">
						${this.slashOpen && filtered.length > 0 ? html`
							<div class="slash-list">
								${filtered.map((c, i) => html`
									<div class="slash-item ${i === this.slashIndex ? 'active' : ''}"
										@click=${() => this.pickCommand(c.name)}>
										<span class="cmd">/${c.name}</span>
										<span class="desc">${c.desc}</span>
									</div>
								`)}
							</div>
						` : nothing}
						<div class="input-box">
							<textarea
								placeholder="Message Pux..."
								rows="1"
								.value=${this.inputText}
								@input=${this.onInput}
								@keydown=${this.onKeyDown}
								?disabled=${this.streaming}
							></textarea>
							<button class="send-btn" @click=${this.send} ?disabled=${this.streaming || !this.inputText.trim()}>
								<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
									${this.streaming
										? html`<rect x="6" y="6" width="12" height="12" rx="1"/>`
										: html`<line x1="12" y1="19" x2="12" y2="5"/><polyline points="5 12 12 5 19 12"/>`
									}
								</svg>
							</button>
						</div>
					</div>
				</div>
			</div>
		`;
	}

	private renderMessage(m: ChatMessage, idx: number) {
		const isOpen = this.thinkingExpanded.has(idx);
		return html`
			<div class="msg ${m.role}">
				${m.thinking ? html`
					<div class="thinking-block">
						<button class="thinking-toggle" @click=${() => this.toggleThinking(idx)}>
							<span class="chevron ${isOpen ? 'open' : ''}">▸</span>
							Thinking${this.streaming && m.role === "assistant" && idx === this.messages.length - 1 ? "..." : ""}
						</button>
						${isOpen ? html`<div class="thinking-content">${m.thinking}</div>` : nothing}
					</div>
				` : nothing}
				<div class="text">${m.text}${m.role === "assistant" && this.streaming ? html`<span class="streaming-cursor"></span>` : nothing}</div>
				${m.tools.length > 0 ? m.tools.map(t => this.renderTool(t)) : nothing}
			</div>
		`;
	}

	private toggleThinking(idx: number) {
		const next = new Set(this.thinkingExpanded);
		if (next.has(idx)) next.delete(idx);
		else next.add(idx);
		this.thinkingExpanded = next;
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

	private getFilteredCommands() {
		if (!this.slashOpen) return [];
		const q = this.slashFilter.toLowerCase();
		return ChatPanel.COMMANDS.filter(c => c.name.includes(q));
	}

	private onInput(e: Event) {
		const el = e.target as HTMLTextAreaElement;
		this.inputText = el.value;
		// Auto-grow textarea
		el.style.height = "auto";
		el.style.height = el.scrollHeight + "px";
		if (this.inputText.startsWith("/")) {
			this.slashOpen = true;
			this.slashFilter = this.inputText.slice(1);
			this.slashIndex = 0;
		} else {
			this.slashOpen = false;
		}
	}

	private onKeyDown(e: KeyboardEvent) {
		const filtered = this.getFilteredCommands();
		if (this.slashOpen && filtered.length > 0) {
			if (e.key === "ArrowDown") {
				e.preventDefault();
				this.slashIndex = (this.slashIndex + 1) % filtered.length;
				return;
			}
			if (e.key === "ArrowUp") {
				e.preventDefault();
				this.slashIndex = (this.slashIndex - 1 + filtered.length) % filtered.length;
				return;
			}
			if (e.key === "Enter" || e.key === "Tab") {
				e.preventDefault();
				this.pickCommand(filtered[this.slashIndex].name);
				return;
			}
			if (e.key === "Escape") {
				this.slashOpen = false;
				return;
			}
		}
		if (e.key === "Enter" && !e.shiftKey) this.send();
	}

	private pickCommand(name: string) {
		this.slashOpen = false;
		this.inputText = "";
		switch (name) {
			case "scheduler":
				this.dispatchEvent(new CustomEvent("toggle-scheduler", { bubbles: true, composed: true }));
				break;
			case "new":
				this.reset();
				break;
			case "compact":
				this.sendBackendCommand("compact");
				break;
			case "model":
				this.showLocalMessage(`Current model: ${this.project}\nUse the sidebar to switch projects.`);
				break;
			case "session":
				this.showLocalMessage(`Session: ${this.messages.length} messages\nStreaming: ${this.streaming}`);
				break;
			case "export":
				this.exportChat();
				break;
			case "help":
				this.showHelp();
				break;
		}
	}

	private showLocalMessage(text: string) {
		this.chatState.messages.push({ role: "assistant", text, tools: [] });
		this.syncFromState();
	}

	private exportChat() {
		const lines = this.messages.map(m => {
			const prefix = m.role === "user" ? "You" : "Pux";
			return `${prefix}: ${m.text}`;
		}).join("\n\n");
		const blob = new Blob([lines], { type: "text/plain" });
		const url = URL.createObjectURL(blob);
		const a = document.createElement("a");
		a.href = url; a.download = `pux-chat-${Date.now()}.txt`;
		a.click();
		URL.revokeObjectURL(url);
	}

	private showHelp() {
		this.chatState.messages.push({
			role: "assistant",
			text: ChatPanel.COMMANDS.map(c => `/${c.name} — ${c.desc}`).join("\n"),
			tools: [],
		});
		this.syncFromState();
	}

	private async sendBackendCommand(cmd: string) {
		// Only used by /compact — sends to backend and streams SSE
		const text = `/${cmd}`;
		this._sending = true;
		this.chatState.handleEvent({ type: "message_start", message: { role: "user", content: [{ type: "text", text }] } } as any);
		this.chatState.handleEvent({ type: "message_start", message: { role: "assistant", content: [] } } as any);
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
			if (!resp.ok) throw new Error(`Backend ${resp.status}`);
			if (!resp.body) throw new Error("No response body");
			const reader = resp.body.getReader();
			const decoder = new TextDecoder();
			const parser = new SSEParser();
			const READ_TIMEOUT = 60_000;
			while (true) {
				const readPromise = reader.read();
				const timeout = new Promise<never>((_, reject) =>
					setTimeout(() => reject(new Error("Stream timeout")), READ_TIMEOUT)
				);
				const { done, value } = await Promise.race([readPromise, timeout]);
				if (done) break;
				for (const evt of parser.feed(decoder.decode(value, { stream: true }))) {
					this.handleSSE(evt.event, evt.data);
				}
			}
		} catch (err: any) {
			if (err.name !== "AbortError") {
				this.chatState.handleEvent({
					type: "message_end",
					message: { role: "assistant", content: [{ type: "text", text: `Error: ${err.message}` }], stopReason: "error", errorMessage: err.message },
				} as any);
			}
		} finally {
			this._sending = false;
			this.chatState.handleEvent({ type: "agent_end", messages: [] } as any);
			this.syncFromState();
		}
	}

	/** Reset to a fresh session state */
	reset() {
		this.chatState = new ChatState();
		this.accText = "";
		this.accThinking = "";
		this.subAgents = [];
		this.compacting = false;
		this.abortCtrl?.abort();
		this.abortCtrl = undefined;
		this.syncFromState();
	}

	private _sending = false;
	private async send() {
		const text = this.inputText.trim();
		if (!text || this.streaming || this._sending) return;

		// Slash command — route to pickCommand instead of backend
		if (text.startsWith("/")) {
			const name = text.slice(1).split(/\s/)[0];
			this.inputText = "";
			this.slashOpen = false;
			this.requestUpdate();
			requestAnimationFrame(() => { if (this.textareaEl) this.textareaEl.style.height = "auto"; });
			this.pickCommand(name);
			return;
		}

		this._sending = true;
		this.inputText = "";
		this.requestUpdate();
		// Reset textarea height
		requestAnimationFrame(() => { if (this.textareaEl) this.textareaEl.style.height = "auto"; });

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

			// Read timeout: if no data for 60s, assume stuck (model down, etc.)
			const READ_TIMEOUT = 60_000;
			while (true) {
				const readPromise = reader.read();
				const timeout = new Promise<never>((_, reject) =>
					setTimeout(() => reject(new Error("Stream timeout — no data from backend. Is the model running?")), READ_TIMEOUT)
				);
				const { done, value } = await Promise.race([readPromise, timeout]);
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
			this._sending = false;
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
				// Auto-expand thinking while streaming
				{
					const lastIdx = this.messages.length - 1;
					if (lastIdx >= 0 && !this.thinkingExpanded.has(lastIdx)) {
						this.thinkingExpanded = new Set(this.thinkingExpanded).add(lastIdx);
					}
				}
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
			const el = this.shadowRoot?.querySelector(".scroll-area");
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
