/**
 * Terminal rendering helpers for scheduler data.
 */

import type { SchedulerJob, RunLogEntry, ScheduleType } from "./types.js";

// ── Status glyphs ──────────────────────────────────────────

const STATUS_GLYPH: Record<string, string> = {
	running: "\x1b[33m●\x1b[0m",   // yellow dot
	idle:    "\x1b[32m○\x1b[0m",    // green circle
	error:   "\x1b[31m✗\x1b[0m",    // red X
	disabled:"\x1b[90m⊘\x1b[0m",    // gray slashed circle
};

function statusGlyph(job: SchedulerJob): string {
	if (!job.enabled && job.status !== "running") return STATUS_GLYPH.disabled;
	return STATUS_GLYPH[job.status] || STATUS_GLYPH.idle;
}

// ── Formatters ─────────────────────────────────────────────

export function formatSchedule(job: SchedulerJob): string {
	switch (job.scheduleType) {
		case "cron": return job.cronExpr || "cron";
		case "every": {
			const s = job.everySeconds || 0;
			if (s < 60) return `every ${s}s`;
			if (s < 3600) return `every ${Math.floor(s / 60)}m`;
			if (s < 86400) return `every ${Math.floor(s / 3600)}h`;
			return `every ${Math.floor(s / 86400)}d`;
		}
		case "at": return job.atTime ? `at ${job.atTime.slice(0, 19)}` : "at ?";
		case "manual": return "manual";
		default: return "?";
	}
}

function formatDuration(ms?: number): string {
	if (!ms) return "—";
	if (ms < 1000) return `${ms}ms`;
	if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
	return `${(ms / 60000).toFixed(1)}m`;
}

function formatTokens(n?: number): string {
	if (!n) return "—";
	if (n < 1000) return String(n);
	return `${(n / 1000).toFixed(1)}k`;
}

function formatRelative(iso?: string): string {
	if (!iso) return "—";
	const d = new Date(iso);
	if (isNaN(d.getTime())) return "invalid";
	const diff = d.getTime() - Date.now();
	if (diff < 0) return "overdue";
	if (diff < 60000) return "<1m";
	if (diff < 3600000) return `${Math.ceil(diff / 60000)}m`;
	if (diff < 86400000) return `${Math.ceil(diff / 3600000)}h`;
	return `${Math.ceil(diff / 86400000)}d`;
}

// ── Renderers ──────────────────────────────────────────────

export function renderJobList(jobs: SchedulerJob[]): string {
	if (jobs.length === 0) {
		return "\x1b[90m  No scheduled jobs.\x1b[0m\n  Create one with: \x1b[36morch scheduler create\x1b[0m\n";
	}

	const lines: string[] = [
		`\x1b[1m  Scheduled Jobs (${jobs.length})\x1b[0m`,
		"",
	];

	// Sort: running first, then by nextRunAt
	const sorted = [...jobs].sort((a, b) => {
		if (a.status === "running" && b.status !== "running") return -1;
		if (b.status === "running" && a.status !== "running") return 1;
		return 0;
	});

	for (const job of sorted) {
		const glyph = statusGlyph(job);
		const sched = formatSchedule(job);
		const next = job.scheduleType !== "manual" ? `next: ${formatRelative(job.nextRunAt)}` : "";
		const errTag = job.lastError ? ` \x1b[31m⚠ error\x1b[0m` : "";
		const runTag = job.status === "running" ? ` \x1b[33m⟳ running\x1b[0m` : "";

		lines.push(`  ${glyph} \x1b[1m${job.name}\x1b[0m  \x1b[90m${sched}\x1b[0m${runTag}${errTag}`);
		if (next) lines.push(`    ${next}`);
		if (job.lastRunAt) {
			const ago = formatRelative(job.lastRunAt);
			const dur = formatDuration(job.durationMs);
			lines.push(`    last: ${ago}, ${dur}, ${formatTokens(job.inputTokens)}in/${formatTokens(job.outputTokens)}out`);
		}
	}

	lines.push("");
	lines.push(`  \x1b[90m/scheduler trigger <name>  /scheduler runs [name]\x1b[0m`);
	return lines.join("\n");
}

export function renderJobDetail(job: SchedulerJob): string {
	const lines: string[] = [
		`\x1b[1m  ${job.name}\x1b[0m  ${statusGlyph(job)}`,
		"",
		`  ID:          ${job.id}`,
		`  Project:     ${job.project}`,
		`  Schedule:    ${formatSchedule(job)}`,
		`  Status:      ${job.status}`,
		`  Enabled:     ${job.enabled}`,
	];

	if (job.description) lines.push(`  Description: ${job.description}`);
	if (job.model) lines.push(`  Model:       ${job.model}`);
	if (job.nextRunAt) lines.push(`  Next run:    ${new Date(job.nextRunAt).toLocaleString()}`);
	if (job.lastRunAt) {
		lines.push(`  Last run:    ${new Date(job.lastRunAt).toLocaleString()}`);
		lines.push(`  Last status: ${job.lastRunStatus || "—"}`);
		lines.push(`  Duration:    ${formatDuration(job.durationMs)}`);
		lines.push(`  Tokens:      ${formatTokens(job.inputTokens)} in / ${formatTokens(job.outputTokens)} out`);
	}
	if (job.lastError) {
		lines.push("");
		lines.push(`  \x1b[31mError:\x1b[0m ${job.lastError.slice(0, 200)}`);
	}
	if (job.consecutiveErrors > 0) {
		lines.push(`  Consecutive errors: ${job.consecutiveErrors}`);
	}
	if (job.webhookToken) {
		lines.push("");
		lines.push(`  \x1b[36mWebhook:\x1b[0m POST /api/scheduler/webhook/${job.webhookToken}`);
	}
	lines.push("");
	lines.push(`  \x1b[90mPrompt:\x1b[0m ${job.message.slice(0, 120)}${job.message.length > 120 ? "..." : ""}`);

	return lines.join("\n");
}

export function renderRunLog(runs: RunLogEntry[]): string {
	if (runs.length === 0) {
		return "\x1b[90m  No run history.\x1b[0m\n";
	}

	const lines: string[] = [
		`\x1b[1m  Run History (${runs.length})\x1b[0m`,
		"",
	];

	for (const run of runs.slice(0, 20)) {
		const ts = new Date(run.ts).toLocaleString();
		const statusGlyph = run.status === "ok"
			? "\x1b[32m✓\x1b[0m"
			: run.status === "error"
				? "\x1b[31m✗\x1b[0m"
				: "\x1b[90m○\x1b[0m";
		const dur = run.durationMs ? formatDuration(run.durationMs) : "";
		const model = run.model || "";

		lines.push(`  ${statusGlyph} ${ts}  ${dur}  \x1b[90m${model}\x1b[0m`);
		if (run.error) lines.push(`    \x1b[31m${run.error.slice(0, 100)}\x1b[0m`);
		if (run.summary) lines.push(`    \x1b[90m${run.summary.slice(0, 100)}\x1b[0m`);
	}

	return lines.join("\n");
}

export function renderStatusWidget(jobs: SchedulerJob[]): string {
	if (jobs.length === 0) return "";
	const running = jobs.filter(j => j.status === "running").length;
	const errors = jobs.filter(j => j.status === "error").length;
	const parts = [`${jobs.length} jobs`];
	if (running > 0) parts.push(`\x1b[33m${running} running\x1b[0m`);
	if (errors > 0) parts.push(`\x1b[31m${errors} error\x1b[0m`);
	return `⚙ ${parts.join(", ")}`;
}

export function formatJobSummary(job: SchedulerJob): string {
	const sched = formatSchedule(job);
	const status = job.enabled ? job.status : "disabled";
	const icon = status === "running" ? "\u25CF" : status === "error" ? "\u2717" : "\u2713";
	return `${icon} ${job.name} (${sched}) — ${status}`;
}
