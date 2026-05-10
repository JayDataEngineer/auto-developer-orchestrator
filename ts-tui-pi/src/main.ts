#!/usr/bin/env bun
// pux-tui — minimal entry point for testing SSE bridge in full TUI

import { homedir } from "node:os";
import { join } from "node:path";
import { InteractiveMode } from "./modes/interactive/interactive-mode.js";
import { AgentSessionRuntime } from "./core/agent-session-runtime.js";
import { SessionManager } from "./core/session-manager.js";
import { SettingsManager } from "./core/settings-manager.js";
import { PuxAgentSession } from "./core/pux-agent-session.js";
import { parseArgs } from "node:util";

const { values: opts } = parseArgs({
  options: {
    server: { type: "string", default: "http://localhost:3847" },
    project: { type: "string", default: "ts-tui-pi" },
    model: { type: "string", default: "deepseek/deepseek-v4-flash" },
    cwd: { type: "string", default: process.cwd() },
    org: { type: "string" },
  },
});

// Resolve --org flag: find pux.yaml in ~/Documents/programs/dev/<org>/
if (opts.org) {
  const homeDir = homedir();
  const fs = await import("node:fs");
  const path = await import("node:path");
  // Aliases: --org code → dev-bot, etc.
  const orgAliases: Record<string, string> = { code: "dev-bot", dev: "dev-bot" };
  const orgName = orgAliases[opts.org] || opts.org;
  const candidates = [
    path.join(homeDir, "Documents", "programs", "dev", orgName),
    path.join(homeDir, "Documents", "programs", "dev", orgName + "-bot"),
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
    process.stderr.write(`\x1b[31mOrganization '${opts.org}' not found. Looked for pux.yaml in:\n${candidates.map(d => "  " + d).join("\n")}\x1b[0m\n`);
    process.exit(1);
  }
}

const cwd = opts.cwd!;
const agentDir = join(homedir(), ".pux");

// ── Startup health check ──
let backendOnline = false;
try {
  const healthResp = await fetch(`${opts.server}/api/health`, { signal: AbortSignal.timeout(3000) });
  if (healthResp.ok) backendOnline = true;
} catch {}

if (!backendOnline) {
  process.stderr.write(
    `\x1b[33m⚠  Backend not reachable at ${opts.server}\x1b[0m\n` +
    `\x1b[90m   Start it with: task dev   (or cd go-backend && go run ./cmd/server/)\x1b[0m\n` +
    `\x1b[90m   The TUI will work but prompts will fail until backend starts.\x1b[0m\n\n`
  );
}

// Register org project with backend so it can resolve the path
if (opts.org && backendOnline) {
  try {
    await fetch(`${opts.server}/api/projects`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: opts.project, path: opts.cwd }),
    });
  } catch {}
}

const settingsManager = SettingsManager.create(cwd, agentDir);
// Resume most recent session for this project, or create new one.
// Sessions persist to ~/.pi/agent/sessions/<encoded-cwd>/*.jsonl
const sessionManager = SessionManager.continueRecent(cwd);

// Fetch models for footer display only
let modelMeta: any = null;
try {
  const resp = await fetch(`${opts.server}/api/pux/models`);
  if (resp.ok) {
    const models: any[] = await resp.json();
    if (models.length > 0) {
      const m = models.find((x: any) => x.id.includes(opts.model!)) ?? models[0];
      modelMeta = {
        id: m.id.split("/").pop() || m.id,
        name: m.name,
        provider: m.provider,
        reasoning: false,
        contextWindow: m.contextWindow || 0,
      };
    }
  }
} catch {}

const session = new PuxAgentSession(settingsManager, sessionManager, opts.server!, opts.project!, opts.model!);
if (modelMeta) {
  session.model = modelMeta;
  session.state.model = modelMeta;
  session.agent.model = modelMeta;
}

const services = {
  cwd, agentDir, settingsManager, sessionManager,
  modelRegistry: null as any,
  resourceLoader: {
    getExtensions: () => ({ extensions: [], diagnostics: [] }),
    getSkills: () => ({ skills: [] }),
    getPrompts: () => ({ prompts: [] }),
    getThemes: () => ({ themes: [], diagnostics: [] }),
    reload: async () => {},
  } as any,
  authStorage: null as any,
  extensionFlags: new Map(),
};

const createRuntime: any = async () => ({
  session, services,
  extensionsResult: { extensions: [], diagnostics: [] },
  diagnostics: [],
});

const runtime = new AgentSessionRuntime(session, services as any, createRuntime, []);
const interactiveMode = new InteractiveMode(runtime, { verbose: false });

// Safety net: double SIGINT forces exit even if raw mode swallows Ctrl+C
let sigintCount = 0;
let sigintTimer: ReturnType<typeof setTimeout> | undefined;
process.on("SIGINT", () => {
  sigintCount++;
  if (sigintCount >= 2) {
    process.exit(130);
  }
  clearTimeout(sigintTimer);
  sigintTimer = setTimeout(() => { sigintCount = 0; }, 1000);
});

await interactiveMode.run();
