/**
 * PUX Scheduler Extension — TUI Commands
 *
 * Adds /scheduler command to the TUI for job management.
 * Connects to the Go backend scheduler API for CRUD, triggering, and run history.
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

// ── Key=value arg parser ─────────────────────────────────────
function parseKVArgs(raw: string): Record<string, string> {
	const result: Record<string, string> = {};
	const regex = /(\w+)=(?:"([^"]*)"|'([^']*)'|(\S+))/g;
	let m: RegExpExecArray | null;
	while ((m = regex.exec(raw)) !== null) {
		result[m[1]] = m[2] ?? m[3] ?? m[4] ?? "";
	}
	return result;
}

// ── Human-friendly duration parser ────────────────────────────
function parseDuration(s: string): number | null {
	const match = s.match(/^(\d+(?:\.\d+)?)\s*(ms|s|m|h|d)?$/);
	if (!match) return null;
	const val = parseFloat(match[1]);
	const unit = match[2] || "s";
	switch (unit) {
		case "ms": return Math.round(val / 1000);
		case "s": return Math.round(val);
		case "m": return Math.round(val * 60);
		case "h": return Math.round(val * 3600);
		case "d": return Math.round(val * 86400);
		default: return null;
	}
}

// ── Cron expression validator (6-field with seconds) ──────────
function isValidCron(expr: string): boolean {
	const parts = expr.trim().split(/\s+/);
	if (parts.length !== 6) return false;
	const re = /^(\d+|\*|\?|\/\d+|\d+-\d+|\d+\/\d+|L|W|#\d+|MON|TUE|WED|THU|FRI|SAT|SUN)$/;
	return parts.every(p => re.test(p));
}

function resolveJob(jobs: SchedulerJob[], nameOrId: string): SchedulerJob | undefined {
	return jobs.find(j => j.name === nameOrId || j.id === nameOrId);
}

const CREATE_USAGE = [
	"\x1b[33m  Usage: /scheduler create name=\"Job Name\" project=<project> message=\"prompt\" [options]\x1b[0m",
	"",
	"  Required:  name, project, message",
	"  Schedule:  cron=\"0 0 9 * * *\" | every=1h | every=30m | at=\"2026-06-01T09:00:00Z\" | manual",
	"  Optional:  model=<model> enabled=true description=\"...\" timezone=\"America/New_York\"",
	"",
].join("\n");

const EDIT_USAGE = [
	"\x1b[33m  Usage: /scheduler edit <name> [key=value ...]\x1b[0m",
	"",
	"  Editable fields: name, message, project, cron, every, model, enabled, description, timezone",
	"  Intervals: every=30s | every=5m | every=1h",
	"  Example:",
	"    \x1b[36m/scheduler edit \"Daily Report\" cron=\"0 0 10 * * *\" message=\"New prompt\"\x1b[0m",
	"    \x1b[36m/scheduler edit \"Health Check\" every=10m\x1b[0m",
	"",
].join("\n");

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

	// ── /scheduler command ────────────────────────────────────
	pi.registerCommand("scheduler", {
		description: "Manage scheduled jobs: list, create, edit, delete, trigger, enable, disable, runs, detail",
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
					case "create":
					case "new": {
						if (!arg1) {
							process.stdout.write(CREATE_USAGE);
							break;
						}
						const kv = parseKVArgs(arg1);
						if (!kv.name || !kv.project || !kv.message) {
							process.stdout.write(CREATE_USAGE);
							break;
						}
						let scheduleType: ScheduleType = "manual";
						let cronExpr: string | undefined;
						let everySeconds: number | undefined;
						let atTime: string | undefined;
						if (kv.cron) {
							scheduleType = "cron";
							cronExpr = kv.cron;
							if (!isValidCron(cronExpr)) {
								process.stdout.write(`\x1b[31m  Invalid cron: ${cronExpr}. Need 6 fields (with seconds) like '0 0 9 * * *'\x1b[0m\n`);
								break;
							}
						} else if (kv.every) {
							scheduleType = "every";
							const parsed = parseDuration(kv.every);
							if (parsed === null || parsed <= 0) {
								process.stdout.write(`\x1b[31m  Invalid interval: ${kv.every}. Use 30s, 5m, 1h, 2d\x1b[0m\n`);
								break;
							}
							everySeconds = parsed;
						} else if (kv.at) {
							scheduleType = "at";
							atTime = kv.at;
						}

						const req: CreateJobRequest = {
							name: kv.name,
							project: kv.project,
							message: kv.message,
							scheduleType,
							cronExpr,
							everySeconds,
							atTime,
							description: kv.description,
							model: kv.model,
							timezone: kv.timezone,
							enabled: kv.enabled !== "false",
						};
						const job = await getClient().createJob(req);
						process.stdout.write(`  \x1b[32m✓ Created job '${job.name}' (${job.id})\x1b[0m\n`);
						refreshStatus(ctx);
						break;
					}
					case "edit":
					case "update": {
						const editParts = arg1.match(/^(?:"([^"]+)"|'([^']+)'|(\S+))\s*(.*)/);
						if (!editParts) {
							process.stdout.write(EDIT_USAGE);
							break;
						}
						const jobRef = editParts[1] ?? editParts[2] ?? editParts[3];
						const kvRaw = editParts[4] || "";
						const kv = parseKVArgs(kvRaw);
						if (Object.keys(kv).length === 0) {
							process.stdout.write(EDIT_USAGE);
							break;
						}
						const jobs = await fetchJobs(ctx);
						if (!jobs) break;
						const existing = resolveJob(jobs, jobRef);
						if (!existing) {
							process.stdout.write(`\x1b[31m  Job '${jobRef}' not found.\x1b[0m\n`);
							break;
						}
						const update: Record<string, any> = {};
						if (kv.name) update.name = kv.name;
						if (kv.message) update.message = kv.message;
						if (kv.project) update.project = kv.project;
						if (kv.model) update.model = kv.model;
						if (kv.description !== undefined) update.description = kv.description;
						if (kv.timezone) update.timezone = kv.timezone;
						if (kv.enabled !== undefined) update.enabled = kv.enabled !== "false";
						if (kv.cron) {
							if (!isValidCron(kv.cron)) {
								process.stdout.write(`\x1b[31m  Invalid cron: ${kv.cron}. Need 6 fields (with seconds) like '0 0 9 * * *'\x1b[0m\n`);
								break;
							}
							update.scheduleType = "cron"; update.cronExpr = kv.cron;
						} else if (kv.every) {
							const parsed = parseDuration(kv.every);
							if (parsed === null || parsed <= 0) {
								process.stdout.write(`\x1b[31m  Invalid interval: ${kv.every}. Use 30s, 5m, 1h, 2d\x1b[0m\n`);
								break;
							}
							update.scheduleType = "every"; update.everySeconds = parsed;
						} else if (kv.at) { update.scheduleType = "at"; update.atTime = kv.at; }

						const updated = await getClient().updateJob(existing.id, update);
						process.stdout.write(`  \x1b[32m✓ Updated job '${updated.name}'\x1b[0m\n`);
						refreshStatus(ctx);
						break;
					}
					case "delete":
					case "rm": {
						if (!arg1) {
							process.stdout.write("\x1b[33m  Usage: /scheduler delete <name>\x1b[0m\n");
							break;
						}
						const jobs = await fetchJobs(ctx);
						if (!jobs) break;
						const job = resolveJob(jobs, arg1);
						if (!job) {
							process.stdout.write(`\x1b[31m  Job '${arg1}' not found.\x1b[0m\n`);
							break;
						}
						await getClient().deleteJob(job.id);
						process.stdout.write(`  \x1b[32m✓ Deleted job '${job.name}'\x1b[0m\n`);
						refreshStatus(ctx);
						break;
					}
					case "enable":
					case "disable": {
						if (!arg1) {
							process.stdout.write(`\x1b[33m  Usage: /scheduler ${subcmd} <name>\x1b[0m\n`);
							break;
						}
						const jobs = await fetchJobs(ctx);
						if (!jobs) break;
						const job = resolveJob(jobs, arg1);
						if (!job) {
							process.stdout.write(`\x1b[31m  Job '${arg1}' not found.\x1b[0m\n`);
							break;
						}
						const enable = subcmd === "enable";
						await getClient().updateJob(job.id, { enabled: enable });
						process.stdout.write(`  \x1b[32m✓ ${enable ? "Enabled" : "Disabled"} '${job.name}'\x1b[0m\n`);
						refreshStatus(ctx);
						break;
					}
					default:
						process.stdout.write(
							`\x1b[33m  Unknown subcommand '${subcmd}'.\x1b[0m\n` +
							"  Usage: /scheduler [list|detail|create|edit|delete|trigger|enable|disable|runs]\n"
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
