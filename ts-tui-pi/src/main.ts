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

// Fetch available models from Go backend
let activeModelId = opts.model!;
let activeModel: any = null;
let backendModels: Array<{ id: string; name: string; provider: string }> = [];
try {
  const resp = await fetch(`${opts.server}/api/pux/models`);
  if (resp.ok) {
    backendModels = await resp.json();
    if (backendModels.length > 0) {
      const matched = backendModels.find((m) => m.id.includes(opts.model!)) ?? backendModels[0];
      activeModelId = matched.id;
      activeModel = {
        id: matched.id.split("/").pop() || matched.id,  // "deepseek/deepseek-v4-flash" → "deepseek-v4-flash"
        name: matched.name,
        provider: matched.provider,
        api: matched.provider,
        reasoning: false,
        input: ["text"],
        contextWindow: 128000,
        maxTokens: 16384,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        baseUrl: "",
      };
    }
  }
} catch {
  // backend not reachable yet, keep default
}

// Build pi-ai compatible model list
const piModels = backendModels.map((m) => ({
  id: m.id.split("/").pop() || m.id,
  name: m.name,
  provider: m.provider,
  api: m.provider,
  reasoning: false,
  input: ["text"],
  contextWindow: 128000,
  maxTokens: 16384,
  cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
  baseUrl: "",
}));

// Create our PuxAgentSession — bridges to Go SSE backend
const session = new PuxAgentSession(
  settingsManager,
  sessionManager,
  opts.server!,
  opts.project!,
  activeModelId,
);

// Populate models: scopedModels for the UI, and _availableModels for the model selector
if (piModels.length > 0) {
  session.scopedModels = piModels.map((m: any) => ({ model: m }));
  // Also push into the internal _availableModels array (referenced by modelRegistry)
  // We access via the duck-typed object
  const registry = session.modelRegistry as any;
  // Override the available models getter
  const modelsList = [...piModels];
  registry.getAvailable = () => modelsList;
  registry.getAll = () => modelsList;
  registry.find = (provider: string, id: string) =>
    modelsList.find((m: any) => m.provider === provider && m.id === id);
  registry.get = (provider: string, id: string) =>
    modelsList.find((m: any) => m.provider === provider && m.id === id);

  // Set the active model
  if (activeModel) {
    session.model = activeModel;
    session.state.model = activeModel;
    session.agent.model = activeModel;
  }
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
