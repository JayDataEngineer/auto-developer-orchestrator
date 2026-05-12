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
import type { SchedulerJob } from "./types.js";

const STATUS_KEY = "pux-scheduler";

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
		description: "Manage scheduled jobs: list, trigger <name>, runs [name], detail <name>",
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
					default:
						process.stdout.write(
							`\x1b[33m  Unknown subcommand '${subcmd}'.\x1b[0m\n` +
							"  Usage: /scheduler [list|detail|trigger|runs]\n"
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
