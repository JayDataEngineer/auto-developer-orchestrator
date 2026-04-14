/**
 * Cron Tool Extension
 *
 * Allows agents to create, manage, and monitor scheduled jobs.
 * The agent can schedule tasks like "check email every 5 minutes" or
 * "run a weather report daily at 8am".
 *
 * All calls go to the orchestrator API (via ORCHESTRATOR_API_HOST env, default localhost:3847).
 */

import type { ExtensionAPI, ExtensionContext, Theme } from "@mariozechner/pi-coding-agent";
import { Text } from "@mariozechner/pi-tui";
import { Type } from "@sinclair/typebox";

// ─── API Helper ────────────────────────────────────────────────

const API_HOST = process.env.ORCHESTRATOR_API_HOST || "localhost:3847";

function callApi(endpoint: string, method = "GET", body?: Record<string, unknown>): string {
	const args: string[] = ["-s"];
	if (method !== "GET") args.push("-X", method);
	if (body) args.push("-d", JSON.stringify(body));
	args.push(`http://${API_HOST}${endpoint}`);
	const result = require("child_process").spawnSync("curl", args, { encoding: "utf-8", timeout: 15000 });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(result.stderr);
	return result.stdout;
}

// ─── Extension ─────────────────────────────────────────────────

export default function (pi: ExtensionAPI) {
	pi.registerTool({
		name: "cron",
		label: "Cron Jobs",
		description: "Create, manage, and monitor scheduled jobs. Schedule tasks like weather checks, email monitoring, or periodic reports.",
		parameters: Type.Object({
			action: Type.Union([
				Type.Literal("list"),
				Type.Literal("add"),
				Type.Literal("update"),
				Type.Literal("remove"),
				Type.Literal("run"),
				Type.Literal("runs"),
				Type.Literal("status"),
			], { description: "Action to perform" }),
			// For "add"
			name: Type.Optional(Type.String({ description: "Job name (for add)" })),
			schedule: Type.Optional(Type.String({ description: "Schedule: 'every N seconds', 'at YYYY-MM-DDTHH:MM:SSZ', or cron expression 'sec min hour day month weekday'" })),
			message: Type.Optional(Type.String({ description: "The prompt message for the job to execute" })),
			model: Type.Optional(Type.String({ description: "Model to use (default: gemma-4-26b)" })),
			jobId: Type.Optional(Type.String({ description: "Job ID (for update, remove, run, runs)" })),
			// For "update"
			patch: Type.Optional(Type.Object({
				name: Type.Optional(Type.String()),
				message: Type.Optional(Type.String()),
				schedule: Type.Optional(Type.String()),
				enabled: Type.Optional(Type.Boolean()),
			}, { description: "Fields to update (for update action)" })),
			// For "runs"
			limit: Type.Optional(Type.Number({ description: "Number of runs to return (default: 20)" })),
			statusFilter: Type.Optional(Type.String({ description: "Filter by status: ok, error, all" })),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const action = params.action;

			try {
				switch (action) {
					case "list": {
						const raw = callApi("/api/scheduler/");
						const data = JSON.parse(raw);
						const jobs = data.jobs || [];
						if (jobs.length === 0) {
							return { content: [{ type: "text", text: "No scheduled jobs. Use action='add' to create one." }] };
						}
						const lines = jobs.map((j: any) =>
							`${j.enabled ? "✅" : "⏸️"} ${j.name} (${j.id})\n` +
							`   Schedule: ${j.scheduleType} ${j.cronExpr || (j.everySeconds ? `every ${j.everySeconds}s` : j.atTime || "")}\n` +
							`   Next run: ${j.nextRunAt?.slice(0, 19) || "—"}\n` +
							`   Last: ${j.lastRunStatus || "never"}${j.lastError ? " — " + j.lastError.slice(0, 100) : ""}`
						);
						return { content: [{ type: "text", text: `Scheduled jobs (${jobs.length}):\n\n${lines.join("\n\n")}` }] };
					}

					case "add": {
						if (!params.name || !params.schedule || !params.message) {
							return {
								content: [{ type: "text", text: "Missing required fields: name, schedule, message.\n" +
									"Example: {action:'add', name:'Weather Check', schedule:'every 300', message:'Check the weather forecast.'}" }],
								isError: true,
							};
						}
						const req: any = {
							name: params.name,
							message: params.message,
							project: ctx.cwd.split("/").pop() || "test-repo",
							enabled: true,
							model: params.model || "gemma-4-26b",
						};
						// Parse schedule
						const sched = params.schedule.toLowerCase();
						if (sched.startsWith("every ")) {
							const secs = parseInt(sched.replace("every ", "").replace(/\s*(seconds?|s)$/, "").trim());
							req.scheduleType = "every";
							req.everySeconds = isNaN(secs) ? 300 : secs;
						} else if (sched.startsWith("at ")) {
							req.scheduleType = "at";
							req.atTime = sched.replace("at ", "").trim();
						} else {
							req.scheduleType = "cron";
							req.cronExpr = params.schedule;
						}
						const raw = callApi("/api/scheduler/", "POST", req);
						const data = JSON.parse(raw);
						if (data.success) {
							const job = data.job;
							return { content: [{ type: "text", text: `✅ Job created: "${job.name}" (${job.id})\nSchedule: ${job.scheduleType} ${job.cronExpr || `every ${job.everySeconds}s`}\nNext run: ${job.nextRunAt?.slice(0, 19) || "—"}` }] };
						}
						return { content: [{ type: "text", text: `Failed: ${data.error}` }], isError: true };
					}

					case "update": {
						if (!params.jobId || !params.patch) {
							return { content: [{ type: "text", text: "Missing required fields: jobId, patch.\n" +
								"Example: {action:'update', jobId:'job-xxx', patch:{enabled:false}}" }],
								isError: true };
						}
						const patch: any = {};
						if (params.patch.name) patch.name = params.patch.name;
						if (params.patch.message) { patch.message = params.patch.message; }
						if (params.patch.enabled !== undefined) patch.enabled = params.patch.enabled;
						if (params.patch.schedule) {
							const sched = params.patch.schedule.toLowerCase();
							if (sched.startsWith("every ")) {
								const secs = parseInt(sched.replace("every ", "").replace(/\s*(seconds?|s)$/, "").trim());
								patch.scheduleType = "every";
								patch.everySeconds = isNaN(secs) ? 300 : secs;
							} else if (sched.startsWith("at ")) {
								patch.scheduleType = "at";
								patch.atTime = sched.replace("at ", "").trim();
							} else {
								patch.scheduleType = "cron";
								patch.cronExpr = params.patch.schedule;
							}
						}
						patch.enabled = patch.enabled !== undefined ? patch.enabled : true;
						const raw = callApi(`/api/scheduler/${params.jobId}`, "PUT", patch);
						const data = JSON.parse(raw);
						if (data.success) {
							return { content: [{ type: "text", text: `✅ Job updated: ${params.jobId}` }] };
						}
						return { content: [{ type: "text", text: `Failed: ${data.error}` }], isError: true };
					}

					case "remove": {
						if (!params.jobId) {
							return { content: [{ type: "text", text: "Missing required field: jobId" }], isError: true };
						}
						callApi(`/api/scheduler/${params.jobId}`, "DELETE");
						return { content: [{ type: "text", text: `✅ Job deleted: ${params.jobId}` }] };
					}

					case "run": {
						if (!params.jobId) {
							return { content: [{ type: "text", text: "Missing required field: jobId" }], isError: true };
						}
						callApi(`/api/scheduler/${params.jobId}/trigger`, "POST");
						return { content: [{ type: "text", text: `✅ Job triggered: ${params.jobId}. Results will appear in the run log.` }] };
					}

					case "runs": {
						const jobId = params.jobId;
						const limit = params.limit || 20;
						const endpoint = jobId
							? `/api/scheduler/${jobId}/runs?limit=${limit}${params.statusFilter ? `&status=${params.statusFilter}` : ""}`
							: `/api/scheduler/runs?limit=${limit}${params.statusFilter ? `&status=${params.statusFilter}` : ""}`;
						const raw = callApi(endpoint);
						const data = JSON.parse(raw);
						const runs = data.runs || [];
						if (runs.length === 0) {
							return { content: [{ type: "text", text: jobId ? "No runs for this job." : "No runs recorded." }] };
						}
						const lines = runs.map((r: any) =>
							`${r.status === "ok" ? "✅" : "❌"} ${new Date(r.runAtMs).toISOString().slice(0, 19)} — ${r.durationMs}ms\n` +
							`   ${r.summary?.slice(0, 200) || ""}${r.error ? "\n   Error: " + r.error.slice(0, 200) : ""}`
						);
						const title = jobId ? `Run history for ${jobId}` : "Recent runs across all jobs";
						return { content: [{ type: "text", text: `${title} (${runs.length}):\n\n${lines.join("\n\n")}` }] };
					}

					case "status": {
						const raw = callApi("/api/scheduler/");
						const data = JSON.parse(raw);
						const jobs = data.jobs || [];
						const active = jobs.filter((j: any) => j.enabled).length;
						const next = jobs.filter((j: any) => j.nextRunAt).sort((a: any, b: any) => (a.nextRunAt || "").localeCompare(b.nextRunAt || ""));
						const nextRun = next[0]?.nextRunAt?.slice(0, 19) || "—";
						const nextJob = next[0]?.name || "—";
						return { content: [{ type: "text", text: `Scheduler status:\n- Total jobs: ${jobs.length}\n- Active: ${active}\n- Next run: ${nextRun} (${nextJob})` }] };
					}

					default:
						return { content: [{ type: "text", text: `Unknown action: ${action}. Available: list, add, update, remove, run, runs, status` }], isError: true };
				}
			} catch (e: any) {
				return { content: [{ type: "text", text: `Cron tool error: ${e.message}` }], isError: true };
			}
		},

		renderCall(args, theme) {
			return new Text(theme.fg("muted", "📅 ") + theme.fg("toolTitle", `cron ${args.action || "?"}`), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			const msg = text?.type === "text" ? text.text : "";
			return new Text(theme.fg("muted", msg.slice(0, 80)), 0, 0);
		},
	});
}
