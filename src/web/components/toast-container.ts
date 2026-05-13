/**
 * Toast notification container — global, lightweight.
 *
 * Usage from any component or plain JS:
 *   window.dispatchEvent(new CustomEvent("pux-toast", {
 *     detail: { text: "Job created", type: "ok" }
 *   }));
 *
 * Types: "ok" (green), "err" (red), "info" (accent)
 */

import { LitElement, html, css } from "lit";
import { customElement, state } from "lit/decorators.js";

interface Toast {
	id: number;
	text: string;
	type: "ok" | "err" | "info";
}

@customElement("toast-container")
export class ToastContainer extends LitElement {
	static styles = css`
		:host {
			position: fixed;
			bottom: 16px;
			right: 16px;
			z-index: 9999;
			display: flex;
			flex-direction: column-reverse;
			gap: 8px;
			pointer-events: none;
		}
		.toast {
			padding: 8px 14px;
			border-radius: 6px;
			font-size: 12px;
			background: var(--surface);
			border: 1px solid var(--border);
			color: var(--text);
			pointer-events: auto;
			animation: slideIn 0.15s ease-out;
			max-width: 300px;
		}
		.toast.ok { border-color: var(--success); color: var(--success); }
		.toast.err { border-color: var(--error); color: var(--error); }
		.toast.info { border-color: var(--accent); color: var(--accent); }
		@keyframes slideIn {
			from { opacity: 0; transform: translateY(8px); }
			to { opacity: 1; transform: translateY(0); }
		}
	`;

	@state() private toasts: Toast[] = [];
	private nextId = 0;
	private static MAX = 5;

	connectedCallback() {
		super.connectedCallback();
		this._handler = ((e: CustomEvent) => {
			const { text, type = "info" } = e.detail;
			const id = this.nextId++;
			let next = [...this.toasts, { id, text, type }];
			if (next.length > ToastContainer.MAX) next = next.slice(-ToastContainer.MAX);
			this.toasts = next;
			setTimeout(() => {
				this.toasts = this.toasts.filter(t => t.id !== id);
			}, 3000);
		}) as EventListener;
		window.addEventListener("pux-toast", this._handler);
	}

	disconnectedCallback() {
		super.disconnectedCallback();
		window.removeEventListener("pux-toast", this._handler);
	}

	render() {
		return this.toasts.map(t => html`<div class="toast ${t.type}">${t.text}</div>`);
	}

	private _handler!: EventListener;
}

/** Helper to fire a toast from anywhere. */
export function toast(text: string, type: "ok" | "err" | "info" = "info") {
	window.dispatchEvent(new CustomEvent("pux-toast", { detail: { text, type } }));
}
