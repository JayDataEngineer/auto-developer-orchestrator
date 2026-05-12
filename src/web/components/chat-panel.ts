/**
 * Chat panel — thin HTML render layer over PuxAgentSession + ChatState.
 *
 * Uses the exact same session and state accumulator as the TUI.
 * Only rendering differs (HTML vs terminal).
 */

import { LitElement, html, css, nothing } from "lit";
import { customElement, property, state, query } from "lit/decorators.js";
import { PuxAgentSession } from "../../../ts-tui-pi/src/core/pux-agent-session.js";
import { ChatState } from "../../../ts-tui-pi/src/core/chat-state.js";
import { SettingsManager } from "../../../ts-tui-pi/src/core/settings-manager.js";
import type { AgentSessionEvent } from "../../../ts-tui-pi/src/core/agent-session.js";
import type { ChatMessage, ChatToolCall } from "../../../ts-tui-pi/src/core/chat-state.js";

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
	`;

	@property() serverUrl = "http://localhost:3847";
	@property() project = "ts-tui-pi";
	@state() private messages: ChatMessage[] = [];
	@state() private streaming = false;
	@state() private inputText = "";
	@query("input") private inputEl!: HTMLInputElement;

	private session!: PuxAgentSession;
	private chatState = new ChatState();

	connectedCallback() {
		super.connectedCallback();
		const settings = SettingsManager.inMemory();
		this.session = new PuxAgentSession(settings, {}, this.serverUrl, this.project, "");
		this.session.subscribe((event: AgentSessionEvent) => {
			this.chatState.handleEvent(event);
			this.syncFromState();
		});
	}

	disconnectedCallback() {
		super.disconnectedCallback();
		this.session.dispose();
	}

	private syncFromState() {
		this.messages = [...this.chatState.messages];
		this.streaming = this.chatState.streaming;
		this.requestUpdate();
		this.scrollToBottom();
	}

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

	private send() {
		const text = this.inputText.trim();
		if (!text || this.streaming) return;
		this.inputText = "";
		this.requestUpdate();
		this.session.prompt(text);
	}

	private scrollToBottom() {
		requestAnimationFrame(() => {
			const el = this.shadowRoot?.querySelector(".messages");
			if (el) el.scrollTop = el.scrollHeight;
		});
	}
}
