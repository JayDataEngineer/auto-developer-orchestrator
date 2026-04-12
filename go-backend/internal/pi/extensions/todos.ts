/**
 * Artifacts Extension — Plans, Todos & Scratch Pad
 *
 * Registers native Pi tools for task tracking, planning, and note-taking:
 *   - write_todos / read_todos: Structured task lists with checkbox format
 *   - write_plan / read_plan: Implementation plans
 *   - write_scratch_pad / read_scratch_pad: Research notes and observations
 *
 * All tools persist via the orchestrator's artifacts API.
 * Uses ARTIFACT_AGENT_ID env var (format: "projectName:agentId") to match
 * the frontend's artifact query format.
 */

import type { ExtensionAPI, ExtensionContext, Theme } from "@mariozechner/pi-coding-agent";
import { Text, truncateToWidth } from "@mariozechner/pi-tui";
import { Type } from "@sinclair/typebox";
import { execSync } from "node:child_process";

// ─── API Helper ────────────────────────────────────────────────

const API_HOST = process.env.ORCHESTRATOR_API_HOST || "localhost:3847";
const ARTIFACT_AGENT_ID = process.env.ARTIFACT_AGENT_ID || process.env.AGENT_ID || "default";

function callApi(endpoint: string, method = "GET", body?: Record<string, unknown>): string {
	const args: string[] = ["-s"];
	if (method !== "GET") args.push("-X", method);
	if (body) args.push("-d", JSON.stringify(body));
	args.push(`http://${API_HOST}${endpoint}`);
	return execSync("curl", args, { encoding: "utf-8", timeout: 10000 });
}

// ─── Types ─────────────────────────────────────────────────────

interface Todo {
	text: string;
	status: "pending" | "in_progress" | "done";
}

interface TodoDetails {
	action: "write" | "read";
	todos: Todo[];
}

function parseTodos(markdown: string): Todo[] {
	const lines = markdown.split("\n").filter((l) => l.trim().startsWith("- ["));
	const todos: Todo[] = [];
	for (const line of lines) {
		const match = line.match(/- \[([ x>])\] (.+)/);
		if (match) {
			const statusMap: Record<string, Todo["status"]> = { x: "done", ">": "in_progress", " ": "pending" };
			todos.push({ text: match[2], status: statusMap[match[1]] || "pending" });
		}
	}
	return todos;
}

// ─── Extension ─────────────────────────────────────────────────

export default function (pi: ExtensionAPI) {
	// ── Write Todos ──

	pi.registerTool({
		name: "write_todos",
		label: "Write Todos",
		description:
			"Save or update the todo list. Use checkbox format: - [ ] for pending, - [>] for in-progress, - [x] for done.",
		parameters: Type.Object({
			todos: Type.String({
				description:
					"Todo list in markdown checkbox format. Each line: '- [ ] task', '- [>] task', or '- [x] task'.",
			}),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			try {
				const todos = parseTodos(params.todos);

				callApi("/api/pi/artifacts", "POST", {
					agentId: ARTIFACT_AGENT_ID,
					type: "todo",
					title: "Tasks",
					content: params.todos,
				});

				return {
					content: [{ type: "text", text: `Saved ${todos.length} todos.` }],
					details: { action: "write", todos } as TodoDetails,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to save todos: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("write_todos")), 0, 0);
		},

		renderResult(result, _opts, theme, _ctx) {
			const details = result.details as TodoDetails | undefined;
			if (!details || details.action !== "write") {
				const text = result.content[0];
				return new Text(text?.type === "text" ? text.text : "", 0, 0);
			}
			const done = details.todos.filter((t) => t.status === "done").length;
			const total = details.todos.length;
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", `${done}/${total} todos saved`), 0, 0);
		},
	});

	// ── Read Todos ──

	pi.registerTool({
		name: "read_todos",
		label: "Read Todos",
		description: "Load the current todo list.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			try {
				const raw = callApi(`/api/pi/artifacts?agentId=${encodeURIComponent(ARTIFACT_AGENT_ID)}`);
				const data = JSON.parse(raw);
				const todoArtifact = data.artifacts?.find((a: any) => a.type === "todo");
				if (!todoArtifact) {
					return {
						content: [{ type: "text", text: "No todos yet." }],
						details: { action: "read", todos: [] } as TodoDetails,
					};
				}

				const todos = parseTodos(todoArtifact.content);

				return {
					content: [{ type: "text", text: todoArtifact.content }],
					details: { action: "read", todos } as TodoDetails,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to read todos: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("read_todos")), 0, 0);
		},

		renderResult(result, _opts, theme, _ctx) {
			const details = result.details as TodoDetails | undefined;
			if (!details) {
				const text = result.content[0];
				return new Text(text?.type === "text" ? text.text : "", 0, 0);
			}
			const done = details.todos.filter((t) => t.status === "done").length;
			const total = details.todos.length;
			return new Text(
				theme.fg("muted", `${done}/${total} todos completed`) +
					(details.todos.length > 0 ? "" : theme.fg("dim", " — no todos")),
				0,
				0,
			);
		},
	});

	// ── Write Plan ──

	pi.registerTool({
		name: "write_plan",
		label: "Write Plan",
		description:
			"Save or update an implementation plan. Use markdown headings and lists to structure the plan with steps, files to modify, and rationale.",
		parameters: Type.Object({
			title: Type.String({ description: "Short title for the plan, e.g. 'Refactor Auth Module'." }),
			content: Type.String({ description: "Plan content in markdown format." }),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			try {
				callApi("/api/pi/artifacts", "POST", {
					agentId: ARTIFACT_AGENT_ID,
					type: "plan",
					title: params.title,
					content: params.content,
				});
				const lines = params.content.split("\n").length;
				return {
					content: [{ type: "text", text: `Plan "${params.title}" saved (${lines} lines).` }],
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to save plan: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("write_plan")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Read Plan ──

	pi.registerTool({
		name: "read_plan",
		label: "Read Plan",
		description: "Load the current implementation plan.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			try {
				const raw = callApi(`/api/pi/artifacts?agentId=${encodeURIComponent(ARTIFACT_AGENT_ID)}`);
				const data = JSON.parse(raw);
				const planArtifact = data.artifacts?.find((a: any) => a.type === "plan");
				if (!planArtifact) {
					return {
						content: [{ type: "text", text: "No plan yet." }],
					};
				}
				return {
					content: [{ type: "text", text: planArtifact.content }],
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to read plan: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("read_plan")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			const msg = text?.type === "text" ? text.text : "";
			return new Text(theme.fg("muted", msg ? `${msg.split("\n").length} lines in plan` : "No plan"), 0, 0);
		},
	});

	// ── Write Scratch Pad ──

	pi.registerTool({
		name: "write_scratch_pad",
		label: "Write Scratch Pad",
		description: "Save observations, notes, decisions, and discoveries. Use this to store context that might be useful later.",
		parameters: Type.Object({
			content: Type.String({ description: "Markdown content for the scratch pad." }),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			try {
				callApi("/api/pi/artifacts", "POST", {
					agentId: ARTIFACT_AGENT_ID,
					type: "notes",
					title: "Scratch Pad",
					content: params.content,
				});
				const lines = params.content.split("\n").length;
				return {
					content: [{ type: "text", text: `Saved scratch pad (${lines} lines).` }],
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to save scratch pad: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("write_scratch_pad")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Read Scratch Pad ──

	pi.registerTool({
		name: "read_scratch_pad",
		label: "Read Scratch Pad",
		description: "Load the scratch pad notes.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			try {
				const raw = callApi(`/api/pi/artifacts?agentId=${encodeURIComponent(ARTIFACT_AGENT_ID)}`);
				const data = JSON.parse(raw);
				const notesArtifact = data.artifacts?.find((a: any) => a.type === "notes" && a.title === "Scratch Pad");
				if (!notesArtifact) {
					return {
						content: [{ type: "text", text: "Scratch pad is empty." }],
					};
				}
				return {
					content: [{ type: "text", text: notesArtifact.content }],
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to read scratch pad: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("read_scratch_pad")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			const msg = text?.type === "text" ? text.text : "";
			const lines = msg.split("\n").length;
			return new Text(theme.fg("muted", `${lines} lines in scratch pad`), 0, 0);
		},
	});
}
