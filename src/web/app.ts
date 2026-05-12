/**
 * Pux Web SPA — minimal Lit app.
 *
 * Imports PuxAgentSession from TUI codebase (shared SSE bridge).
 * Renders chat + browser visual + scheduler panel.
 */

import { LitElement, html, css } from "lit";
import { customElement, state } from "lit/decorators.js";
import "./components/chat-panel.js";
import "./components/browser-panel.js";
import "./components/scheduler-panel.js";

type Tab = "chat" | "automate" | "pilot";

@customElement("pux-app")
export class PuxApp extends LitElement {
	static styles = css`
		:host { display: flex; flex-direction: column; height: 100vh; }
		.topbar { height: 40px; display: flex; align-items: center; padding: 0 12px; border-bottom: 1px solid var(--border); background: var(--surface); gap: 4px; flex-shrink: 0; }
		.topbar .brand { font-weight: 700; font-size: 13px; color: var(--accent); margin-right: 12px; }
		.tab-btn { background: none; border: none; color: var(--dim); font-size: 12px; padding: 4px 10px; cursor: pointer; border-radius: 4px; }
		.tab-btn:hover { color: var(--text); background: var(--border); }
		.tab-btn.active { color: var(--accent); background: rgba(59,130,246,0.1); }
		.main { flex: 1; overflow: hidden; display: flex; }
		.sidebar { width: 220px; border-right: 1px solid var(--border); overflow-y: auto; background: var(--surface); flex-shrink: 0; }
		.content { flex: 1; overflow: hidden; }
		.sidebar-title { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.1em; color: var(--dim); padding: 10px 12px 6px; }
	`;

	@state() private activeTab: Tab = "chat";
	@state() private serverUrl = "http://localhost:3847";

	render() {
		return html`
			<div class="topbar">
				<span class="brand">Pux</span>
				<button class="tab-btn ${this.activeTab === "chat" ? "active" : ""}" @click=${() => { this.activeTab = "chat"; }}>Chat</button>
				<button class="tab-btn ${this.activeTab === "automate" ? "active" : ""}" @click=${() => { this.activeTab = "automate"; }}>Automate</button>
				<button class="tab-btn ${this.activeTab === "pilot" ? "active" : ""}" @click=${() => { this.activeTab = "pilot"; }}>Pilot</button>
			</div>
			<div class="main">
				${this.activeTab === "chat" ? html`
					<chat-panel serverUrl=${this.serverUrl}></chat-panel>
				` : null}
				${this.activeTab === "automate" ? html`
					<scheduler-panel serverUrl=${this.serverUrl}></scheduler-panel>
				` : null}
				${this.activeTab === "pilot" ? html`
					<div style="display:flex;width:100%;height:100%;">
						<chat-panel serverUrl=${this.serverUrl} style="flex:1;"></chat-panel>
						<browser-panel serverUrl=${this.serverUrl}></browser-panel>
					</div>
				` : null}
			</div>
		`;
	}
}
