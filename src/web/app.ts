/**
 * Pux Web SPA — sidebar + main layout.
 *
 * Layout (from design plan):
 *
 * ┌──────────┬───────────────────────────────────────────┐
 * │  Pux     │  Browser / Desktop Visual (top strip)     │
 * │          ├───────────────────────────────────────────┤
 * │  Chat    │                                           │
 * │  History │  Chat messages + tool calls               │
 * │          │  (scrollable, fills remaining space)      │
 * │          ├───────────────────────────────────────────┤
 * │  ⚙ Jobs  │  Input: ask me anything...                │
 * └──────────┴───────────────────────────────────────────┘
 *
 * Left sidebar: branding + scheduler summary
 * Right main:   browser visual (top) → chat (middle) → input (bottom)
 */

import { LitElement, html, css } from "lit";
import { customElement, state } from "lit/decorators.js";
import "./components/chat-panel.js";
import "./components/browser-panel.js";
import "./components/scheduler-panel.js";

@customElement("pux-app")
export class PuxApp extends LitElement {
	static styles = css`
		:host { display: flex; height: 100vh; overflow: hidden; }

		/* Left sidebar */
		.sidebar {
			width: 220px;
			flex-shrink: 0;
			display: flex;
			flex-direction: column;
			border-right: 1px solid var(--border);
			background: var(--surface);
		}
		.sidebar-brand {
			height: 36px;
			display: flex;
			align-items: center;
			padding: 0 14px;
			border-bottom: 1px solid var(--border);
			flex-shrink: 0;
		}
		.sidebar-brand .name { font-weight: 700; font-size: 14px; color: var(--accent); }
		.sidebar-chat-history {
			flex: 1;
			overflow-y: auto;
			padding: 8px;
		}
		.sidebar-chat-history .empty {
			color: var(--dim);
			font-size: 12px;
			text-align: center;
			padding: 24px 8px;
		}
		.sidebar-bottom {
			border-top: 1px solid var(--border);
			flex-shrink: 0;
		}

		/* Right main area */
		.main {
			flex: 1;
			min-width: 0;
			display: flex;
			flex-direction: column;
		}

		/* Browser strip at top of main */
		.browser-strip {
			height: 220px;
			flex-shrink: 0;
			border-bottom: 1px solid var(--border);
			overflow: hidden;
		}

		/* Chat fills remaining space */
		.chat-area {
			flex: 1;
			min-height: 0;
		}
	`;

	render() {
		return html`
			<div class="sidebar">
				<div class="sidebar-brand">
					<span class="name">Pux</span>
				</div>
				<div class="sidebar-chat-history">
					<div class="empty">Sessions appear here</div>
				</div>
				<div class="sidebar-bottom">
					<scheduler-panel></scheduler-panel>
				</div>
			</div>
			<div class="main">
				<div class="browser-strip">
					<browser-panel></browser-panel>
				</div>
				<div class="chat-area">
					<chat-panel></chat-panel>
				</div>
			</div>
		`;
	}
}
