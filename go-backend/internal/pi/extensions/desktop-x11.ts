/**
 * Desktop X11 Extension
 *
 * Registers native Pi tools for X11 desktop automation (xdotool):
 *   - desktop_screenshot: Full desktop screenshot (not just Chrome)
 *   - desktop_click: Click at absolute X,Y coordinates
 *   - desktop_type: Type text into the focused window
 *   - desktop_key: Press a special key (Return, ctrl+a, alt+F4, etc.)
 *   - desktop_resolution: Get the display resolution
 *   - desktop_active_window: Get the focused window name
 *
 * These tools work with ANY X11 application, not just Chrome.
 * They complement the computer_use_* tools which only control the browser via CDP.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Text } from "@mariozechner/pi-tui";
import { Type } from "@sinclair/typebox";

// ─── API Helper ────────────────────────────────────────────────

const API_HOST = process.env.ORCHESTRATOR_API_HOST || "localhost:3847";

function callApi(endpoint: string, method = "GET", body?: Record<string, unknown>): string {
	const args: string[] = ["-s"];
	if (method !== "GET") args.push("-X", method);
	if (body) args.push("-d", JSON.stringify(body));
	args.push(`http://${API_HOST}${endpoint}`);
	const result = require("child_process").spawnSync("curl", args, { encoding: "utf-8", timeout: 30000 });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(result.stderr);
	return result.stdout;
}

function sandboxId(ctx: { cwd: string }): string {
	return `sandbox-${ctx.cwd.split("/").pop()}`;
}

// ─── Extension ─────────────────────────────────────────────────

export default function (pi: ExtensionAPI) {
	// ── Desktop Screenshot ──

	pi.registerTool({
		name: "desktop_screenshot",
		label: "Desktop Screenshot",
		description: "Take a full desktop screenshot via X11 (captures ALL windows, not just the browser). Returns base64 PNG.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			try {
				const raw = callApi(`/api/sandbox/${sandboxId(ctx)}/x11/screenshot`);
				const result = JSON.parse(raw);
				return {
					content: [{ type: "text", text: "Full desktop screenshot captured." }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Desktop screenshot failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("desktop screenshot")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			const msg = text?.type === "text" ? text.text : "";
			const isError = result.isError;
			return new Text(
				isError ? theme.fg("error", "✗ ") + theme.fg("muted", msg) : theme.fg("success", "✓ ") + theme.fg("muted", msg),
				0, 0,
			);
		},
	});

	// ── Desktop Click ──

	pi.registerTool({
		name: "desktop_click",
		label: "Desktop Click",
		description: "Click at absolute screen coordinates using X11 (xdotool). Works with any desktop application.",
		parameters: Type.Object({
			x: Type.Number({ description: "X coordinate (pixels from left)" }),
			y: Type.Number({ description: "Y coordinate (pixels from top)" }),
			button: Type.Optional(Type.Number({ description: "Mouse button: 1=left (default), 2=middle, 3=right", default: 1 })),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			try {
				callApi(`/api/sandbox/${sandboxId(ctx)}/x11/mouse`, "POST", {
					action: "click",
					x: params.x,
					y: params.y,
					button: params.button || 1,
				});
				return {
					content: [{ type: "text", text: `Clicked at (${params.x}, ${params.y}) with button ${params.button || 1}` }],
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
				theme.fg("toolTitle", theme.bold("desktop click ")) +
				theme.fg("accent", `(${args.x}, ${args.y})`),
				0, 0,
			);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Desktop Type ──

	pi.registerTool({
		name: "desktop_type",
		label: "Desktop Type",
		description: "Type text into the currently focused window using X11 (xdotool). Works with any desktop application.",
		parameters: Type.Object({
			text: Type.String({ description: "Text to type" }),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			try {
				callApi(`/api/sandbox/${sandboxId(ctx)}/x11/keyboard`, "POST", {
					action: "type",
					text: params.text,
				});
				return {
					content: [{ type: "text", text: `Typed: "${params.text.slice(0, 50)}${params.text.length > 50 ? "..." : ""}"` }],
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
				theme.fg("toolTitle", theme.bold("desktop type ")) +
				theme.fg("dim", `"${String(args.text || "").slice(0, 30)}${String(args.text || "").length > 30 ? "..." : ""}"`),
				0, 0,
			);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Desktop Key ──

	pi.registerTool({
		name: "desktop_key",
		label: "Desktop Key",
		description: "Press a special key or key combination using X11 (xdotool). Examples: 'Return', 'ctrl+a', 'alt+F4', 'ctrl+c', 'Tab'.",
		parameters: Type.Object({
			key: Type.String({ description: "Key or key combo (e.g. 'Return', 'ctrl+a', 'alt+F4')" }),
		}),

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			try {
				callApi(`/api/sandbox/${sandboxId(ctx)}/x11/keyboard`, "POST", {
					action: "key",
					key: params.key,
				});
				return {
					content: [{ type: "text", text: `Pressed key: ${params.key}` }],
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Key press failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(args, theme) {
			return new Text(
				theme.fg("toolTitle", theme.bold("desktop key ")) +
				theme.fg("accent", args.key),
				0, 0,
			);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Desktop Resolution ──

	pi.registerTool({
		name: "desktop_resolution",
		label: "Desktop Resolution",
		description: "Get the X11 display resolution. Use this to calibrate coordinate-based clicks.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			try {
				const raw = callApi(`/api/sandbox/${sandboxId(ctx)}/x11/resolution`);
				const result = JSON.parse(raw);
				return {
					content: [{ type: "text", text: `Display resolution: ${result.width}x${result.height}` }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Resolution check failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("desktop resolution")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});

	// ── Desktop Active Window ──

	pi.registerTool({
		name: "desktop_active_window",
		label: "Desktop Active Window",
		description: "Get the currently focused X11 window name and ID. Useful for verifying which app has focus.",
		parameters: Type.Object({}),

		async execute(_toolCallId, _params, _signal, _onUpdate, ctx) {
			try {
				const raw = callApi(`/api/sandbox/${sandboxId(ctx)}/x11/active-window`);
				const result = JSON.parse(raw);
				return {
					content: [{ type: "text", text: `Active window: ${result.windowName} (ID: ${result.windowId})` }],
					details: result,
				};
			} catch (e: any) {
				return {
					content: [{ type: "text", text: `Active window check failed: ${e.message}` }],
					isError: true,
				};
			}
		},

		renderCall(_args, theme) {
			return new Text(theme.fg("toolTitle", theme.bold("desktop active window")), 0, 0);
		},

		renderResult(result, _opts, theme) {
			const text = result.content[0];
			return new Text(theme.fg("success", "✓ ") + theme.fg("muted", text?.type === "text" ? text.text : ""), 0, 0);
		},
	});
}
