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
			display: flex;
			flex-direction: column;
			border-right: none;
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

		/* Resize handles */
		.resize-h {
			width: 5px;
			cursor: col-resize;
			background: transparent;
			flex-shrink: 0;
			transition: background 0.15s;
		}
		.resize-h:hover, .resize-h.active { background: var(--accent); }
		.resize-v {
			height: 5px;
			cursor: row-resize;
			background: transparent;
			flex-shrink: 0;
			transition: background 0.15s;
		}
		.resize-v:hover, .resize-v.active { background: var(--accent); }

		/* Right main area */
		.main {
			flex: 1;
			min-width: 0;
			display: flex;
			flex-direction: column;
		}

		/* Browser strip at top of main */
		.browser-strip {
			flex-shrink: 0;
			border-bottom: none;
			overflow: hidden;
		}

		/* Chat fills remaining space */
		.chat-area {
			flex: 1;
			min-height: 0;
		}
	`;

	@state() private sidebarWidth = 220;
	@state() private browserHeight = 220;

	render() {
		return html`
			<div class="sidebar" style="width:${this.sidebarWidth}px">
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
			<div class="resize-h" @mousedown=${this.startH}></div>
			<div class="main">
				<div class="browser-strip" style="height:${this.browserHeight}px">
					<browser-panel></browser-panel>
				</div>
				<div class="resize-v" @mousedown=${this.startV}></div>
				<div class="chat-area">
					<chat-panel></chat-panel>
				</div>
			</div>
		`;
	}

	private startH(e: MouseEvent) {
		e.preventDefault();
		const startX = e.clientX;
		const startW = this.sidebarWidth;
		const handle = e.target as HTMLElement;
		handle.classList.add("active");

		const move = (ev: MouseEvent) => {
			const w = startW + (ev.clientX - startX);
			this.sidebarWidth = Math.max(140, Math.min(500, w));
		};
		const up = () => {
			handle.classList.remove("active");
			document.removeEventListener("mousemove", move);
			document.removeEventListener("mouseup", up);
		};
		document.addEventListener("mousemove", move);
		document.addEventListener("mouseup", up);
	}

	private startV(e: MouseEvent) {
		e.preventDefault();
		const startY = e.clientY;
		const startH = this.browserHeight;
		const handle = e.target as HTMLElement;
		handle.classList.add("active");

		const move = (ev: MouseEvent) => {
			const h = startH + (ev.clientY - startY);
			this.browserHeight = Math.max(0, Math.min(600, h));
		};
		const up = () => {
			handle.classList.remove("active");
			document.removeEventListener("mousemove", move);
			document.removeEventListener("mouseup", up);
		};
		document.addEventListener("mousemove", move);
		document.addEventListener("mouseup", up);
	}
}
