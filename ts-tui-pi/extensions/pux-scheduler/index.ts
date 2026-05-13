/**
 * PUX Scheduler Extension — TUI Commands
 *
 * Adds /scheduler command to the TUI for job monitoring and quick actions.
 * Management (create/edit/delete) is done via web UI or `orch scheduler` CLI.
 * Shows job count in the status bar with adaptive polling.
 */

import { Type } from "@sinclair/typebox";
import { Container, Text } from "@mariozechner/pi-tui";
import type { ExtensionAPI, ExtensionCommandContext } from "../../src/core/extensions/types.js";
import { SchedulerClient, isConnectionError } from "./api.js";
import {
	renderJobList, renderJobDetail, renderRunLog, renderStatusWidget,
	hasRunningJobs,
} from "./render.js";
import type { SchedulerJob, CreateJobRequest, ScheduleType } from "./types.js";

const STATUS_KEY = "pux-scheduler";

function resolveJob(jobs: SchedulerJob[], nameOrId: string): SchedulerJob | undefined {
	return jobs.find(j => j.name === nameOrId || j.id === nameOrId);
}

export default function registerPuxSchedulerExtension(pi: ExtensionAPI): void {
	let client: SchedulerClient | null = null;
	let lastJobs: SchedulerJob[] = [];
	let pollTimer: ReturnType<typeof setInterval> | undefined;
	let backendUnavailable = false;

	function getClient(): SchedulerClient {
		if (!client) client = new SchedulerClient("http://localhost:3847");
		return client;
	}

	// ── Status bar widget with adaptive polling ────────────
	async function refreshStatus(ctx: any): Promise<void> {
		try {
			const jobs = await getClient().listJobs();
			lastJobs = jobs;
			backendUnavailable = false;
			const widget = renderStatusWidget(jobs);
			ctx.ui.setStatus(STATUS_KEY, widget || undefined);

			if (pollTimer) clearInterval(pollTimer);
			const interval = hasRunningJobs(jobs) ? 10_000 : 30_000;
			pollTimer = setInterval(() => refreshStatus(ctx), interval);
		} catch (err: any) {
			if (isConnectionError(err)) {
				backendUnavailable = true;
				ctx.ui.setStatus(STATUS_KEY, undefined);
				if (pollTimer) clearInterval(pollTimer);
				pollTimer = setInterval(() => refreshStatus(ctx), 60_000);
			}
		}
	}

	pi.on("session_start", (_event, ctx) => {
		refreshStatus(ctx);
	});

	pi.on("session_shutdown", () => {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = undefined;
		}
	});

	// ── Helper: fetch jobs or show connection error ────────
	async function fetchJobs(ctx: ExtensionCommandContext): Promise<SchedulerJob[] | null> {
		try {
			const jobs = await getClient().listJobs();
			lastJobs = jobs;
			backendUnavailable = false;
			return jobs;
		} catch (err: any) {
			if (isConnectionError(err)) {
				backendUnavailable = true;
				process.stdout.write("\x1b[31m  Scheduler backend not running.\x1b[0m\n  Start it with: \x1b[36mtask dev\x1b[0m\n");
				return null;
			}
			process.stdout.write(`\x1b[31m  Error: ${err.message}\x1b[0m\n`);
			return null;
		}
	}

	// ── /scheduler command — monitoring + quick actions ──────
	pi.registerCommand("scheduler", {
		description: "Monitor scheduled jobs: list, detail, trigger, runs",
		handler: async (args: string, ctx: ExtensionCommandContext) => {
			const parts = args.trim().split(/\s+/);
			const subcmd = parts[0] || "";
			const arg1 = parts.slice(1).join(" ");

			// No args → show list
			if (!subcmd) {
				const jobs = await fetchJobs(ctx);
				if (jobs) process.stdout.write(renderJobList(jobs));
				return;
			}

			try {
				switch (subcmd) {
					case "list": {
						const jobs = await fetchJobs(ctx);
						if (jobs) process.stdout.write(renderJobList(jobs));
						break;
					}
					case "detail":
					case "get": {
						if (!arg1) {
							process.stdout.write("\x1b[33m  Usage: /scheduler detail <name>\x1b[0m\n");
							break;
						}
						const jobs = await fetchJobs(ctx);
						if (!jobs) break;
						const job = jobs.find(j => j.name === arg1 || j.id === arg1);
						if (!job) {
							process.stdout.write(`\x1b[31m  Job '${arg1}' not found.\x1b[0m\n`);
							break;
						}
						process.stdout.write(renderJobDetail(job));
						break;
					}
					case "trigger":
					case "run": {
						if (!arg1) {
							process.stdout.write("\x1b[33m  Usage: /scheduler trigger <name>\x1b[0m\n");
							break;
						}
						const jobs = await fetchJobs(ctx);
						if (!jobs) break;
						const job = jobs.find(j => j.name === arg1 || j.id === arg1);
						if (!job) {
							process.stdout.write(`\x1b[31m  Job '${arg1}' not found.\x1b[0m\n`);
							break;
						}
						process.stdout.write(`  \x1b[33mTriggering '${job.name}'...\x1b[0m\n`);
						const msg = await getClient().triggerJob(job.id);
						process.stdout.write(`  \x1b[32m✓ ${msg}\x1b[0m\n`);
						refreshStatus(ctx);
						break;
					}
					case "runs":
					case "history": {
						let jobId: string | undefined;
						if (arg1) {
							const jobs = await fetchJobs(ctx);
							if (!jobs) break;
							const job = jobs.find(j => j.name === arg1 || j.id === arg1);
							jobId = job?.id;
						}
						const runs = await getClient().listRuns(jobId, 20);
						process.stdout.write(renderRunLog(runs));
						break;
					}
					default:
						process.stdout.write(
							`\x1b[33m  Unknown subcommand '${subcmd}'.\x1b[0m\n` +
							"  Usage: /scheduler [list|detail|trigger|runs]\n" +
							"  Manage jobs via web UI or: orch scheduler create/edit/delete/enable/disable\n"
						);
				}
			} catch (err: any) {
				const msg = isConnectionError(err)
					? "Backend not running. Start it with: \x1b[36mtask dev\x1b[0m"
					: err.message;
				process.stdout.write(`\x1b[31m  Error: ${msg}\x1b[0m\n`);
			}
		},
	});

	// ── scheduler tool (LLM can manage jobs) ──────────────────
	pi.registerTool({
		name: "scheduler",
		label: "Scheduler",
		description: "Manage scheduled jobs: list, create, edit, delete, trigger, view details and run history. The Go backend handles execution.",
		parameters: Type.Object({
			action: Type.Union([
				Type.Literal("list"),
				Type.Literal("detail"),
				Type.Literal("create"),
				Type.Literal("edit"),
				Type.Literal("delete"),
				Type.Literal("trigger"),
				Type.Literal("runs"),
			]),
			name: Type.Optional(Type.String({ description: "Job name or ID (for detail/edit/delete/trigger/runs)" })),
			project: Type.Optional(Type.String({ description: "Project name (defaults to current project)" })),
			message: Type.Optional(Type.String({ description: "Prompt message (for create/edit)" })),
			scheduleType: Type.Optional(Type.Union([
				Type.Literal("cron"),
				Type.Literal("every"),
				Type.Literal("at"),
				Type.Literal("manual"),
			])),
			cronExpr: Type.Optional(Type.String({ description: "Cron expression e.g. '0 9 * * *'" })),
			everySeconds: Type.Optional(Type.Number({ description: "Interval in seconds" })),
			atTime: Type.Optional(Type.String({ description: "One-shot time (RFC3339)" })),
			description: Type.Optional(Type.String({ description: "Job description" })),
			model: Type.Optional(Type.String({ description: "Model override" })),
			enabled: Type.Optional(Type.Boolean({ description: "Enable/disable the job" })),
		}),
		execute: async (args: any) => {
			const a = args as {
				action: string; name?: string; project?: string; message?: string;
				scheduleType?: string; cronExpr?: string; everySeconds?: number;
				atTime?: string; description?: string; model?: string; enabled?: boolean;
			};
			try {
				const c = getClient();
				switch (a.action) {
					case "list": {
						const jobs = await c.listJobs();
						const lines = jobs.map(j =>
							`- ${j.name} (${j.id.slice(0,8)}) schedule=${j.scheduleType} enabled=${j.enabled} status=${j.status}`
						);
						return { content: [{ type: "text", text: lines.length ? lines.join("\n") : "No jobs found." }] };
					}
					case "detail": {
						if (!a.name) return { content: [{ type: "text", text: "Error: name or ID required for detail" }] };
						const jobs = await c.listJobs();
						const job = resolveJob(jobs, a.name);
						if (!job) return { content: [{ type: "text", text: `Job '${a.name}' not found` }] };
						const detail = [
							`Name: ${job.name} (${job.id})`,
							`Project: ${job.project}`,
							`Schedule: ${job.scheduleType} ${job.cronExpr || job.everySeconds ? job.cronExpr || `every ${job.everySeconds}s` : ""}`,
							`Enabled: ${job.enabled}  Status: ${job.status}`,
							`Message: ${job.message.slice(0, 200)}`,
							job.lastRunAt ? `Last run: ${job.lastRunAt} (${job.lastRunStatus})` : "No runs yet",
						].join("\n");
						return { content: [{ type: "text", text: detail }] };
					}
					case "create": {
						if (!a.name || !a.project || !a.message)
							return { content: [{ type: "text", text: "Error: name, project, and message required for create" }] };
						const req: CreateJobRequest = {
							name: a.name, project: a.project, message: a.message,
							scheduleType: (a.scheduleType as ScheduleType) || "manual",
							cronExpr: a.cronExpr, everySeconds: a.everySeconds,
							atTime: a.atTime, description: a.description, model: a.model,
							enabled: a.enabled !== false,
						};
						const job = await c.createJob(req);
						return { content: [{ type: "text", text: `Created job '${job.name}' (${job.id})` }] };
					}
					case "edit": {
						if (!a.name) return { content: [{ type: "text", text: "Error: name or ID required for edit" }] };
						const jobs = await c.listJobs();
						const existing = resolveJob(jobs, a.name);
						if (!existing) return { content: [{ type: "text", text: `Job '${a.name}' not found` }] };
						const update: Record<string, any> = {};
						if (a.message) update.message = a.message;
						if (a.project) update.project = a.project;
						if (a.model) update.model = a.model;
						if (a.description !== undefined) update.description = a.description;
						if (a.enabled !== undefined) update.enabled = a.enabled;
						if (a.scheduleType) update.scheduleType = a.scheduleType;
						if (a.cronExpr) update.cronExpr = a.cronExpr;
						if (a.everySeconds) update.everySeconds = a.everySeconds;
						if (a.atTime) update.atTime = a.atTime;
						const updated = await c.updateJob(existing.id, update);
						return { content: [{ type: "text", text: `Updated job '${updated.name}'` }] };
					}
					case "delete": {
						if (!a.name) return { content: [{ type: "text", text: "Error: name or ID required for delete" }] };
						const jobs = await c.listJobs();
						const job = resolveJob(jobs, a.name);
						if (!job) return { content: [{ type: "text", text: `Job '${a.name}' not found` }] };
						await c.deleteJob(job.id);
						return { content: [{ type: "text", text: `Deleted job '${job.name}'` }] };
					}
					case "trigger": {
						if (!a.name) return { content: [{ type: "text", text: "Error: name or ID required for trigger" }] };
						const jobs = await c.listJobs();
						const job = resolveJob(jobs, a.name);
						if (!job) return { content: [{ type: "text", text: `Job '${a.name}' not found` }] };
						const msg = await c.triggerJob(job.id);
						return { content: [{ type: "text", text: msg }] };
					}
					case "runs": {
						let jobId: string | undefined;
						if (a.name) {
							const jobs = await c.listJobs();
							const job = resolveJob(jobs, a.name);
							jobId = job?.id;
						}
						const runs = await c.listRuns(jobId, 10);
						if (runs.length === 0) return { content: [{ type: "text", text: "No runs found." }] };
						const lines = runs.map(r => {
							const ts = new Date(r.ts * 1000).toISOString().slice(0, 19);
							return `- ${ts} ${r.status || "?"} ${r.summary?.slice(0, 80) || r.error?.slice(0, 80) || ""}`;
						});
						return { content: [{ type: "text", text: lines.join("\n") }] };
					}
					default:
						return { content: [{ type: "text", text: `Unknown action: ${a.action}` }] };
				}
			} catch (err: any) {
				return { content: [{ type: "text", text: `Error: ${err.message}` }], isError: true };
			}
		},
		renderCall: (args, _theme) => {
			const a = args as { action: string; name?: string };
			const target = a.name ? ` ${a.name}` : "";
			const glyphs: Record<string, string> = {
				list: "\u{1F4CB}",
				trigger: "\u25B6",
				detail: "\u{1F50D}",
				runs: "\u{1F4DC}",
				create: "\u2795",
				edit: "\u270F",
				delete: "\u{1F5D1}",
			};
			return `${glyphs[a.action] || "\u2699"} scheduler ${a.action}${target}`;
		},
		renderResult: (result: any, _options, theme) => {
			const c = new Container();
			const content = result?.content;
			if (!Array.isArray(content) || content.length === 0) return undefined;

			const text = content
				.filter((b: any) => b.type === "text" && b.text)
				.map((b: any) => b.text)
				.join("\n");
			if (!text) return undefined;

			const isError = result?.isError === true;
			const dot = isError ? theme.fg("error", "●") : theme.fg("success", "●");
			const firstLine = text.split("\n")[0] || "";

			c.addChild(new Text(`${dot} ${theme.fg("toolTitle", theme.bold("scheduler"))}`, 1, 0));
			c.addChild(new Text(theme.fg("dim", `  ${firstLine.slice(0, 120)}`), 1, 0));

			return c;
		},
	});
}
