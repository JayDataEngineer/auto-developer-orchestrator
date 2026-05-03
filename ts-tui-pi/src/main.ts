#!/usr/bin/env bun
// pux-tui — pi-mono TUI powered by pux Go backend

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
    project: { type: "string", default: "default" },
    model: { type: "string", default: "qwen" },
    cwd: { type: "string", default: process.cwd() },
  },
});

const cwd = opts.cwd!;
const agentDir = join(homedir(), ".pux");

// Create SettingsManager (reads settings from ~/.pux/settings.json)
const settingsManager = SettingsManager.create(cwd, agentDir);

// Create SessionManager (in-memory for now, no persistence between runs)
const sessionManager = SessionManager.inMemory(cwd);

// Fetch available models from Go backend to discover active model
let activeModelId = opts.model!;
let activeModel: any = null;
try {
  const resp = await fetch(`${opts.server}/api/pux/models`);
  if (resp.ok) {
    const models: Array<{ id: string; name: string; provider: string }> = await resp.json();
    if (models.length > 0) {
      const matched = models.find((m) => m.id.includes(opts.model!)) ?? models[0];
      activeModelId = matched.id;
      activeModel = { id: matched.id, name: matched.name, provider: matched.provider, reasoning: false };
    }
  }
} catch {
  // backend not reachable yet, keep default
}

// Create our PuxAgentSession — bridges to Go SSE backend
const session = new PuxAgentSession(
  settingsManager,
  sessionManager,
  opts.server!,
  opts.project!,
  activeModelId,
);

// Set discovered model on session state (footer reads state.model)
if (activeModel) {
  session.state.model = activeModel;
}

// Create AgentSessionServices (minimal duck-typed object)
const services = {
  cwd,
  agentDir,
  settingsManager,
  sessionManager,
  modelRegistry: null as any,
  resourceLoader: {
    getExtensions: () => ({ extensions: [], diagnostics: [] }),
    getSkills: () => ({ skills: [] }),
    getPrompts: () => ({ prompts: [] }),
    getThemes: () => ({ themes: [] }),
    reload: async () => {},
  } as any,
  authStorage: null as any,
  extensionFlags: new Map(),
};

// Create a no-op CreateAgentSessionRuntimeFactory
const createRuntime: any = async () => ({
  session,
  services,
  extensionsResult: { extensions: [], diagnostics: [] },
  diagnostics: [],
});

// Create AgentSessionRuntime (wrapper expected by InteractiveMode)
const runtime = new AgentSessionRuntime(session, services as any, createRuntime, []);

// Create InteractiveMode (constructor calls initTheme internally)
const interactiveMode = new InteractiveMode(runtime, {
  verbose: false,
});

// Initialize and start
await interactiveMode.init();
