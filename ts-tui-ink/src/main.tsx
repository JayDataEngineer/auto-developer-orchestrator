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

// ── Stdin fixes (runs before Ink processes any input) ──
//
// 1. Linux backspace: Ink maps \x7f (DEL) to key.delete but Linux terminals
//    send \x7f for backspace. Rewrite → \b so Ink maps it to key.backspace.
//
// 2. Ctrl+J newline: Ink's ComposerInput inserts \n as a regular character
//    (it falls through to the catch-all insert path). Rewrite \n → \r so
//    Ctrl+J acts like Enter (submit) instead of inserting a newline.
//
// Both fixes patch stdin.read() since Ink reads via that method in utf8
// string mode (setEncoding('utf8') in setRawMode).

const origRead = process.stdin.read.bind(process.stdin);
process.stdin.read = function (size?: number) {
	const chunk = origRead(size);
	if (typeof chunk === "string") {
		let fixed = chunk;
		if (process.platform !== "darwin" && fixed.includes("\x7f")) {
			fixed = fixed.replaceAll("\x7f", "\b");
		}
		if (fixed.includes("\n")) {
			fixed = fixed.replaceAll("\n", "\r");
		}
		return fixed;
	}
	return chunk;
} as typeof process.stdin.read;

// ── Kitty keyboard protocol ──
// Enables enhanced key reporting so Shift+Enter sends CSI 13;2 u
// (with modifier flags) instead of just CR. Also enables modifyOtherKeys
// for xterm/tmux. Restore both on exit.
// Whitelist from Claude Code — only terminals known to support extended keys.

const ENABLE_KITTY_KEYBOARD = "\x1b[>1u";
const DISABLE_KITTY_KEYBOARD = "\x1b[<u";
const ENABLE_MODIFY_OTHER_KEYS = "\x1b[>4;2m";
const DISABLE_MODIFY_OTHER_KEYS = "\x1b[>4m";

const EXTENDED_KEYS_TERMINALS = [
	"iTerm.app", "kitty", "WezTerm", "ghostty", "tmux", "windows-terminal",
	"xterm-kitty", "xterm-ghostty",
];

function supportsExtendedKeys(): boolean {
	const term = process.env.TERM_PROGRAM ?? process.env.TERM ?? "";
	return EXTENDED_KEYS_TERMINALS.some((t) => term.includes(t));
}

const hasExtendedKeys = supportsExtendedKeys();
if (hasExtendedKeys) {
	process.stdout.write(ENABLE_KITTY_KEYBOARD + ENABLE_MODIFY_OTHER_KEYS);
}

function restoreTerminal() {
	if (hasExtendedKeys) {
		const { writeSync } = require("node:fs") as { writeSync: (fd: number, data: string) => void };
		writeSync(1, DISABLE_MODIFY_OTHER_KEYS + DISABLE_KITTY_KEYBOARD);
	}
}
process.on("exit", restoreTerminal);
process.on("SIGINT", () => { restoreTerminal(); process.exit(0); });
process.on("SIGTERM", () => { restoreTerminal(); process.exit(0); });

// ── CLI Args ──

const { values: opts } = parseArgs({
	options: {
		server: { type: "string", default: "http://localhost:3847" },
		project: { type: "string", default: "auto-developer-orchestrator" },
		model: { type: "string", default: "deepseek/deepseek-v4-flash" },
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

// Set active project in store
const projectName = opts.project as string;
const modelName = opts.model as string;
const cwdName = opts.cwd as string;

usePuxStore.getState().setProject(projectName);
await usePuxStore.getState().loadProjects();
await usePuxStore.getState().loadConversations();

// ── Render ──

const { waitUntilExit } = render(
	React.createElement(App, {
		model: modelName,
		project: projectName,
		cwd: cwdName,
	})
);

await waitUntilExit();
