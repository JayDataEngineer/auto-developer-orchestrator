/**
 * Browser screenshot panel — shows live screenshots from sandbox CDP.
 *
 * Polls /api/sandbox/{id}/screenshot for the latest screenshot.
 * Displays current URL and basic controls.
 */

import { LitElement, html, css, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";

@customElement("browser-panel")
export class BrowserPanel extends LitElement {
	static styles = css`
		:host { display: flex; flex-direction: column; height: 100%; background: var(--surface); }
		.header { height: 28px; display: flex; align-items: center; padding: 0 12px; border-bottom: 1px solid var(--border); gap: 8px; flex-shrink: 0; }
		.header .title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.1em; color: var(--dim); }
		.header .status { margin-left: auto; font-size: 11px; }
		.header .status.active { color: var(--success); }
		.header .status.inactive { color: var(--dim); }
		.screenshot { flex: 1; display: flex; align-items: center; justify-content: center; overflow: hidden; background: #0a0a0a; }
		.screenshot img { max-width: 100%; max-height: 100%; object-fit: contain; }
		.empty { color: var(--dim); font-size: 12px; text-align: center; }
	`;

	@property() serverUrl = "";
	@state() private screenshot: string | null = null;
	@state() private currentUrl = "";
	@state() private sandboxId = "";
	@state() private pollInterval: ReturnType<typeof setInterval> | undefined;

	connectedCallback() {
		super.connectedCallback();
		this.resolveSandbox();
	}

	disconnectedCallback() {
		super.disconnectedCallback();
		if (this.pollInterval) clearInterval(this.pollInterval);
	}

	private async resolveSandbox() {
		try {
			const resp = await fetch(`${this.serverUrl}/api/sandbox`);
			const sandboxes = await resp.json() as any[];
			if (sandboxes.length > 0) {
				this.sandboxId = sandboxes[0].id;
				this.startPolling();
			}
		} catch {}
	}

	private startPolling() {
		if (this.pollInterval) clearInterval(this.pollInterval);
		this.capture();
		this.pollInterval = setInterval(() => this.capture(), 3000);
	}

	private async capture() {
		if (!this.sandboxId) return;
		try {
			const resp = await fetch(`${this.serverUrl}/api/sandbox/${this.sandboxId}/screenshot`);
			if (!resp.ok) return;
			const data = await resp.json() as { image?: string; url?: string };
			if (data.image) {
				this.screenshot = data.image.startsWith("data:") ? data.image : `data:image/png;base64,${data.image}`;
			}
			if (data.url) this.currentUrl = data.url;
		} catch {}
	}

	render() {
		return html`
			<div class="header">
				<span class="title">Browser</span>
				<span class="status ${this.sandboxId ? "active" : "inactive"}">
					${this.sandboxId ? "Active" : "No sandbox"}
				</span>
			</div>
			<div class="screenshot">
				${this.screenshot
					? html`<img src=${this.screenshot} alt="Browser" />`
					: html`<div class="empty">No active browser session</div>`
				}
			</div>
		`;
	}
}
