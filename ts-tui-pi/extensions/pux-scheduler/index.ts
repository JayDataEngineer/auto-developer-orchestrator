/**
 * PUX Scheduler Extension — Interactive TUI
 *
 * Adds scheduler management to the TUI via /scheduler command.
 * Connects to the Go backend scheduler API for job CRUD, triggering, and run history.
 * Shows job count in the status bar with adaptive polling.
 *
 * Interactive features:
 * - /scheduler (no args) → interactive menu
 * - Guided creation wizard
 * - Job picker for detail/trigger/delete
 * - Inline tool rendering in chat
 */

import { Type } from "@sinclair/typebox";
import { Container, Text } from "@mariozechner/pi-tui";
import type { ExtensionAPI, ExtensionCommandContext } from "../../src/core/extensions/types.js";
import { SchedulerClient } from "./api.js";
import {
	renderJobList, renderJobDetail, renderRunLog, renderStatusWidget,
	formatSchedule, formatJobOption, hasRunningJobs,
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

// ── Cron expression validator ─────────────────────────────────
function isValidCron(expr: string): boolean {
	const parts = expr.trim().split(/\s+/);
	if (parts.length < 5 || parts.length > 6) return false;
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
	"  Schedule:  cron=\"0 9 * * *\" | every=1h | every=30m | at=\"2026-06-01T09:00:00Z\" | manual",
	"  Optional:  model=<model> enabled=true description=\"...\" timezone=\"America/New_York\"",
	"  Or just:   \x1b[36m/scheduler create\x1b[0m for a guided wizard",
	"",
].join("\n");

const EDIT_USAGE = [
	"\x1b[33m  Usage: /scheduler edit <name> [key=value ...]\x1b[0m",
	"",
	"  Editable fields: name, message, project, cron, every, model, enabled, description, timezone",
	"  Intervals: every=30s | every=5m | every=1h",
	"  Example:",
	"    \x1b[36m/scheduler edit \"Daily Report\" cron=\"0 10 * * *\" message=\"New prompt\"\x1b[0m",
	"    \x1b[36m/scheduler edit \"Health Check\" every=10m\x1b[0m",
	"",
].join("\n");

export default function registerPuxSchedulerExtension(pi: ExtensionAPI): void {
	let client: SchedulerClient | null = null;
	let lastJobs: SchedulerJob[] = [];
	let pollTimer: ReturnType<typeof setInterval> | undefined;

	function getClient(): SchedulerClient {
		if (!client) client = new SchedulerClient("http://localhost:3847");
		return client;
	}

	// ── Status bar widget with adaptive polling ────────────
	async function refreshStatus(ctx: any): Promise<void> {
		try {
			const jobs = await getClient().listJobs();
			lastJobs = jobs;
			const widget = renderStatusWidget(jobs);
			ctx.ui.setStatus(STATUS_KEY, widget || undefined);

			// Adaptive polling: 10s when jobs running, 30s otherwise
			if (pollTimer) clearInterval(pollTimer);
			const interval = hasRunningJobs(jobs) ? 10_000 : 30_000;
			pollTimer = setInterval(() => refreshStatus(ctx), interval);
		} catch {
			ctx.ui.setStatus(STATUS_KEY, undefined);
		}
	}

	pi.on("session_start", (_event, ctx) => {
		refreshStatus(ctx);
		// Initial poll; refreshStatus sets adaptive interval
	});

	pi.on("session_shutdown", () => {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = undefined;
		}
	});

	// ── Interactive flows ──────────────────────────────────

	/** Main menu — shown when user types /scheduler with no args */
	async function interactiveMenu(ctx: ExtensionCommandContext): Promise<void> {
		const choice = await ctx.ui.select("Scheduler", [
			"List all jobs",
			"Create new job",
			"View job details",
			"Trigger a job",
			"View run history",
		]);
		if (!choice) return;

		switch (choice) {
			case "List all jobs":
				await interactiveJobList(ctx);
				break;
			case "Create new job":
				await interactiveCreateWizard(ctx);
				break;
			case "View job details":
				await interactivePickJob(ctx, "Select a job to view", async (job) => {
					process.stdout.write(renderJobDetail(job));
				});
				break;
			case "Trigger a job":
				await interactivePickJob(ctx, "Select a job to trigger", async (job) => {
					const ok = await ctx.ui.confirm(`Trigger '${job.name}'?`, "This will run the job immediately.");
					if (ok) {
						const msg = await getClient().triggerJob(job.id);
						ctx.ui.notify(msg, "info");
						refreshStatus(ctx);
					}
				});
				break;
			case "View run history":
				await interactivePickJob(ctx, "Select a job (or cancel for all)", async (job) => {
					const runs = await getClient().listRuns(job?.id, 20);
					process.stdout.write(renderRunLog(runs));
				}, true); // optional = true, cancel shows all
				break;
		}
	}

	/** Job list as interactive picker */
	async function interactiveJobList(ctx: ExtensionCommandContext): Promise<void> {
		const jobs = await getClient().listJobs();
		lastJobs = jobs;
		if (jobs.length === 0) {
			ctx.ui.notify("No scheduled jobs yet. Create one!", "info");
			return;
		}

		const options = jobs.map(j => formatJobOption(j));
		const choice = await ctx.ui.select(`Jobs (${jobs.length})`, options);
		if (!choice) return;

		const idx = options.indexOf(choice);
		if (idx < 0) return;
		const job = jobs[idx];

		await interactiveJobActions(ctx, job);
	}

	/** Show actions for a specific job */
	async function interactiveJobActions(ctx: ExtensionCommandContext, job: SchedulerJob): Promise<void> {
		const choice = await ctx.ui.select(job.name, [
			"View details",
			"Trigger now",
			"Run history",
			job.enabled ? "Disable" : "Enable",
			"Delete",
		]);
		if (!choice) return;

		switch (choice) {
			case "View details":
				process.stdout.write(renderJobDetail(job));
				break;
			case "Trigger now": {
				const msg = await getClient().triggerJob(job.id);
				ctx.ui.notify(msg, "info");
				refreshStatus(ctx);
				break;
			}
			case "Run history": {
				const runs = await getClient().listRuns(job.id, 20);
				process.stdout.write(renderRunLog(runs));
				break;
			}
			case "Disable":
			case "Enable": {
				const enable = choice === "Enable";
				await getClient().updateJob(job.id, { enabled: enable });
				ctx.ui.notify(`${enable ? "Enabled" : "Disabled"} '${job.name}'`, "info");
				refreshStatus(ctx);
				break;
			}
			case "Delete": {
				const ok = await ctx.ui.confirm(`Delete '${job.name}'?`, "This cannot be undone.");
				if (ok) {
					await getClient().deleteJob(job.id);
					ctx.ui.notify(`Deleted '${job.name}'`, "info");
					refreshStatus(ctx);
				}
				break;
			}
		}
	}

	/** Pick a job from the list, then run an action */
	async function interactivePickJob(
		ctx: ExtensionCommandContext,
		title: string,
		action: (job: SchedulerJob | undefined) => Promise<void>,
		optional = false,
	): Promise<void> {
		const jobs = await getClient().listJobs();
		lastJobs = jobs;
		if (jobs.length === 0) {
			ctx.ui.notify("No scheduled jobs.", "info");
			return;
		}

		const options = jobs.map(j => formatJobOption(j));
		if (optional) options.unshift("All jobs");
		const choice = await ctx.ui.select(title, options);
		if (!choice) return;

		if (optional && choice === "All jobs") {
			await action(undefined);
			return;
		}

		const idx = options.indexOf(choice) - (optional ? 1 : 0);
		if (idx >= 0 && idx < jobs.length) {
			await action(jobs[idx]);
		}
	}

	/** Guided creation wizard */
	async function interactiveCreateWizard(ctx: ExtensionCommandContext): Promise<void> {
		// Step 1: Job name
		const name = await ctx.ui.input("Job name", "My Daily Task");
		if (!name) return;

		// Step 2: Prompt message
		const message = await ctx.ui.input("What should this job do?", "e.g. Summarize recent changes");
		if (!message) return;

		// Step 3: Schedule type
		const schedChoice = await ctx.ui.select("Schedule", [
			"Every interval (5m, 1h, 1d)",
			"Cron schedule (advanced)",
			"One-shot at specific time",
			"Manual (trigger only)",
		]);
		if (!schedChoice) return;

		let scheduleType: ScheduleType = "manual";
		let cronExpr: string | undefined;
		let everySeconds: number | undefined;
		let atTime: string | undefined;

		if (schedChoice.startsWith("Every")) {
			const interval = await ctx.ui.input("Interval", "5m");
			if (!interval) return;
			const parsed = parseDuration(interval);
			if (parsed === null || parsed <= 0) {
				ctx.ui.notify(`Invalid interval '${interval}'. Use 30s, 5m, 1h, 2d.`, "error");
				return;
			}
			scheduleType = "every";
			everySeconds = parsed;
		} else if (schedChoice.startsWith("Cron")) {
			const cron = await ctx.ui.input("Cron expression", "0 9 * * *");
			if (!cron) return;
			if (!isValidCron(cron)) {
				ctx.ui.notify(`Invalid cron: ${cron}. Need 5 fields like '0 9 * * *'`, "error");
				return;
			}
			scheduleType = "cron";
			cronExpr = cron;
		} else if (schedChoice.startsWith("One-shot")) {
			const time = await ctx.ui.input("When? (RFC3339)", "2026-06-01T09:00:00Z");
			if (!time) return;
			scheduleType = "at";
			atTime = time;
		}
		// else manual — no schedule fields needed

		// Step 4: Confirm
		const schedDesc = scheduleType === "cron" ? `cron: ${cronExpr}`
			: scheduleType === "every" ? `every ${everySeconds}s`
			: scheduleType === "at" ? `at ${atTime}`
			: "manual";
		const ok = await ctx.ui.confirm(
			`Create '${name}'?`,
			`Schedule: ${schedDesc}\nPrompt: ${message}`,
		);
		if (!ok) return;

		// Create
		const req: CreateJobRequest = {
			name,
			project: "default",
			message,
			scheduleType,
			cronExpr,
			everySeconds,
			atTime,
			enabled: true,
		};
		try {
			const job = await getClient().createJob(req);
			ctx.ui.notify(`Created '${job.name}' (${schedDesc})`, "info");
			refreshStatus(ctx);
		} catch (err: any) {
			ctx.ui.notify(`Failed: ${err.message}`, "error");
		}
	}

	// ── /scheduler command ────────────────────────────────────
	pi.registerCommand("scheduler", {
		description: "Manage scheduled jobs: list, create, edit, delete, trigger, enable, disable, runs, detail",
		handler: async (args: string, ctx: ExtensionCommandContext) => {
			const parts = args.trim().split(/\s+/);
			const subcmd = parts[0] || "";
			const arg1 = parts.slice(1).join(" ");

			// No args → interactive menu
			if (!subcmd) {
				if (ctx.hasUI) {
					await interactiveMenu(ctx);
				} else {
					const jobs = await getClient().listJobs();
					process.stdout.write(renderJobList(jobs));
				}
				return;
			}

			try {
				switch (subcmd) {
					case "list": {
						if (ctx.hasUI) {
							await interactiveJobList(ctx);
						} else {
							const jobs = await getClient().listJobs();
							lastJobs = jobs;
							process.stdout.write(renderJobList(jobs));
						}
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
						if (ctx.hasUI) {
							await interactiveJobActions(ctx, job);
						} else {
							process.stdout.write(renderJobDetail(job));
						}
						break;
					}
					case "trigger":
					case "run": {
						if (!arg1) {
							if (ctx.hasUI) {
								await interactivePickJob(ctx, "Select a job to trigger", async (job) => {
									if (!job) return;
									const msg = await getClient().triggerJob(job.id);
									ctx.ui.notify(msg, "info");
									refreshStatus(ctx);
								});
							} else {
								process.stdout.write("\x1b[33m  Usage: /scheduler trigger <name>\x1b[0m\n");
							}
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
						if (ctx.hasUI) {
							ctx.ui.notify(msg, "info");
						} else {
							process.stdout.write(`  \x1b[32m✓ ${msg}\x1b[0m\n`);
						}
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
						if (!arg1) {
							if (ctx.hasUI) {
								await interactiveCreateWizard(ctx);
							} else {
								process.stdout.write(CREATE_USAGE);
							}
							break;
						}
						const kv = parseKVArgs(arg1);
						if (!kv.name || !kv.project || !kv.message) {
							if (ctx.hasUI && !kv.name && !kv.project && !kv.message) {
								await interactiveCreateWizard(ctx);
							} else {
								process.stdout.write(CREATE_USAGE);
							}
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
								ctx.ui.notify(`Invalid cron: ${cronExpr}. Need 5 fields like '0 9 * * *'`, "error");
								break;
							}
						} else if (kv.every) {
							scheduleType = "every";
							const parsed = parseDuration(kv.every);
							if (parsed === null || parsed <= 0) {
								ctx.ui.notify(`Invalid interval: ${kv.every}. Use 30s, 5m, 1h, 2d`, "error");
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
						if (ctx.hasUI) {
							ctx.ui.notify(`Created '${job.name}' (${formatSchedule(job)})`, "info");
						} else {
							process.stdout.write(`  \x1b[32m✓ Created job '${job.name}' (${job.id})\x1b[0m\n`);
						}
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
						const jobs = await getClient().listJobs();
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
								ctx.ui.notify(`Invalid cron: ${kv.cron}`, "error");
								break;
							}
							update.scheduleType = "cron"; update.cronExpr = kv.cron;
						} else if (kv.every) {
							const parsed = parseDuration(kv.every);
							if (parsed === null || parsed <= 0) {
								ctx.ui.notify(`Invalid interval: ${kv.every}. Use 30s, 5m, 1h, 2d`, "error");
								break;
							}
							update.scheduleType = "every"; update.everySeconds = parsed;
						} else if (kv.at) { update.scheduleType = "at"; update.atTime = kv.at; }

						const updated = await getClient().updateJob(existing.id, update);
						if (ctx.hasUI) {
							ctx.ui.notify(`Updated '${updated.name}'`, "info");
						} else {
							process.stdout.write(`  \x1b[32m✓ Updated job '${updated.name}'\x1b[0m\n`);
						}
						refreshStatus(ctx);
						break;
					}
					case "delete":
					case "rm": {
						if (!arg1) {
							if (ctx.hasUI) {
								await interactivePickJob(ctx, "Select a job to delete", async (job) => {
									if (!job) return;
									const ok = await ctx.ui.confirm(`Delete '${job.name}'?`, "This cannot be undone.");
									if (ok) {
										await getClient().deleteJob(job.id);
										ctx.ui.notify(`Deleted '${job.name}'`, "info");
										refreshStatus(ctx);
									}
								});
							} else {
								process.stdout.write("\x1b[33m  Usage: /scheduler delete <name>\x1b[0m\n");
							}
							break;
						}
						const jobs = await getClient().listJobs();
						const job = resolveJob(jobs, arg1);
						if (!job) {
							process.stdout.write(`\x1b[31m  Job '${arg1}' not found.\x1b[0m\n`);
							break;
						}
						await getClient().deleteJob(job.id);
						if (ctx.hasUI) {
							ctx.ui.notify(`Deleted '${job.name}'`, "info");
						} else {
							process.stdout.write(`  \x1b[32m✓ Deleted job '${job.name}'\x1b[0m\n`);
						}
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
						if (ctx.hasUI) {
							ctx.ui.notify(`${enable ? "Enabled" : "Disabled"} '${job.name}'`, "info");
						} else {
							process.stdout.write(`  \x1b[32m✓ ${enable ? "Enabled" : "Disabled"} '${job.name}'\x1b[0m\n`);
						}
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
				if (ctx.hasUI) {
					ctx.ui.notify(`Error: ${err.message}`, "error");
				} else {
					process.stdout.write(`\x1b[31m  Error: ${err.message}\x1b[0m\n`);
				}
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
		execute: async () => ({ content: [] }),
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
