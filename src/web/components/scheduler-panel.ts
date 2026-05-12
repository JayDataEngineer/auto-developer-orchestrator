/**
 * Scheduler panel — reuses SchedulerClient from TUI extension.
 *
 * Imports api.ts and types.ts directly from ts-tui-pi/extensions/pux-scheduler/.
 * Same client, same types, zero duplication.
 */

import { LitElement, html, css, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { SchedulerClient } from "../../../ts-tui-pi/extensions/pux-scheduler/api.js";
import { formatSchedule } from "../../../ts-tui-pi/extensions/pux-scheduler/render.js";
import type { SchedulerJob } from "../../../ts-tui-pi/extensions/pux-scheduler/types.js";

@customElement("scheduler-panel")
export class SchedulerPanel extends LitElement {
	static styles = css`
		:host { display: flex; flex-direction: column; height: 100%; background: var(--bg); }
		.header { height: 32px; display: flex; align-items: center; padding: 0 12px; border-bottom: 1px solid var(--border); background: var(--surface); gap: 8px; flex-shrink: 0; }
		.header .title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.1em; color: var(--dim); }
		.header .count { font-size: 12px; color: var(--accent); }
		.header button { margin-left: auto; background: none; border: 1px solid var(--border); color: var(--dim); border-radius: 4px; padding: 4px 8px; cursor: pointer; font-size: 11px; }
		.header button:hover { color: var(--text); background: var(--border); }
		.jobs { flex: 1; overflow-y: auto; padding: 8px; }
		.job { display: flex; align-items: center; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; margin-bottom: 6px; gap: 10px; cursor: pointer; }
		.job:hover { background: var(--surface); }
		.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
		.dot.idle { background: var(--success); }
		.dot.running { background: var(--warn); animation: pulse 1s infinite; }
		.dot.error { background: var(--error); }
		.dot.disabled { background: var(--dim); }
		@keyframes pulse { 50% { opacity: 0.4; } }
		.job-info { flex: 1; min-width: 0; }
		.job-name { font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
		.job-meta { font-size: 11px; color: var(--dim); margin-top: 2px; }
		.job-actions { display: flex; gap: 4px; }
		.job-actions button { background: none; border: 1px solid var(--border); color: var(--dim); border-radius: 4px; padding: 3px 6px; cursor: pointer; font-size: 11px; }
		.job-actions button:hover { color: var(--text); background: var(--border); }
		.job-actions button.run:hover { color: var(--success); border-color: var(--success); }
		.error-banner { padding: 8px 12px; background: rgba(239,68,68,0.1); color: var(--error); font-size: 12px; }
		.empty { flex: 1; display: flex; align-items: center; justify-content: center; color: var(--dim); font-size: 13px; }
	`;

	@property() serverUrl = "";
	@state() private jobs: SchedulerJob[] = [];
	@state() private error = "";
	@state() private loading = true;
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

	private async triggerJob(job: SchedulerJob) {
		try {
			await this.client.triggerJob(job.id);
			this.fetchJobs();
		} catch (err: any) {
			this.error = err.message;
		}
	}

	private async deleteJob(job: SchedulerJob) {
		try {
			await this.client.deleteJob(job.id);
			this.fetchJobs();
		} catch (err: any) {
			this.error = err.message;
		}
	}

	render() {
		return html`
			${this.error ? html`<div class="error-banner">${this.error}</div>` : nothing}
			<div class="header">
				<span class="title">Scheduled Jobs</span>
				<span class="count">${this.jobs.length}</span>
				<button @click=${this.fetchJobs}>Refresh</button>
			</div>
			<div class="jobs">
				${this.loading && this.jobs.length === 0
					? html`<div class="empty">Loading...</div>`
					: this.jobs.length === 0
						? html`<div class="empty">No jobs. Create one with the CLI: orch scheduler create</div>`
						: this.jobs.map(j => this.renderJob(j))
				}
			</div>
		`;
	}

	private renderJob(job: SchedulerJob) {
		return html`
			<div class="job">
				<div class="dot ${job.status}"></div>
				<div class="job-info">
					<div class="job-name">${job.name}</div>
					<div class="job-meta">${formatSchedule(job)} · ${job.status}${job.lastError ? " · error" : ""}</div>
				</div>
				<div class="job-actions">
					<button class="run" @click=${() => this.triggerJob(job)} ?disabled=${job.status === "running"}>Run</button>
					<button @click=${() => this.deleteJob(job)}>Del</button>
				</div>
			</div>
		`;
	}
}
