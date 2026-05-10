// PuxAgentSession — bridges Go SSE backend to pi-mono InteractiveMode AgentSessionEvent types.
// Satisfies the interface InteractiveMode expects (duck-typed, no TypeScript enforcement at runtime).

import type { AgentSessionEvent, AgentSessionEventListener } from "./agent-session.js";
import type { ThinkingLevel } from "../agent-core/types.js";
import type {
  AssistantMessage, AssistantMessageEvent,
  TextContent, ThinkingContent, ToolCall,
} from "../ai/types.js";
import type { SettingsManager } from "./settings-manager.js";

// ---------------------------------------------------------------------------
// Minimal model stub — InteractiveMode reads model.id, model.provider, etc.
// ---------------------------------------------------------------------------
interface PuxModel {
  id: string;
  name: string;
  api: string;
  provider: string;
}

const DEFAULT_MODEL: PuxModel = {
  id: "deepseek/deepseek-v4-flash",
  name: "DeepSeek V4 Flash",
  api: "openrouter",
  provider: "openrouter",
};

// Build a pi-ai compatible Model shape from backend data
function toPiModel(m: { id: string; name: string; provider: string }, contextWindow?: number): PuxModel {
  const [api, ...rest] = m.id.split("/");
  return {
    id: rest.join("/") || m.id,
    name: m.name,
    api: m.provider,
    provider: m.provider,
    // pi-ai Model also expects these:
    reasoning: false,
    input: ["text" as const] as ("text" | "image")[],
    contextWindow: contextWindow || 0,
    maxTokens: 16384,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    baseUrl: "",
  } as any;
}

// ---------------------------------------------------------------------------
// Helper: emit a well-formed AssistantMessage (needed by message_start/update/end)
// ---------------------------------------------------------------------------
function mkAssistant(overrides: Partial<AssistantMessage> = {}): AssistantMessage {
  return {
    role: "assistant",
    content: [],
    api: "pux",
    provider: "pux",
    model: "pux",
    usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0 },
    stopReason: "stop",
    timestamp: Date.now(),
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Helper: build a user AgentMessage (for message_start with role "user")
// ---------------------------------------------------------------------------
function userMessage(text: string): any {
  return {
    role: "user",
    content: [{ type: "text", text }],
    timestamp: Date.now(),
  };
}

// ---------------------------------------------------------------------------
// SSE → pi-mono AgentSessionEvent mapper
// ---------------------------------------------------------------------------

export class PuxAgentSession {
  public settingsManager: SettingsManager;
  public sessionManager: any;
  public agent: any;
  public model: any = DEFAULT_MODEL;
  public thinkingLevel: ThinkingLevel = "none";
  public scopedModels: Array<{ model: any; thinkingLevel?: ThinkingLevel }> = [];
  private _availableModels: any[] = [];
  public resourceLoader: any = {
    getThemes: () => ({ themes: [], diagnostics: [] }),
    getExtensions: () => ({ extensions: [], errors: [], diagnostics: [] }),
    getSkills: () => ({ skills: [], diagnostics: [] }),
    getPrompts: () => ({ prompts: [], diagnostics: [] }),
    getAgentsFiles: () => ({ agentsFiles: [], diagnostics: [] }),
    getSystemPrompt: () => ({ systemPrompt: "", agentsFiles: [], diagnostics: [] }),
    getAppendSystemPrompt: () => ({ appendSystemPrompt: "", agentsFiles: [], diagnostics: [] }),
    extendResources: () => [],
    reload: async () => {},
  };
  public modelRegistry: any = {
    get: (provider: string, id: string) => this._availableModels.find((m: any) => m.provider === provider && m.id === id),
    find: (provider: string, id: string) => this._availableModels.find((m: any) => m.provider === provider && m.id === id),
    getAll: () => this._availableModels,
    getAvailable: () => this._availableModels,
    getError: () => null,
    getApiKeyAndHeaders: () => ({ apiKey: "", headers: {} }),
    hasConfiguredAuth: () => false,
    isUsingOAuth: () => false,
    refresh: () => {},
    registerProvider: () => {},
    unregisterProvider: () => {},
    set: () => {},
    authStorage: {
      list: () => [] as string[],
      get: (_provider: string) => undefined as any,
      login: async () => { throw new Error("OAuth not available in Pux backend mode"); },
      logout: () => {},
      getOAuthProviders: () => [] as any[],
    },
    keys: () => [] as string[],
  };
  public promptTemplates: any[] = [];
  public messages: any[] = [];
  public steeringMessages: string[] = [];
  public followUpMessages: string[] = [];
  public tasks: any[] = [];
  public autoCompactionEnabled = false;
  public state: any = { messages: [], model: DEFAULT_MODEL };
  public systemPrompt = "";
  public extensionRunner: any = null;
  public session: any;
  public steeringMode: "all" | "one-at-a-time" = "one-at-a-time";
  public followUpMode: "all" | "one-at-a-time" = "one-at-a-time";
  public retryAttempt = 0;
  public pendingMessageCount = 0;
  public sessionFile: string | undefined;

  private serverUrl: string;
  private project: string;
  private modelName: string;
  private abortCtrl?: AbortController;
  private listeners: AgentSessionEventListener[] = [];
  private streaming = false;
  private _fetch: typeof fetch; // injectable for tests

  constructor(
    settingsManager: SettingsManager,
    sessionManager: any,
    serverUrl: string,
    project: string,
    modelName: string,
    customFetch?: typeof fetch,
  ) {
    this.settingsManager = settingsManager;
    this.sessionManager = sessionManager;
    this.serverUrl = serverUrl;
    this.project = project;
    this.modelName = modelName;
    this._fetch = customFetch ?? globalThis.fetch.bind(globalThis);
    this.agent = {
      state: { messages: [] },
      model: DEFAULT_MODEL,
      thinkingLevel: "none",
      abort: () => this.abort(),
      waitForIdle: async () => {},
    };
    this.session = this;
  }

  // ---- subscribe (called by InteractiveMode after init) ----

  subscribe(listener: AgentSessionEventListener): () => void {
    this.listeners.push(listener);
    return () => {
      this.listeners = this.listeners.filter((l) => l !== listener);
    };
  }

  // ---- prompt (called from the main event loop: line 641) ----

  async prompt(text: string, options?: { images?: any[] }): Promise<void> {
    // 1. Emit user message so TUI renders it
    this.emit({ type: "message_start", message: userMessage(text) });

    // 2. Emit agent_start (TUI shows loader)
    this.emit({ type: "agent_start" });
    this.streaming = true;

    // 3. Emit message_start for assistant (creates streaming component)
    const assistantMsg = mkAssistant();
    this.emit({ type: "message_start", message: assistantMsg });

    this.abortCtrl = new AbortController();

    // 4. POST to Go backend
    const body = JSON.stringify({
      message: text,
      project: this.project,
      agentId: "default",
      model: this.modelName,
      thinkingLevel: this.thinkingLevel !== "none" ? this.thinkingLevel : undefined,
    });

    try {
      const response = await this._fetch(`${this.serverUrl}/api/pux/prompt`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body,
        signal: this.abortCtrl.signal,
      });

      if (!response.ok) {
        const errText = await response.text();
        throw new Error(`Backend ${response.status}: ${errText}`);
      }

      // 5. Parse SSE stream
      const reader = response.body!.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let currentEvent = "";
      let accText = "";
      let accThinking = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";

        for (const line of lines) {
          const t = line.trimEnd();
          if (t.startsWith("event: ")) {
            currentEvent = t.slice(7).trim();
          } else if (t.startsWith("event:")) {
            currentEvent = t.slice(6).trim();
          } else if (t.startsWith("data: ")) {
            const raw = t.slice(6).trim();
            if (raw === "[DONE]") {
              currentEvent = "";
              continue;
            }
            if (currentEvent) {
              try {
                const payload = JSON.parse(raw);
                this.handleSSE(currentEvent, payload, { accText, accThinking });
                // Update accumulators
                if (currentEvent === "text_delta") {
                  accText += payload.text || "";
                }
                if (currentEvent === "thinking_delta") {
                  accThinking += payload.text || "";
                }
              } catch {
                // malformed JSON — skip
              }
            }
            currentEvent = "";
          }
          if (t === "") currentEvent = "";
        }
      }
      // SSE stream ended normally

    } catch (err: any) {
      if (err.name !== "AbortError") {
        let errorMsg = err.message || String(err);
        // Friendlier messages for common failures
        if (err.cause?.code === "ECONNREFUSED" || errorMsg.includes("Connection refused")) {
          errorMsg = `Backend not running at ${this.serverUrl}. Start it with: task dev`;
        } else if (err.cause?.code === "ENOTFOUND" || errorMsg.includes("fetch failed")) {
          errorMsg = `Cannot reach ${this.serverUrl}. Check if the backend is running.`;
        } else if (errorMsg.includes("Project not found")) {
          errorMsg = `Project "${this.project}" not registered. Register it with: orch project add ${this.project}`;
        }
        const errMsg = mkAssistant({
          stopReason: "error",
          errorMessage: errorMsg,
          status: "error",
        } as any);
        this.emit({ type: "message_end", message: errMsg });
      }
    } finally {
      this.emit({ type: "agent_end", messages: [...this.messages] });
      this.streaming = false;
    }
  }

  // ---- SSE event → pi-mono AgentSessionEvent ----

  private handleSSE(eventType: string, payload: any, acc: { accText: string; accThinking: string }): void {
    switch (eventType) {
      case "agent_start":
        break;

      case "thinking_delta": {
        const content: any[] = [];
        if (acc.accThinking || payload.text) content.push({ type: "thinking", thinking: (acc.accThinking || "") + (payload.text || "") });
        if (acc.accText) content.push({ type: "text", text: acc.accText });
        if (content.length > 0) this.emitMessageUpdate(content);
        break;
      }

      case "text_delta": {
        const content: any[] = [];
        if (acc.accThinking) content.push({ type: "thinking", thinking: acc.accThinking });
        if (acc.accText || payload.text) content.push({ type: "text", text: (acc.accText || "") + (payload.text || "") });
        if (content.length > 0) this.emitMessageUpdate(content);
        break;
      }

      case "tool_execution_start": {
        const toolCall: ToolCall = {
          type: "toolCall",
          id: payload.toolId || `tool_${Date.now()}`,
          name: payload.toolName || "unknown",
          arguments: payload.args || {},
        };
        this.emitMessageUpdate([toolCall]);
        break;
      }

      case "tool_execution_end": {
        this.emit({
          type: "tool_execution_end",
          toolCallId: payload.toolId || "",
          toolName: payload.toolName || "",
          result: payload.result !== undefined ? payload.result : payload.error || "",
          isError: !!payload.error,
        });
        break;
      }

      case "tool_update": {
        this.emit({
          type: "tool_execution_update",
          toolCallId: payload.toolId || "",
          toolName: payload.toolName || "",
          args: {},
          partialResult: payload.text || "",
        });
        break;
      }

      case "agent_end": {
        // Build final content from accumulators
        const content: any[] = [];
        if (acc.accThinking) content.push({ type: "thinking", thinking: acc.accThinking });
        if (acc.accText) content.push({ type: "text", text: acc.accText });
        const msg = mkAssistant({
          content,
          usage: {
            input: Number(payload.input) || 0,
            output: Number(payload.output) || 0,
            cacheRead: 0,
            cacheWrite: 0,
            totalTokens: (Number(payload.input) || 0) + (Number(payload.output) || 0),
          },
          stopReason: "stop",
        });
        this.emit({ type: "message_end", message: msg });

        // Update model contextWindow from backend if provided
        const cw = Number(payload.contextWindow) || 0;
        if (cw > 0 && this.model) {
          (this.model as any).contextWindow = cw;
          if (this.state.model) (this.state.model as any).contextWindow = cw;
          if (this.agent.model) (this.agent.model as any).contextWindow = cw;
        }
        break;
      }

      case "error": {
        const content: any[] = [];
        if (acc.accThinking) content.push({ type: "thinking", thinking: acc.accThinking });
        if (acc.accText) content.push({ type: "text", text: acc.accText });
        const msg = mkAssistant({
          content,
          stopReason: "error",
          errorMessage: payload.error || payload.message || "Unknown error",
        });
        this.emit({ type: "message_end", message: msg });
        break;
      }

      case "compaction_start":
        this.emit({ type: "compaction_start", reason: "manual" as any });
        break;

      case "compaction_end":
        this.emit({
          type: "compaction_end",
          reason: "manual" as any,
          result: undefined,
          aborted: false,
          willRetry: false,
        });
        break;

      case "artifact_created":
      case "artifact_updated":
      case "plan_created":
      case "plan_updated":
      case "subagent_start":
      case "subagent_end":
      case "approval_request":
      case "agent_spawned":
        break;

      default:
        break;
    }
  }

  // ---- helpers ----

  private emitMessageUpdate(content: AssistantMessage["content"]): void {
    const msg = mkAssistant({ content });
    this.emit({ type: "message_update", message: msg });
  }

  private emit(event: AgentSessionEvent): void {
    for (const l of this.listeners) {
      try { l(event); } catch (e) {
        // silently ignore handler errors
      }
    }
  }

  // ---- AgentSession stubs (no-ops for SSE-driven TUI) ----

  abort(): void { this.abortCtrl?.abort(); }
  dispose(): void { this.abort(); }
  cancelPendingRequests(): void { this.abort(); }
  get isStreaming(): boolean { return this.streaming; }
  get isCompacting(): boolean { return false; }
  get isBashRunning(): boolean { return false; }
  getCwd(): string { return this.sessionManager?.getCwd?.() || process.cwd(); }
  getContextUsage() {
    const cw = (this.model as any)?.contextWindow || 0;
    return { used: 0, limit: cw, contextWindow: cw, percent: cw > 0 ? 0 : null };
  }
  getAvailableThinkingLevels(): ThinkingLevel[] { return ["none", "low", "high"]; }
  getSessionStats() {
    return {
      turns: 0,
      messages: 0,
      userMessages: 0,
      assistantMessages: 0,
      toolCalls: 0,
      toolResults: 0,
      totalMessages: 0,
      sessionFile: undefined as string | undefined,
      sessionId: "pux-session",
      tokens: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
      cost: 0,
    };
  }
  getLastAssistantText(): string { return ""; }
  getFollowUpMessages(): string[] { return []; }
  getSteeringMessages(): string[] { return []; }
  getToolDefinition(_name: string) { return null; }
  getUserMessagesForForking() { return []; }
  setModel(model: any): void {
    this.model = model;
    this.state.model = model;
    this.agent.model = model;
    // Notify Go backend in background
    this._fetch(`${this.serverUrl}/api/pux/model`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        project: this.project,
        provider: model.provider,
        modelId: `${model.provider}/${model.id}`,
        agentId: "default",
      }),
    }).catch(() => {});
  }
  setThinkingLevel(level: ThinkingLevel): void { this.thinkingLevel = level; }
  setScopedModels(models: any[]): void {
    this.scopedModels = models;
    if (models.length > 0) {
      this._availableModels = models.map((s) => s.model);
    }
  }
  setAutoCompactionEnabled(enabled: boolean): void { this.autoCompactionEnabled = enabled; }
  setSteeringMode(mode: "all" | "one-at-a-time"): void { this.steeringMode = mode; }
  setFollowUpMode(mode: "all" | "one-at-a-time"): void { this.followUpMode = mode; }
  async cycleModel(): Promise<any> {
    const models = this.scopedModels.length > 0
      ? this.scopedModels.map((s) => s.model)
      : this._availableModels;
    if (models.length <= 1) return undefined;
    const idx = models.findIndex((m: any) => m.id === this.model.id);
    const next = idx < 0 || idx >= models.length - 1 ? models[0] : models[idx + 1];
    if (next.thinkingLevel !== undefined) {
      this.thinkingLevel = next.thinkingLevel;
    }
    this.setModel(next);
    return { model: next, thinkingLevel: this.thinkingLevel };
  }
  async cycleThinkingLevel(): Promise<void> {}
  async compact(_opts?: any): Promise<any> { return undefined; }
  async reload(): Promise<void> {}
  async steer(text: string): Promise<void> { await this.prompt(text); }
  async followUp(text: string): Promise<void> { await this.prompt(text); }
  async navigateTree(_targetId: string, _opts?: any): Promise<any> { return undefined; }
  async executeBash(_command: string): Promise<any> { return ""; }
  async recordBashResult(_result: any): Promise<void> {}
  async exportToHtml(_path?: string): Promise<any> { return undefined; }
  async exportToJsonl(): Promise<any> { return undefined; }
  abortBash(): void {}
  abortCompaction(): void {}
  abortBranchSummary(): void {}
  abortRetry(): void {}
  async clearQueue(): Promise<void> {}
  async bindExtensions(_opts?: any): Promise<void> {}
  async saveMessages(): Promise<void> {}
}
