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
