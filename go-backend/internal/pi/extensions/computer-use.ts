/**
 * Computer Use Extension
 *
 * Registers native Pi tools for desktop automation:
 *   - computer_use_enable: Start the desktop environment
 *   - computer_use_screenshot: Take a screenshot (returns base64 PNG)
 *   - computer_use_snapshot: Get page elements as structured data
 *   - computer_use_click: Click an element by ID
 *   - computer_use_type: Type text into an element
 *   - computer_use_navigate: Navigate to a URL
 *   - computer_use_scroll: Scroll the page
 *
 * All tools call the orchestrator's computer use API at 172.17.0.1:3847.
 */

import { StringEnum } from "@mariozechner/pi-ai";
import type { ExtensionAPI, ExtensionContext, Theme } from "@mariozechner/pi-coding-agent";
import { Text, truncateToWidth } from "@mariozechner/pi-tui";
import { Type } from "@sinclair/typebox";
import { execSync } from "node:child_process";

// ─── API Helper ────────────────────────────────────────────────

function callApi(endpoint: string, method = "GET", body?: Record<string, unknown>): string {
	const args: string[] = ["-s"];
	if (method !== "GET") args.push("-X", method);
	if (body) args.push("-d", JSON.stringify(body));
	args.push(`http://172.17.0.1:3847${endpoint}`);
	const result = require("child_process").spawnSync("curl", args, { encoding: "utf-8", timeout: 30000 }); if (result.error) throw result.error; if (result.status !== 0) throw new Error(result.stderr); return result.stdout;
}

// ─── Types ─────────────────────────────────────────────────────

interface ComputerUseResult {
	enabled: boolean;
	sandboxId: string;
	cdpPort: number;
	novncPort: number;
	viewerUrl: string;
}

interface ScreenshotResult {
	image: string;
	description?: string;
	url?: string;
	title?: string;
}

interface PageInfo {
	url: string;
	title: string;
	elements: { id: number; tag: string; text?: string }[];
}

// ─── Screenshot UI Component ───────────────────────────────────

class ScreenshotComponent {
	private description: string;
	private url: string;
	private title: string;
	private theme: Theme;
	private cachedLines?: string[];

	constructor(desc: string, url: string, title: string, theme: Theme) {
		this.description = desc;
		this.url = url;
		this.title = title;
		this.theme = theme;
	}

	handleInput(_data: string): void {}

	render(width: number): string[] {
		if (this.cachedLines) return this.cachedLines;

		const lines: string[] = [];
		const th = this.theme;

		lines.push("");
		const title = th.fg("accent", " Desktop ");
		lines.push(th.fg("borderMuted", "─".repeat(3)) + title + th.fg("borderMuted", "─".repeat(Math.max(0, width - 10))));
		lines.push("");

		if (this.title) lines.push(truncateToWidth(th.fg("muted", this.title), width));
		if (this.url) lines.push(truncateToWidth(th.fg("dim", this.url), width));
		lines.push("");

		if (this.description) {
			const descLines = this.description.split("\n");
			for (const line of descLines.slice(0, 10)) {
				lines.push(truncateToWidth(th.fg("toolOutput", line), width));
			}
			if (descLines.length > 10) {
				lines.push(truncateToWidth(th.fg("dim", "..."), width));
			}
		}

		lines.push("");
		this.cachedLines = lines;
		return lines;
	}

	invalidate(): void {
		this.cachedLines = undefined;
	}
}

// ─── Extension ─────────────────────────────────────────────────

export default function (pi: ExtensionAPI) {
	// ── Enable Desktop ──

	pi.registerTool({
		name: "computer_use_enable",
		label: "Enable Desktop",
		description: "Start the desktop environment with browser and VNC access. Call this first before using other computer_use tools.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			try {
				const raw = callApi(`/api/sandbox/sandbox-${ctx.cwd.split("/").pop()}/computer-use/enable`, "POST");
				const result: ComputerUseResult = JSON.parse(raw);
				return {
					content: [{ type: "text", text: `Desktop enabled. Sandbox: ${result.sandboxId}, CDP port: ${result.cdpPort}, VNC port: ${result.novncPort}` }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to enable desktop: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("success", "✓ ") + theme.fg("toolTitle", "desktop enabled"), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			const msg = text?.type === "text" ? text.text : "";
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", msg), 0, 0);
		},
	});

	// ── Screenshot ──

	pi.registerTool({
		name: "computer_use_screenshot",
		label: "Screenshot",
		description: "Take a screenshot of the current desktop state. Returns an AI description of what's visible plus the page URL and title.",
		parameters: Type.Object({
			describe: Type.Optional(Type.Boolean({ description: "Get AI description of the page. Default: true.", default: true })),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
			const describe = params.describe !== false;
			try {
				const raw = callApi(`/api/sandbox/${sandboxId}/computer-use/screenshot?describe=${describe}`);
				const result: ScreenshotResult = JSON.parse(raw);
				const lines: string[] = [];
				if (result.title) lines.push(`Title: ${result.title}`);
				if (result.url) lines.push(`URL: ${result.url}`);
				if (result.description) lines.push(``, result.description);
				return {
					content: [{ type: "text", text: lines.join("\n") }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Screenshot failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("screenshot")), 0, 0);
		},

		renderResult(result, _opts, theme, _ctx) {
			const details = result.details as ScreenshotResult | undefined;
			if (!details) {
				const text = result.content[0];
				return new Text(text?.type === "text" ? text.text : "", 0, 0);
			}
			return new Text(
				theme.fg("success", "✓ ") +
				theme.fg("muted", details.title || details.url || "Screenshot captured"),
				0,
				0,
			);
		},
	});

	// ── Snapshot (Page Elements) ──

	pi.registerTool({
		name: "computer_use_snapshot",
		label: "Snapshot",
		description: "Get a structured list of all clickable/interactive elements on the current page. Returns element IDs, tags, and text content.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
			try {
				const raw = callApi(`/api/sandbox/${sandboxId}/computer-use/snapshot`);
				const result: PageInfo = JSON.parse(raw);
				const lines: string[] = [`URL: ${result.url}`, `Title: ${result.title}`, ``, `Elements (${result.elements.length}):`];
				const display = result.elements.slice(0, 30);
				for (const el of display) {
					lines.push(`  [${el.id}] <${el.tag}>${el.text ? ` "${el.text}"` : ""}`);
				}
				if (result.elements.length > 30) {
					lines.push(`  ... and ${result.elements.length - 30} more`);
				}
				return {
					content: [{ type: "text", text: lines.join("\n") }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Snapshot failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("snapshot")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const details = result.details as PageInfo | undefined;
			if (!details) {
				const text = result.content[0];
				return new Text(text?.type === "text" ? text.text : "", 0, 0);
			}
			return new Text(
				theme.fg("success", "✓ ") +
				theme.fg("muted", `${details.elements.length} elements on ${details.url}`),
				0,
				0,
			);
		},
	});

	// ── Click ──

	pi.registerTool({
		name: "computer_use_click",
		label: "Click",
		description: "Click an element by its ID (from snapshot).",
		parameters: Type.Object({
			element: Type.Number({ description: "Element ID from snapshot" }),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
			try {
				const raw = callApi(`/api/sandbox/${sandboxId}/computer-use/act`, "POST", {
					action: "click",
					element: params.element,
				});
				const result: PageInfo = JSON.parse(raw);
				return {
					content: [{ type: "text", text: `Clicked element ${params.element}. Now at: ${result.url} (${result.title})` }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Click failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(args, theme) {
			return new Text(
				theme.fg("toolTitle", theme.bold("click ")) +
				theme.fg("accent", `#${args.element}`),
				0,
				0,
			);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Type ──

	pi.registerTool({
		name: "computer_use_type",
		label: "Type",
		description: "Type text into an element by its ID (from snapshot).",
		parameters: Type.Object({
			element: Type.Number({ description: "Element ID from snapshot" }),
			text: Type.String({ description: "Text to type" }),
			submit: Type.Optional(Type.Boolean({ description: "Press Enter after typing. Default: false.", default: false })),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
			try {
				const raw = callApi(`/api/sandbox/${sandboxId}/computer-use/act`, "POST", {
					action: "type",
					element: params.element,
					text: params.text,
					submit: params.submit || false,
				});
				const result: PageInfo = JSON.parse(raw);
				return {
					content: [{ type: "text", text: `Typed into element ${params.element}. Now at: ${result.url} (${result.title})` }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Type failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(args, theme) {
			return new Text(
				theme.fg("toolTitle", theme.bold("type ")) +
				theme.fg("accent", `#${args.element}`) +
				theme.fg("dim", ` "${args.text.slice(0, 30)}${args.text.length > 30 ? "..." : ""}"`),
				0,
				0,
			);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Navigate ──

	pi.registerTool({
		name: "computer_use_navigate",
		label: "Navigate",
		description: "Navigate the browser to a URL.",
		parameters: Type.Object({
			url: Type.String({ description: "URL to navigate to" }),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
			try {
				const raw = callApi(`/api/sandbox/${sandboxId}/computer-use/act`, "POST", {
					action: "navigate",
					url: params.url,
				});
				const result: PageInfo = JSON.parse(raw);
				return {
					content: [{ type: "text", text: `Navigated to ${params.url}. Now at: ${result.url} (${result.title})` }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Navigate failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(args, theme) {
			return new Text(
				theme.fg("toolTitle", theme.bold("navigate ")) +
				theme.fg("accent", args.url),
				0,
				0,
			);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Scroll ──

	pi.registerTool({
		name: "computer_use_scroll",
		label: "Scroll",
		description: "Scroll the page up or down.",
		parameters: Type.Object({
			direction: StringEnum(["up", "down"] as const, { description: "Direction to scroll" }),
			amount: Type.Optional(Type.Number({ description: "Pixels to scroll. Default: 300.", default: 300 })),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
			try {
				const raw = callApi(`/api/sandbox/${sandboxId}/computer-use/act`, "POST", {
					action: "scroll",
					direction: params.direction,
					amount: params.amount || 300,
				});
				const result: PageInfo = JSON.parse(raw);
				return {
					content: [{ type: "text", text: `Scrolled ${params.direction} ${params.amount || 300}px. Now at: ${result.url} (${result.title})` }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Scroll failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(args, theme) {
			return new Text(
				theme.fg("toolTitle", theme.bold("scroll ")) +
				theme.fg("accent", args.direction) +
				theme.fg("dim", ` ${args.amount || 300}px`),
				0,
				0,
			);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Disable Desktop ──

	pi.registerTool({
		name: "computer_use_disable",
		label: "Disable Desktop",
		description: "Stop the desktop environment and free resources.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
			try {
				callApi(`/api/sandbox/${sandboxId}/computer-use/disable`, "POST");
				return {
					content: [{ type: "text", text: "Desktop disabled." }],
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to disable desktop: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("warning", "⊘ ") + theme.fg("toolTitle", "desktop disabled"), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("warning", "⊘ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Execute in Sandbox ──

	pi.registerTool({
		name: "computer_use_exec",
		label: "Execute in Sandbox",
		description: "Execute a bash command inside the sandbox container. Use this for apt install, file operations, etc. inside the sandbox environment.",
		parameters: Type.Object({
			command: Type.String({ description: "The bash command to execute inside the sandbox container" }),
			timeout: Type.Optional(Type.Number({ description: "Timeout in milliseconds (default 30000)" })),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			try {
				const sandboxId = `sandbox-${ctx.cwd.split("/").pop()}`;
				const raw = callApi(`/api/sandbox/${sandboxId}/exec`, "POST", {
					cmd: params.command.split(" "),
					timeout: params.timeout || 30000,
				});
				const result: { output: string; exitCode: number } = JSON.parse(raw);
				const display = result.exitCode === 0 ? result.output : `Exit code: ${result.exitCode}\n${result.output}`;
				return {
					content: [{ type: "text", text: display.slice(0, 2000) }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Failed to execute command: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(args, theme) {
			return new Text(theme.fg("muted", "$ ") + theme.fg("toolArgs", String(args.command || "").slice(0, 60)), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			const msg = text?.type === "text" ? text.text : "";
			return new Text(theme.fg("muted", msg.slice(0, 100)), 0, 0);
		},
	});
}
