/**
 * PUX Scheduler Extension
 *
 * Adds scheduler management to the TUI via /scheduler command.
 * Connects to the Go backend scheduler API for job CRUD, triggering, and run history.
 * Shows job count in the status bar.
 *
 * Render-only — the Go backend handles actual scheduling and execution.
 */

import { Type } from "@sinclair/typebox";
import type { ExtensionAPI, ExtensionCommandContext } from "../../src/core/extensions/types.js";
import { SchedulerClient } from "./api.js";
import { renderJobList, renderJobDetail, renderRunLog, renderStatusWidget } from "./render.js";
import type { SchedulerJob, CreateJobRequest, ScheduleType } from "./types.js";

const STATUS_KEY = "pux-scheduler";

// ── Key=value arg parser ─────────────────────────────────────
// Handles: name="Daily Report" project=myapp message="do thing" cron="0 9 * * *"
function parseKVArgs(raw: string): Record<string, string> {
	const result: Record<string, string> = {};
	const regex = /(\w+)=(?:"([^"]*)"|'([^']*)'|(\S+))/g;
	let m: RegExpExecArray | null;
	while ((m = regex.exec(raw)) !== null) {
		result[m[1]] = m[2] ?? m[3] ?? m[4] ?? "";
	}
	return result;
}

function resolveJob(jobs: SchedulerJob[], nameOrId: string): SchedulerJob | undefined {
	return jobs.find(j => j.name === nameOrId || j.id === nameOrId);
}

const CREATE_USAGE = [
	"\x1b[33m  Usage: /scheduler create name=\"Job Name\" project=<project> message=\"prompt\" [options]\x1b[0m",
	"",
	"  Required:  name, project, message",
	"  Schedule:  cron=\"0 9 * * *\" | every=3600 | at=\"2026-06-01T09:00:00Z\" | manual",
	"  Optional:  model=<model> enabled=true description=\"...\" timezone=\"America/New_York\"",
	"  Example:",
	"    \x1b[36m/scheduler create name=\"Daily Report\" project=myapp message=\"Summarize issues\" cron=\"0 9 * * *\"\x1b[0m",
	"",
].join("\n");

const EDIT_USAGE = [
	"\x1b[33m  Usage: /scheduler edit <name> [key=value ...]\x1b[0m",
	"",
	"  Editable fields: name, message, project, cron, every, model, enabled, description, timezone",
	"  Example:",
	"    \x1b[36m/scheduler edit \"Daily Report\" cron=\"0 10 * * *\" message=\"New prompt\"\x1b[0m",
	"",
].join("\n");

export default function registerPuxSchedulerExtension(pi: ExtensionAPI): void {
	let client: SchedulerClient | null = null;
	let lastJobs: SchedulerJob[] = [];
	let pollTimer: ReturnType<typeof setInterval> | undefined;

	// ── Resolve server URL ────────────────────────────────────
	// Extensions don't have direct access to CLI args, so we use
	// a well-known default. The Go backend runs on 3847.
	function getClient(): SchedulerClient {
		if (!client) client = new SchedulerClient("http://localhost:3847");
		return client;
	}

	// ── Status bar widget (polls every 30s) ───────────────────
	async function refreshStatus(ctx: any): Promise<void> {
		try {
			const jobs = await getClient().listJobs();
			lastJobs = jobs;
			const widget = renderStatusWidget(jobs);
			ctx.ui.setStatus(STATUS_KEY, widget || undefined);
		} catch {
			ctx.ui.setStatus(STATUS_KEY, undefined);
		}
	}

	// Start polling on session start
	pi.on("session_start", (_event, ctx) => {
		refreshStatus(ctx);
		if (pollTimer) clearInterval(pollTimer);
		pollTimer = setInterval(() => refreshStatus(ctx), 30_000);
	});

	// Stop polling on shutdown
	pi.on("session_shutdown", () => {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = undefined;
		}
	});

	// ── /scheduler command ────────────────────────────────────
	pi.registerCommand("scheduler", {
		description: "Manage scheduled jobs: list, create, edit, delete, trigger, enable, disable, runs, detail",
		handler: async (args: string, ctx: ExtensionCommandContext) => {
			const parts = args.trim().split(/\s+/);
			const subcmd = parts[0] || "list";
			const arg1 = parts.slice(1).join(" ");

			try {
				switch (subcmd) {
					case "list": {
						const jobs = await getClient().listJobs();
						lastJobs = jobs;
						process.stdout.write(renderJobList(jobs));
						break;
					}
					case "detail":
					case "get": {
						if (!arg1) {
							process.stdout.write("\x1b[33m  Usage: /scheduler detail <name>\x1b[0m\n");
							break;
						}
						const jobs = await getClient().listJobs();
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
						const jobs = await getClient().listJobs();
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
							const jobs = await getClient().listJobs();
							const job = jobs.find(j => j.name === arg1 || j.id === arg1);
							jobId = job?.id;
						}
						const runs = await getClient().listRuns(jobId, 20);
						process.stdout.write(renderRunLog(runs));
						break;
					}
					case "create":
					case "new": {
						const kv = parseKVArgs(arg1);
						if (!kv.name || !kv.project || !kv.message) {
							process.stdout.write(CREATE_USAGE);
							break;
						}
						// Determine schedule type
						let scheduleType: ScheduleType = "manual";
						let cronExpr: string | undefined;
						let everySeconds: number | undefined;
						let atTime: string | undefined;
						if (kv.cron) { scheduleType = "cron"; cronExpr = kv.cron; }
						else if (kv.every) { scheduleType = "every"; everySeconds = parseInt(kv.every, 10); }
						else if (kv.at) { scheduleType = "at"; atTime = kv.at; }

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
						process.stdout.write(renderJobDetail(job));
						refreshStatus(ctx);
						break;
					}
					case "edit":
					case "update": {
						// First token is the job name/ID, rest is key=value pairs
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
						const jobs = await getClient().listJobs();
						const existing = resolveJob(jobs, jobRef);
						if (!existing) {
							process.stdout.write(`\x1b[31m  Job '${jobRef}' not found.\x1b[0m\n`);
							break;
						}
						// Build update from kv pairs
						const update: Record<string, any> = {};
						if (kv.name) update.name = kv.name;
						if (kv.message) update.message = kv.message;
						if (kv.project) update.project = kv.project;
						if (kv.model) update.model = kv.model;
						if (kv.description !== undefined) update.description = kv.description;
						if (kv.timezone) update.timezone = kv.timezone;
						if (kv.enabled !== undefined) update.enabled = kv.enabled !== "false";
						// Schedule changes
						if (kv.cron) { update.scheduleType = "cron"; update.cronExpr = kv.cron; }
						else if (kv.every) { update.scheduleType = "every"; update.everySeconds = parseInt(kv.every, 10); }
						else if (kv.at) { update.scheduleType = "at"; update.atTime = kv.at; }

						const updated = await getClient().updateJob(existing.id, update);
						process.stdout.write(`  \x1b[32m✓ Updated job '${updated.name}'\x1b[0m\n`);
						process.stdout.write(renderJobDetail(updated));
						refreshStatus(ctx);
						break;
					}
					case "delete":
					case "rm": {
						if (!arg1) {
							process.stdout.write("\x1b[33m  Usage: /scheduler delete <name>\x1b[0m\n");
							break;
						}
						const jobs = await getClient().listJobs();
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
						const jobs = await getClient().listJobs();
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
				process.stdout.write(`\x1b[31m  Error: ${err.message}\x1b[0m\n`);
			}
		},
	});

	// ── scheduler tool (LLM can manage jobs) ──────────────────
	pi.registerTool({
		name: "scheduler",
		label: "Scheduler",
		description: "Manage scheduled jobs: list, trigger, create, delete. The Go backend handles execution.",
		parameters: Type.Object({
			action: Type.Union([
				Type.Literal("list"),
				Type.Literal("trigger"),
				Type.Literal("detail"),
				Type.Literal("runs"),
			]),
			name: Type.Optional(Type.String({ description: "Job name or ID (for trigger/detail/runs)" })),
		}),
		execute: async () => ({ content: [] }),
		renderCall: (args, _theme) => {
			const a = args as { action: string; name?: string };
			const target = a.name ? ` ${a.name}` : "";
			const glyphs: Record<string, string> = {
				list: "📋",
				trigger: "▶",
				detail: "🔍",
				runs: "📜",
			};
			return `${glyphs[a.action] || "⚙"} scheduler ${a.action}${target}`;
		},
		renderResult: (_result, _options, _theme) => {
			return undefined; // Let default tool result rendering handle it
		},
	});
}
