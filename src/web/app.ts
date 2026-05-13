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
import { customElement, property, state } from "lit/decorators.js";
import "./components/chat-panel.js";
import "./components/browser-panel.js";
import "./components/scheduler-panel.js";

interface ConversationSummary {
	project: string;
	agentId: string;
	title: string;
	lastMessage: string;
	lastAt: string;
	messageCount: number;
}

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
			gap: 8px;
		}
		.sidebar-brand .name { font-weight: 700; font-size: 14px; color: var(--accent); }
		.sidebar-brand .spacer { flex: 1; }
		.btn-new {
			background: none; border: 1px solid var(--border); border-radius: 6px;
			color: var(--dim); font-size: 11px; padding: 2px 8px; cursor: pointer;
			line-height: 18px; white-space: nowrap;
		}
		.btn-new:hover { border-color: var(--accent); color: var(--accent); }
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
		.conv {
			padding: 8px;
			border-bottom: 1px solid var(--border);
			cursor: pointer;
		}
		.conv:hover { background: var(--border); }
		.conv.active { background: var(--border); border-left: 2px solid var(--accent); }
		.conv .conv-title { font-size: 12px; font-weight: 600; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
		.conv .conv-preview { font-size: 11px; color: var(--dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 2px; }
		.conv .conv-meta { font-size: 10px; color: var(--dim); margin-top: 2px; }
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
	@state() private conversations: ConversationSummary[] = [];
	@state() private activeAgentId = "";
	@state() private schedulerOpen = false;
	@property() serverUrl = "";
	@property() project = "ts-tui-pi";
	private pollTimer: ReturnType<typeof setInterval> | undefined;

	render() {
		return html`
			<div class="sidebar" style="width:${this.sidebarWidth}px">
				<div class="sidebar-brand">
					<span class="name">Pux</span>
					<span class="spacer"></span>
					<button class="btn-new" @click=${this.newChat}>+ New</button>
				</div>
				<div class="sidebar-chat-history">
					${this.conversations.length === 0
						? html`<div class="empty">No conversations yet</div>`
						: this.conversations.map(c => html`
							<div class="conv ${c.agentId === this.activeAgentId ? 'active' : ''}"
								@click=${() => this.loadConversation(c)}>
								<div class="conv-title">${c.title || c.lastMessage.slice(0, 40) || 'Untitled'}</div>
								<div class="conv-preview">${c.lastMessage.slice(0, 60)}</div>
								<div class="conv-meta">${c.messageCount} msgs · ${this.fmtTime(c.lastAt)}</div>
							</div>
						`)
					}
				</div>
				<div class="sidebar-bottom">
					<scheduler-panel .serverUrl=${this.serverUrl} .forceOpen=${this.schedulerOpen} @toggle-request=${() => { this.schedulerOpen = !this.schedulerOpen; }}></scheduler-panel>
				</div>
			</div>
			<div class="resize-h" @mousedown=${this.startH}></div>
			<div class="main">
				<div class="browser-strip" style="height:${this.browserHeight}px">
					<browser-panel></browser-panel>
				</div>
				<div class="resize-v" @mousedown=${this.startV}></div>
				<div class="chat-area">
					<chat-panel id="chat" .serverUrl=${this.serverUrl} .project=${this.project} @toggle-scheduler=${this.toggleScheduler}></chat-panel>
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

	connectedCallback() {
		super.connectedCallback();
		this.fetchConversations();
		this.pollTimer = setInterval(() => this.fetchConversations(), 10000);
	}

	disconnectedCallback() {
		super.disconnectedCallback();
		if (this.pollTimer) clearInterval(this.pollTimer);
	}

	private async fetchConversations() {
		try {
			const url = `${this.serverUrl || ""}/api/pux/conversations?project=${encodeURIComponent(this.project)}`;
			const res = await fetch(url);
			if (!res.ok) return;
			const data = await res.json();
			this.conversations = data.map((s: any) => ({
				project: s.project || "",
				agentId: s.agentId || "",
				title: s.title || s.lastMessage?.slice(0, 60) || "Untitled",
				lastMessage: s.lastMessage || "",
				lastAt: s.lastAt || "",
				messageCount: s.messageCount || 0,
			}));
		} catch { /* backend not running */ }
	}

	private async loadConversation(c: ConversationSummary) {
		this.activeAgentId = c.agentId;
		try {
			const url = `${this.serverUrl || ""}/api/pux/history?project=${encodeURIComponent(c.project)}&agentId=${encodeURIComponent(c.agentId)}`;
			const res = await fetch(url);
			if (!res.ok) return;
			const msgs = await res.json();
			const chat = this.shadowRoot?.getElementById("chat") as any;
			if (chat?.loadHistory) chat.loadHistory(msgs);
		} catch { /* ignore */ }
	}

	private newChat() {
		this.activeAgentId = "";
		const chat = this.shadowRoot?.getElementById("chat") as any;
		if (chat?.reset) chat.reset();
	}

	private toggleScheduler() {
		this.schedulerOpen = !this.schedulerOpen;
	}

	private fmtTime(iso: string): string {
		if (!iso) return "";
		try {
			const d = new Date(iso);
			const now = new Date();
			const diffMs = now.getTime() - d.getTime();
			const diffMin = Math.floor(diffMs / 60000);
			if (diffMin < 1) return "just now";
			if (diffMin < 60) return `${diffMin}m ago`;
			const diffHr = Math.floor(diffMin / 60);
			if (diffHr < 24) return `${diffHr}h ago`;
			return d.toLocaleDateString();
		} catch { return ""; }
	}
}
