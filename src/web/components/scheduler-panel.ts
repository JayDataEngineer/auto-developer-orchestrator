/**
 * Scheduler panel — reuses SchedulerClient from TUI extension.
 *
 * Imports api.ts and types.ts directly from ts-tui-pi/extensions/pux-scheduler/.
 * Same client, same types, zero duplication.
 */

import { LitElement, html, css, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { SchedulerClient } from "../../../ts-tui-pi/extensions/pux-scheduler/api.js";
import type { SchedulerJob } from "../../../ts-tui-pi/extensions/pux-scheduler/types.js";

@customElement("scheduler-panel")
export class SchedulerPanel extends LitElement {
	static styles = css`
		:host { display: block; background: var(--surface); }
		.scheduler-summary {
			padding: 8px 12px;
			display: flex;
			align-items: center;
			gap: 8px;
			cursor: pointer;
		}
		.scheduler-summary:hover { background: var(--border); }
		.scheduler-summary .icon { font-size: 13px; }
		.scheduler-summary .label { font-size: 12px; color: var(--dim); flex: 1; }
		.scheduler-summary .count { font-size: 12px; color: var(--accent); font-weight: 600; }
		.scheduler-summary .running-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--warn); animation: pulse 1s infinite; }
		@keyframes pulse { 50% { opacity: 0.4; } }
		.job-list { max-height: 180px; overflow-y: auto; border-top: 1px solid var(--border); }
		.job { display: flex; align-items: center; padding: 6px 12px; gap: 8px; font-size: 12px; }
		.job:hover { background: var(--border); }
		.job .dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
		.job .dot.idle { background: var(--success); }
		.job .dot.running { background: var(--warn); }
		.job .dot.error { background: var(--error); }
		.job .dot.disabled { background: var(--dim); }
		.job .name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
		.job .status-text { color: var(--dim); font-size: 11px; }
	`;

	@property() serverUrl = "";
	@state() private jobs: SchedulerJob[] = [];
	@state() private error = "";
	@state() private loading = true;
	@state() private expanded = false;
	private client!: SchedulerClient;
	private pollTimer: ReturnType<typeof setInterval> | undefined;

	connectedCallback() {
		super.connectedCallback();
		this.client = new SchedulerClient(this.serverUrl);
		this.fetchJobs();
		this.pollTimer = setInterval(() => this.fetchJobs(), 5000);
	}

	disconnectedCallback() {
		super.disconnectedCallback();
		if (this.pollTimer) clearInterval(this.pollTimer);
	}

	private async fetchJobs() {
		try {
			this.jobs = await this.client.listJobs();
			this.error = "";
		} catch (err: any) {
			this.error = err.message;
		} finally {
			this.loading = false;
		}
	}

	render() {
		const running = this.jobs.filter(j => j.status === "running").length;
		return html`
			<div class="scheduler-summary" @click=${() => { this.expanded = !this.expanded; }}>
				<span class="icon">⚙</span>
				<span class="label">Jobs</span>
				${running > 0 ? html`<span class="running-dot"></span>` : nothing}
				<span class="count">${this.jobs.length} total</span>
			</div>
			${this.expanded ? html`
				<div class="job-list">
					${this.loading && this.jobs.length === 0
						? html`<div class="job"><span style="color:var(--dim)">Loading...</span></div>`
						: this.jobs.length === 0
							? html`<div class="job"><span style="color:var(--dim)">No jobs</span></div>`
							: this.jobs.map(j => html`
								<div class="job">
									<div class="dot ${j.status}"></div>
									<span class="name">${j.name}</span>
									<span class="status-text">${j.status}</span>
								</div>
							`)
					}
				</div>
			` : nothing}
		`;
	}
}
