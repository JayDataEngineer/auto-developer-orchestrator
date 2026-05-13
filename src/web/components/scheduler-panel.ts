/**
 * Scheduler panel — full CRUD for scheduled jobs.
 *
 * Reuses SchedulerClient from TUI extension (shared API client).
 * Sidebar panel with: job list, click-to-detail, trigger/delete/enable/disable,
 * create form, and run history.
 */

import { LitElement, html, css, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { SchedulerClient } from "../../../ts-tui-pi/extensions/pux-scheduler/api.js";
import type { SchedulerJob, RunLogEntry, CreateJobRequest, ScheduleType } from "../../../ts-tui-pi/extensions/pux-scheduler/types.js";
import { toast } from "./toast-container.js";

type View = "list" | "detail" | "create" | "runs";

@customElement("scheduler-panel")
export class SchedulerPanel extends LitElement {
	static styles = css`
		:host { display: block; background: var(--surface); font-size: 12px; }

		/* Summary bar */
		.summary {
			padding: 8px 12px;
			display: flex;
			align-items: center;
			gap: 8px;
			cursor: pointer;
		}
		.summary:hover { background: var(--border); }
		.summary .icon { font-size: 13px; }
		.summary .label { color: var(--dim); flex: 1; }
		.summary .count { color: var(--accent); font-weight: 600; }
		.summary .running-dot {
			width: 6px; height: 6px; border-radius: 50%;
			background: var(--warn); animation: pulse 1s infinite;
		}
		@keyframes pulse { 50% { opacity: 0.4; } }

		/* Panel body */
		.body {
			border-top: 1px solid var(--border);
			max-height: 360px;
			overflow-y: auto;
		}

		/* Job row */
		.job {
			display: flex; align-items: center;
			padding: 6px 12px; gap: 8px; cursor: pointer;
		}
		.job:hover { background: var(--border); }
		.job.active { background: var(--border); border-left: 2px solid var(--accent); }
		.job .dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
		.job .dot.idle { background: var(--success); }
		.job .dot.running { background: var(--warn); }
		.job .dot.error { background: var(--error); }
		.job .dot.disabled { background: var(--dim); }
		.job .name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
		.job .sched { color: var(--dim); font-size: 11px; }
		.job .badge { font-size: 10px; padding: 1px 5px; border-radius: 3px; }
		.job .badge.error { background: var(--error); color: white; }
		.job .badge.running { background: var(--warn); color: white; }

		/* Detail view */
		.detail { padding: 8px 12px; }
		.detail .field { margin-bottom: 4px; }
		.detail .field .label { color: var(--dim); font-size: 11px; }
		.detail .field .value { color: var(--text); }
		.detail .error-text { color: var(--error); font-size: 11px; margin-top: 4px; }
		.detail .prompt { color: var(--dim); font-size: 11px; margin-top: 6px; max-height: 60px; overflow: hidden; }

		/* Action buttons */
		.actions {
			display: flex; gap: 6px; padding: 8px 12px;
			border-top: 1px solid var(--border);
		}
		.btn {
			background: none; border: 1px solid var(--border); border-radius: 4px;
			color: var(--text); font-size: 11px; padding: 3px 8px; cursor: pointer;
		}
		.btn:hover { border-color: var(--accent); color: var(--accent); }
		.btn.danger { color: var(--error); }
		.btn.danger:hover { border-color: var(--error); }
		.btn.primary { background: var(--accent); color: white; border-color: var(--accent); }
		.btn:disabled { opacity: 0.4; cursor: not-allowed; }

		/* Back button */
		.back {
			padding: 6px 12px; cursor: pointer; color: var(--dim);
			border-bottom: 1px solid var(--border); font-size: 11px;
		}
		.back:hover { color: var(--accent); }

		/* Run history */
		.run {
			padding: 4px 12px; border-bottom: 1px solid var(--border);
			font-size: 11px; display: flex; gap: 8px; align-items: baseline;
		}
		.run .ts { color: var(--dim); min-width: 80px; }
		.run .ok { color: var(--success); }
		.run .err { color: var(--error); }
		.run .text { color: var(--text); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

		/* Create form */
		.form { padding: 8px 12px; display: flex; flex-direction: column; gap: 6px; }
		.form label { color: var(--dim); font-size: 11px; display: block; }
		.form input, .form select, .form textarea {
			width: 100%; box-sizing: border-box;
			background: var(--bg); border: 1px solid var(--border); border-radius: 4px;
			color: var(--text); font-size: 12px; padding: 4px 8px; outline: none;
		}
		.form input:focus, .form select:focus, .form textarea:focus { border-color: var(--accent); }
		.form textarea { resize: vertical; min-height: 40px; font-family: inherit; }
		.form .row { display: flex; gap: 6px; }
		.form .row > * { flex: 1; }
	`;

	@property() serverUrl = "";
	@property() forceOpen = false;
	@state() private jobs: SchedulerJob[] = [];
	@state() private runs: RunLogEntry[] = [];
	@state() private loading = true;
	@state() private expanded = false;
	@state() private view: View = "list";
	@state() private selectedJob: SchedulerJob | null = null;
	@state() private working = false;

	// Create form state
	@state() private formName = "";
	@state() private formProject = "";
	@state() private formMessage = "";
	@state() private formScheduleType: ScheduleType = "manual";
	@state() private formCron = "";
	@state() private formEvery = "5m";
	@state() private formModel = "";
	@state() private formEnabled = true;

	private client!: SchedulerClient;
	private pollTimer: ReturnType<typeof setInterval> | undefined;

	connectedCallback() {
		super.connectedCallback();
		this.client = new SchedulerClient(this.serverUrl);
		this.fetchJobs();
		this.pollTimer = setInterval(() => this.fetchJobs(), 10000);
	}

	updated(changed: Map<string, any>) {
		if (changed.has("forceOpen") && this.forceOpen) {
			this.expanded = true;
			this.view = "list";
		}
	}

	disconnectedCallback() {
		super.disconnectedCallback();
		if (this.pollTimer) clearInterval(this.pollTimer);
	}

	private async fetchJobs() {
		try {
			this.jobs = await this.client.listJobs();
		} catch { /* backend down */ } finally {
			this.loading = false;
		}
	}

	// ── Scheduling helpers ──────────────────────────────

	private formatSchedule(job: SchedulerJob): string {
		switch (job.scheduleType) {
			case "cron": return job.cronExpr || "cron";
			case "every": {
				const s = job.everySeconds || 0;
				if (s < 60) return `${s}s`;
				if (s < 3600) return `${Math.floor(s / 60)}m`;
				if (s < 86400) return `${Math.floor(s / 3600)}h`;
				return `${Math.floor(s / 86400)}d`;
			}
			case "at": return job.atTime ? `at ${job.atTime.slice(0, 16)}` : "at ?";
			case "manual": return "manual";
			default: return "?";
		}
	}

	private parseEvery(s: string): number {
		const m = s.match(/^(\d+(?:\.\d+)?)\s*(ms|s|m|h|d)?$/);
		if (!m) return 0;
		const val = parseFloat(m[1]);
		const unit = m[2] || "s";
		switch (unit) {
			case "ms": return Math.round(val / 1000);
			case "s": return Math.round(val);
			case "m": return Math.round(val * 60);
			case "h": return Math.round(val * 3600);
			case "d": return Math.round(val * 86400);
			default: return 0;
		}
	}

	private fmtTime(iso?: string): string {
		if (!iso) return "";
		try {
			const d = new Date(iso);
			if (isNaN(d.getTime())) return "";
			const diff = Date.now() - d.getTime();
			if (diff < 60000) return "just now";
			if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
			if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
			return `${Math.floor(diff / 86400000)}d ago`;
		} catch { return ""; }
	}

	// ── Actions ─────────────────────────────────────────

	private async triggerJob(job: SchedulerJob) {
		this.working = true;
		try {
			await this.client.triggerJob(job.id);
			toast(`Triggered '${job.name}'`, "ok");
			this.fetchJobs();
		} catch (err: any) {
			toast(err.message || "Trigger failed", "err");
		} finally {
			this.working = false;
		}
	}

	private async deleteJob(job: SchedulerJob) {
		this.working = true;
		try {
			await this.client.deleteJob(job.id);
			toast(`Deleted '${job.name}'`, "ok");
			this.selectedJob = null;
			this.view = "list";
			this.fetchJobs();
		} catch (err: any) {
			toast(err.message || "Delete failed", "err");
		} finally {
			this.working = false;
		}
	}

	private async toggleEnabled(job: SchedulerJob) {
		this.working = true;
		try {
			await this.client.updateJob(job.id, { enabled: !job.enabled });
			toast(job.enabled ? `Disabled '${job.name}'` : `Enabled '${job.name}'`, "ok");
			this.fetchJobs();
			// Refresh selected job
			const updated = this.jobs.find(j => j.id === job.id);
			if (updated) this.selectedJob = updated;
		} catch (err: any) {
			toast(err.message || "Toggle failed", "err");
		} finally {
			this.working = false;
		}
	}

	private async showRuns(job: SchedulerJob) {
		this.selectedJob = job;
		this.view = "runs";
		try {
			this.runs = await this.client.listRuns(job.id, 15);
		} catch {
			this.runs = [];
		}
	}

	private async createJob() {
		if (!this.formName || !this.formMessage) {
			toast("Name and message are required", "err");
			return;
		}

		const req: CreateJobRequest = {
			name: this.formName,
			project: this.formProject || "default",
			message: this.formMessage,
			model: this.formModel || undefined,
			scheduleType: this.formScheduleType,
			enabled: this.formEnabled,
		};

		if (this.formScheduleType === "cron") {
			req.cronExpr = this.formCron;
		} else if (this.formScheduleType === "every") {
			req.everySeconds = this.parseEvery(this.formEvery);
			if (req.everySeconds <= 0) {
				toast("Invalid interval. Use 30s, 5m, 1h", "err");
				return;
			}
		}

		this.working = true;
		try {
			await this.client.createJob(req);
			toast(`Created '${this.formName}'`, "ok");
			this.view = "list";
			this.formName = "";
			this.formProject = "";
			this.formMessage = "";
			this.formScheduleType = "manual";
			this.formCron = "";
			this.formEvery = "5m";
			this.formModel = "";
			this.formEnabled = true;
			this.fetchJobs();
		} catch (err: any) {
			toast(err.message || "Create failed", "err");
		} finally {
			this.working = false;
		}
	}

	// ── Render ──────────────────────────────────────────

	render() {
		const running = this.jobs.filter(j => j.status === "running").length;
		const errors = this.jobs.filter(j => j.status === "error").length;

		return html`
			<div class="summary" @click=${() => { if (this.view === "list") this.expanded = !this.expanded; }}>
				<span class="icon">⚙</span>
				<span class="label">Scheduler</span>
				${running > 0 ? html`<span class="running-dot"></span>` : nothing}
				${errors > 0 ? html`<span style="color:var(--error);font-weight:600">${errors}✗</span>` : nothing}
				<span class="count">${this.jobs.length} jobs</span>
			</div>
			${this.expanded ? this.renderBody() : nothing}
		`;
	}

	private renderBody() {
		switch (this.view) {
			case "list": return this.renderList();
			case "detail": return this.renderDetail();
			case "runs": return this.renderRuns();
			case "create": return this.renderCreate();
		}
	}

	private renderList() {
		return html`
			<div class="body">
				${this.loading && this.jobs.length === 0
					? html`<div style="padding:12px;color:var(--dim)">Loading...</div>`
					: this.jobs.length === 0
						? html`<div style="padding:12px;color:var(--dim)">No jobs</div>`
						: this.jobs.map(j => html`
							<div class="job" @click=${() => { this.selectedJob = j; this.view = "detail"; }}>
								<div class="dot ${j.enabled ? j.status : "disabled"}"></div>
								<span class="name">${j.name}</span>
								<span class="sched">${this.formatSchedule(j)}</span>
								${j.status === "error" ? html`<span class="badge error">error</span>` : nothing}
								${j.status === "running" ? html`<span class="badge running">run</span>` : nothing}
							</div>
						`)
				}
			</div>
			<div class="actions">
				<button class="btn primary" @click=${() => { this.view = "create"; }}>+ New Job</button>
				<button class="btn" @click=${() => this.fetchJobs()}>Refresh</button>
			</div>
		`;
	}

	private renderDetail() {
		const job = this.selectedJob;
		if (!job) return nothing;
		const lastRun = this.fmtTime(job.lastRunAt);
		return html`
			<div class="back" @click=${() => { this.view = "list"; this.selectedJob = null; }}>← All jobs</div>
			<div class="body">
				<div class="detail">
					<div class="field">
						<span class="value" style="font-weight:600;font-size:13px">${job.name}</span>
						<span style="color:var(--dim);font-size:11px;margin-left:6px">${job.id.slice(0, 8)}</span>
					</div>
					${job.description ? html`<div class="field"><span class="value">${job.description}</span></div>` : nothing}
					<div class="field">
						<span class="label">Project:</span>
						<span class="value">${job.project}</span>
					</div>
					${job.model ? html`
						<div class="field">
							<span class="label">Model:</span>
							<span class="value">${job.model}</span>
						</div>
					` : nothing}
					<div class="field">
						<span class="label">Schedule:</span>
						<span class="value">${this.formatSchedule(job)}</span>
					</div>
					<div class="field">
						<span class="label">Status:</span>
						<span class="value">${job.enabled ? job.status : "disabled"}</span>
						${job.consecutiveErrors > 0 ? html`<span style="color:var(--error)"> (${job.consecutiveErrors} errors)</span>` : nothing}
					</div>
					${lastRun ? html`
						<div class="field">
							<span class="label">Last run:</span>
							<span class="value">${lastRun}${job.durationMs ? ` (${(job.durationMs / 1000).toFixed(1)}s)` : ""}</span>
						</div>
					` : nothing}
					${job.lastError ? html`<div class="error-text">${job.lastError.slice(0, 150)}</div>` : nothing}
					<div class="prompt">${job.message.slice(0, 150)}${job.message.length > 150 ? "..." : ""}</div>
				</div>
			</div>
			<div class="actions">
				<button class="btn" ?disabled=${this.working} @click=${() => this.triggerJob(job)}>▶ Trigger</button>
				<button class="btn" ?disabled=${this.working} @click=${() => this.toggleEnabled(job)}>
					${job.enabled ? "⏸ Disable" : "▶ Enable"}
				</button>
				<button class="btn" ?disabled=${this.working} @click=${() => this.showRuns(job)}>Runs</button>
				<button class="btn danger" ?disabled=${this.working} @click=${() => {
					if (confirm(`Delete job '${job.name}'?`)) this.deleteJob(job);
				}}>🗑 Delete</button>
			</div>
		`;
	}

	private renderRuns() {
		const job = this.selectedJob;
		return html`
			<div class="back" @click=${() => { this.view = "detail"; }}>← ${job?.name || "Jobs"}</div>
			<div class="body">
				${this.runs.length === 0
					? html`<div style="padding:12px;color:var(--dim)">No runs</div>`
					: this.runs.map(r => {
						const ts = new Date(r.ts).toLocaleString();
						const ok = r.status === "ok";
						const text = r.summary || r.error || "";
						return html`
							<div class="run">
								<span class="ts">${ts}</span>
								<span class="${ok ? "ok" : "err"}">${ok ? "✓" : "✗"}</span>
								<span class="text">${text.slice(0, 80)}</span>
							</div>
						`;
					})
				}
			</div>
		`;
	}

	private renderCreate() {
		return html`
			<div class="back" @click=${() => { this.view = "list"; }}>← All jobs</div>
			<div class="body">
				<div class="form">
					<div>
						<label>Name *</label>
						<input type="text" .value=${this.formName} @input=${(e: Event) => { this.formName = (e.target as HTMLInputElement).value; }} placeholder="Daily Report" />
					</div>
					<div>
						<label>Project</label>
						<input type="text" .value=${this.formProject} @input=${(e: Event) => { this.formProject = (e.target as HTMLInputElement).value; }} placeholder="my-project" />
					</div>
					<div>
						<label>Prompt *</label>
						<textarea .value=${this.formMessage} @input=${(e: Event) => { this.formMessage = (e.target as HTMLTextAreaElement).value; }} placeholder="What should this job do?"></textarea>
					</div>
					<div>
						<label>Model</label>
						<input type="text" .value=${this.formModel} @input=${(e: Event) => { this.formModel = (e.target as HTMLInputElement).value; }} placeholder="default" />
					</div>
					<div class="row">
						<div>
							<label>Schedule</label>
							<select .value=${this.formScheduleType} @change=${(e: Event) => {
								this.formScheduleType = (e.target as HTMLSelectElement).value as ScheduleType;
							}}>
								<option value="manual">Manual</option>
								<option value="cron">Cron</option>
								<option value="every">Every</option>
								<option value="at">One-shot</option>
							</select>
						</div>
						<div>
							<label>Enabled</label>
							<select .value=${this.formEnabled ? "yes" : "no"} @change=${(e: Event) => {
								this.formEnabled = (e.target as HTMLSelectElement).value === "yes";
							}}>
								<option value="yes">Yes</option>
								<option value="no">No</option>
							</select>
						</div>
					</div>
					${this.formScheduleType === "cron" ? html`
						<div>
							<label>Cron (6-field with seconds)</label>
							<input type="text" .value=${this.formCron} @input=${(e: Event) => { this.formCron = (e.target as HTMLInputElement).value; }} placeholder="0 0 9 * * *" />
						</div>
					` : nothing}
					${this.formScheduleType === "every" ? html`
						<div>
							<label>Interval</label>
							<input type="text" .value=${this.formEvery} @input=${(e: Event) => { this.formEvery = (e.target as HTMLInputElement).value; }} placeholder="5m, 1h, 30s" />
						</div>
					` : nothing}
				</div>
			</div>
			<div class="actions">
				<button class="btn primary" ?disabled=${this.working || !this.formName || !this.formMessage} @click=${() => this.createJob()}>
					Create Job
				</button>
			</div>
		`;
	}
}
