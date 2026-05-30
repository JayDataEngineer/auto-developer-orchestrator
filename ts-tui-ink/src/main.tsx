#!/usr/bin/env bun
/**
 * Pux TUI — terminal interface powered by Ink + @assistant-ui/react-ink.
 *
 * Shares the same PuxChatAdapter and Zustand store as the web UI.
 */

import { render } from "ink";
import { parseArgs } from "node:util";
import { homedir } from "node:os";
import { join, basename } from "node:path";
import { setBaseUrl, setFetch, usePuxStore } from "@pux/shared";
import React from "react";
import { App } from "./app.js";
import { initMouseTracking, disableMouseTracking, filterAndEmitMouseEvents } from "./mouse.js";

// ── Stdin fixes (runs before Ink processes any input) ──
//
// Kitty keyboard protocol is now handled by Ink's kittyKeyboard render option.
// Only two fixes remain that Ink doesn't handle:
//
// 1. Linux backspace: Ink maps \x7f (DEL) to key.delete but Linux terminals
//    send \x7f for backspace. Rewrite → \b so Ink maps it to key.backspace.
//
// 2. Ctrl+J newline: Ink's ComposerInput inserts \n as a regular character
//    (it falls through to the catch-all insert path). Rewrite \n → \r so
//    Ctrl+J acts like Enter (submit) instead of inserting a newline.

// ── Stdout passthrough ──
// No buffering or interception. Ink writes directly to stdout.
// Flicker was caused by console.log writes while Ink was running,
// not by stdout buffering. Keeping stdout clean is the fix.

const origRead = process.stdin.read.bind(process.stdin);
process.stdin.read = function (size?: number) {
	const chunk = origRead(size);
	if (typeof chunk === "string") {
		let fixed = chunk;
		// Filter SGR mouse sequences before Ink processes them
		fixed = filterAndEmitMouseEvents(fixed);
		// Linux backspace: \x7f → \b (DEL → BS)
		if (process.platform !== "darwin" && fixed.includes("\x7f")) {
			fixed = fixed.replaceAll("\x7f", "\b");
		}
		// Ctrl+J: \n → \r (prevents newline insertion, acts as Enter)
		if (fixed.includes("\n")) {
			fixed = fixed.replaceAll("\n", "\r");
		}
		return fixed;
	}
	return chunk;
} as typeof process.stdin.read;

function restoreTerminal() {
	disableMouseTracking();
	// Restore font size
	const scale = usePuxStore.getState().fontScale;
	if (scale !== 1) applyFontScale(1);
	// No alt-screen buffer — keeps normal terminal scrollback so users can
	// scroll up to see old messages (graduated to Ink's <Static>).
}
process.on("exit", restoreTerminal);
process.on("SIGINT", () => { restoreTerminal(); process.exit(0); });
process.on("SIGTERM", () => { restoreTerminal(); process.exit(0); });

// ── CLI Args ──

const { values: opts } = parseArgs({
	options: {
		server: { type: "string", default: "http://localhost:3847" },
		project: { type: "string", default: "auto-developer-orchestrator" },
		model: { type: "string", default: "" },
		cwd: { type: "string", default: process.cwd() },
		org: { type: "string" },
	},
	strict: false,
});

// ── Org resolution ──

if (opts.org && typeof opts.org === "string") {
	const fs = await import("node:fs");
	const path = await import("node:path");
	const orgAliases: Record<string, string> = { code: "dev-bot", dev: "dev-bot" };
	const orgName = orgAliases[opts.org] || opts.org;
	const candidates = [
		path.join(homedir(), "Documents", "programs", "dev", orgName),
		path.join(homedir(), "Documents", "programs", "dev", orgName + "-bot"),
		path.join(process.cwd(), orgName),
		path.join(process.cwd(), "..", orgName),
	];
	let found = false;
	for (const dir of candidates) {
		try {
			fs.statSync(path.join(dir, "pux.yaml"));
			opts.cwd = dir;
			opts.project = path.basename(dir);
			found = true;
			break;
		} catch {}
	}
	if (!found) {
		process.stderr.write(
			`\x1b[31mOrganization '${opts.org}' not found.\x1b[0m\n`
		);
		process.exit(1);
	}
}

// ── Environment setup ──

const serverUrl = opts.server as string;
setBaseUrl(serverUrl);
setFetch(globalThis.fetch);

// ── Health check ──

let backendOnline = false;
try {
	const healthResp = await fetch(`${serverUrl}/api/health`, {
		signal: AbortSignal.timeout(3000),
	});
	if (healthResp.ok) backendOnline = true;
} catch {}

if (!backendOnline) {
	process.stderr.write(
		`\x1b[33m⚠  Backend not reachable at ${serverUrl}\x1b[0m\n` +
		`\x1b[90m   Start it with: task dev   (or cd go-backend && go run ./cmd/server/)\x1b[0m\n` +
		`\x1b[90m   The TUI will work but prompts will fail until backend starts.\x1b[0m\n\n`
	);
}

// Register org project with backend
if (opts.org && typeof opts.org === "string" && backendOnline) {
	try {
		await fetch(`${serverUrl}/api/projects`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				name: opts.project,
				path: opts.cwd,
			}),
		});
	} catch {}
}

// Set active project and model in store
const projectName = opts.project as string;
const modelName = opts.model as string;
const cwdName = opts.cwd as string;

usePuxStore.getState().setModel(modelName);
usePuxStore.getState().setProject(projectName);
await usePuxStore.getState().loadProjects();
await usePuxStore.getState().loadConversations();

// Load models and defaults from backend, resolve actual model if not specified via CLI
if (backendOnline) {
	await usePuxStore.getState().loadModels();
	await usePuxStore.getState().loadDefaults();
	const store = usePuxStore.getState();
	if (!modelName) {
		// Use logic default, then first available model, then leave empty
		const resolved = store.defaultLogic || store.modelList[0]?.id || "";
		if (resolved) {
			usePuxStore.getState().setModel(resolved);
		}
	}
}

// ── Mouse tracking ──

initMouseTracking();

// ── Font scale ──
// Applies saved font scale via Kitty OSC 50 or terminal-specific sequences.
// Re-applied whenever the store value changes.

const BASE_FONT_SIZE = 14; // assumed default terminal font size
function applyFontScale(scale: number) {
	const term = process.env.TERM_PROGRAM ?? process.env.TERM ?? "";
	const size = Math.round(BASE_FONT_SIZE * scale);
	if (term === "kitty" || term === "xterm-kitty") {
		// Kitty: OSC 50 sets font size
		process.stdout.write(`\x1b]50;FontSize=${size}\x07`);
	} else if (term === "WezTerm") {
		// WezTerm: OSC 50 with font-size
		process.stdout.write(`\x1b]50;font-size=${size}\x07`);
	}
	// Other terminals: font size change not supported via escape sequences
}

// Apply on startup
const initialScale = usePuxStore.getState().fontScale;
if (initialScale !== 1) applyFontScale(initialScale);

// Re-apply on change
usePuxStore.subscribe((state, prev) => {
	if (state.fontScale !== prev.fontScale) applyFontScale(state.fontScale);
});

// ── Render ──

/** Check if the terminal supports the Kitty keyboard protocol. */
function supportsKittyKeyboard(): boolean {
	const term = process.env.TERM_PROGRAM ?? process.env.TERM ?? "";
	return term === "kitty" || term === "xterm-kitty" || term === "WezTerm" ||
		(process.env.KITTY_WINDOW_ID !== undefined);
}

const appElement = React.createElement(App, {
	model: modelName,
	project: projectName,
	cwd: cwdName,
});

// ── Silence ALL console output while Ink runs ──
// Any console.log/warn/error while Ink controls the terminal causes a flash.
// Redirect to /dev/null. Debug writes go to /tmp/pux-run-debug.log instead.
console.log = () => {};
console.warn = () => {};
console.error = () => {};
process.on("unhandledRejection", () => {});
process.on("uncaughtException", () => {});

// No alternate screen buffer — we use BSU/ESU write buffering +
// incrementalRendering to prevent flicker instead. The normal terminal
// buffer has scrollback, so users can scroll up to see old messages
// that graduated to Ink's <Static> component.

const instance = render(appElement, {
	exitOnCtrlC: false,
	kittyKeyboard: { mode: "disabled" },
});

// Force Ink to clear and re-render on terminal resize.
// Without this, scrollback from the old width stays garbled.
process.stdout.on("resize", () => {
	instance.clear();
	instance.rerender(appElement);
});

await instance.waitUntilExit();
restoreTerminal();
process.exit(0);
