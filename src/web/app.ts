/**
 * Pux Web SPA — single page, all panels visible at once.
 *
 * Layout: chat fills the main area, browser panel slides in from the
 * right when a sandbox is active, scheduler lives in a collapsible
 * bottom drawer. No tabs — everything on one page.
 */

import { LitElement, html, css } from "lit";
import { customElement, state } from "lit/decorators.js";
import "./components/chat-panel.js";
import "./components/browser-panel.js";
import "./components/scheduler-panel.js";

@customElement("pux-app")
export class PuxApp extends LitElement {
	static styles = css`
		:host { display: flex; flex-direction: column; height: 100vh; }
		.topbar { height: 36px; display: flex; align-items: center; padding: 0 12px; border-bottom: 1px solid var(--border); background: var(--surface); flex-shrink: 0; gap: 8px; }
		.topbar .brand { font-weight: 700; font-size: 13px; color: var(--accent); }
		.topbar .spacer { flex: 1; }
		.topbar button { background: none; border: 1px solid var(--border); color: var(--dim); border-radius: 4px; padding: 3px 8px; cursor: pointer; font-size: 11px; }
		.topbar button:hover { color: var(--text); background: var(--border); }
		.topbar button.active { color: var(--accent); border-color: var(--accent); }
		.body { flex: 1; display: flex; overflow: hidden; }
		.chat-area { flex: 1; min-width: 0; }
		.browser-area { width: 380px; border-left: 1px solid var(--border); flex-shrink: 0; transition: width 0.2s; overflow: hidden; }
		.browser-area.hidden { width: 0; border: none; }
		.drawer { border-top: 1px solid var(--border); background: var(--surface); flex-shrink: 0; overflow: hidden; transition: height 0.2s; }
		.drawer.open { height: 240px; }
		.drawer.closed { height: 0; }
	`;

	@state() private showBrowser = false;
	@state() private showScheduler = false;

	render() {
		return html`
			<div class="topbar">
				<span class="brand">Pux</span>
				<div class="spacer"></div>
				<button class="${this.showBrowser ? "active" : ""}" @click=${() => { this.showBrowser = !this.showBrowser; }}>Browser</button>
				<button class="${this.showScheduler ? "active" : ""}" @click=${() => { this.showScheduler = !this.showScheduler; }}>Scheduler</button>
			</div>
			<div class="body">
				<div class="chat-area">
					<chat-panel></chat-panel>
				</div>
				<div class="browser-area ${this.showBrowser ? "" : "hidden"}">
					<browser-panel></browser-panel>
				</div>
			</div>
			<div class="drawer ${this.showScheduler ? "open" : "closed"}">
				<scheduler-panel></scheduler-panel>
			</div>
		`;
	}
}
