// PuxAgentSession — wraps Go SSE backend, satisfies pi-mono AgentSession interface
// for InteractiveMode TUI rendering.

import { EventEmitter } from "node:events";
import type { AgentSessionEvent } from "./agent-session.js";
import type { ThinkingLevel } from "../agent-core/types.js";
import type { Model, AssistantMessage } from "../ai/types.js";
import type { FooterDataProvider } from "./footer-data-provider.js";
import type { KeybindingsManager } from "./keybindings.js";
import type { SettingsManager } from "./settings-manager.js";

interface PuxModel {
  id: string;
  name: string;
  api: string;
  provider: string;
}

const DEFAULT_MODEL: PuxModel = {
  id: "pux-qwen",
  name: "Pux Qwen",
  api: "pux",
  provider: "pux",
};

export class PuxAgentSession extends EventEmitter {
  public settingsManager: SettingsManager;
  public sessionManager: any;
  public agent: any;
  public model: PuxModel = DEFAULT_MODEL;
  public thinkingLevel: ThinkingLevel = "none";
  public scopedModels: Array<{ model: PuxModel; thinkingLevel?: ThinkingLevel }> = [];
  public resourceLoader: any = {
    getThemes: () => ({ themes: [] }),
    getExtensions: () => ({ extensions: [], diagnostics: [] }),
    getSkills: () => ({ skills: [] }),
    reload: async () => {},
  };
  public modelRegistry: any = {};
  public promptTemplates: any[] = [];
  public messages: any[] = [];
  public steeringMessages: string[] = [];
  public followUpMessages: string[] = [];
  public tasks: any[] = [];
  public autoCompactionEnabled = false;
  public state: any = { messages: [] };
  public systemPrompt = "";
  public extensionRunner: any = null;
  public session: any;
  public steeringMode = false;
  public followUpMode = false;
  public retryAttempt = 0;
  public pendingMessageCount = 0;

  private serverUrl: string;
  private project: string;
  private modelName: string;
  private abortController?: AbortController;
  private eventQueue: AgentSessionEvent[] = [];
  private listeners: Array<(event: AgentSessionEvent) => void> = [];

  constructor(settingsManager: SettingsManager, sessionManager: any, serverUrl: string, project: string, modelName: string) {
    super();
    this.settingsManager = settingsManager;
    this.sessionManager = sessionManager;
    this.serverUrl = serverUrl;
    this.project = project;
    this.modelName = modelName;
    this.agent = {
      state: { messages: [] },
      model: DEFAULT_MODEL,
      thinkingLevel: "none",
      abort: () => this.abort(),
    };
    this.session = this;
  }

  // --- Core methods needed by InteractiveMode ---

  subscribe(listener: (event: AgentSessionEvent) => void): () => void {
    this.listeners.push(listener);
    // Drain queued events to the new subscriber
    const queued = [...this.eventQueue];
    this.eventQueue = [];
    for (const event of queued) {
      listener(event);
    }
    return () => {
      this.listeners = this.listeners.filter((l) => l !== listener);
    };
  }

  async prompt(text: string, options?: { images?: any[]; streamingBehavior?: string }): Promise<void> {
    // Build the full prompt with images if any
    const body: any = {
      prompt: text,
      project: this.project,
      model: this.modelName,
      stream: true,
    };

    if (options?.images?.length) {
      body.images = options.images.map((img: any) => ({
        data: img.data,
        media_type: img.media_type || "image/png",
      }));
    }

    // Emit agent start
    this.emitEvent({ type: "agent_start" });
    this.emitEvent({ type: "turn_start" });

    this.abortController = new AbortController();

    try {
      const response = await fetch(`${this.serverUrl}/api/pux/prompt`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
        signal: this.abortController.signal,
      });

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Server error ${response.status}: ${errorText}`);
      }

      const reader = response.body!.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let partialText = "";
      let partialContent: any[] = [];

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";

        for (const line of lines) {
          const trimmed = line.trim();
          if (!trimmed.startsWith("data: ")) continue;

          const data = trimmed.slice(6).trim();
          if (data === "[DONE]") continue;
          if (!data) continue;

          try {
            const event = JSON.parse(data);
            this.handleSSEEvent(event);
          } catch {
            // skip parse errors
          }
        }
      }
    } catch (err: any) {
      if (err.name === "AbortError") return;
      this.emitEvent({
        type: "message_end",
        message: { role: "assistant", content: [], errorMessage: err.message },
      });
    } finally {
      this.emitEvent({ type: "agent_end", messages: [...this.messages] });
    }
  }

  private handleSSEEvent(event: any) {
    switch (event.type) {
      case "think": {
        // Thinking content — emit as assistant message with thinking delta
        const msg: any = {
          role: "assistant",
          content: [{ type: "thinking", thinking: event.content || "" }],
          api: "pux",
          provider: "pux",
          model: this.modelName,
          usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0 },
          stopReason: "stop" as const,
          timestamp: Date.now(),
        };
        this.emitEvent({ type: "message_update", message: msg, assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: event.content || "" } as any });
        break;
      }

      case "text": {
        const msg: any = {
          role: "assistant",
          content: [{ type: "text", text: event.content || "" }],
          api: "pux",
          provider: "pux",
          model: this.modelName,
          usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0 },
          stopReason: "stop",
          timestamp: Date.now(),
        };
        this.emitEvent({ type: "message_update", message: msg, assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: event.content || "" } as any });
        break;
      }

      case "tool_call": {
        const toolCallId = event.tool_call_id || "tool_" + Date.now();
        this.emitEvent({
          type: "tool_execution_start",
          toolCallId,
          toolName: event.tool_name || "unknown",
          args: event.data || {},
        });
        // Also emit message update for the tool call
        const msg: any = {
          role: "assistant",
          content: [{ type: "toolCall", id: toolCallId, name: event.tool_name, arguments: event.data || {} }],
          api: "pux",
          provider: "pux",
          model: this.modelName,
          usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0 },
          stopReason: "tool_calls",
          timestamp: Date.now(),
        };
        this.emitEvent({ type: "message_update", message: msg, assistantMessageEvent: { type: "toolcall_start", contentIndex: 0 } as any });
        break;
      }

      case "tool_result": {
        const toolCallId = event.tool_call_id || "";
        this.emitEvent({
          type: "tool_execution_end",
          toolCallId,
          toolName: event.tool_name || "unknown",
          result: event.data || event.content || "",
          isError: false,
        });
        break;
      }

      case "agent_end": {
        const tokens = { input: event.inputTokens || 0, output: event.outputTokens || 0 };
        const msg: any = {
          role: "assistant",
          content: [],
          api: "pux",
          provider: "pux",
          model: this.modelName,
          usage: { input: tokens.input, output: tokens.output, cacheRead: 0, cacheWrite: 0, totalTokens: tokens.input + tokens.output },
          stopReason: "stop",
          timestamp: Date.now(),
        };
        this.emitEvent({ type: "message_end", message: msg });
        this.emitEvent({ type: "turn_end", message: msg, toolResults: [] });
        break;
      }

      case "error": {
        const msg: any = {
          role: "assistant",
          content: [],
          errorMessage: event.content || "Unknown error",
          api: "pux",
          provider: "pux",
          model: this.modelName,
          usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0 },
          stopReason: "error",
          timestamp: Date.now(),
        };
        this.emitEvent({ type: "message_end", message: msg });
        break;
      }

      default:
        // pass through unknown events
        break;
    }
  }

  abort(): void {
    this.abortController?.abort();
  }

  // --- Agent dispatch stubs (no-ops for SSE-driven TUI) ---

  isStreaming(): boolean { return !!this.abortController && !this.abortController.signal.aborted; }
  isCompacting(): boolean { return false; }
  isBashRunning(): boolean { return false; }
  getCwd(): string { return this.sessionManager?.getCwd?.() || process.cwd(); }
  getContextUsage() { return { used: 0, limit: 128000 }; }
  getAvailableThinkingLevels(): ThinkingLevel[] { return ["none", "low", "high"]; }
  getSessionStats() { return { turns: 0, messages: 0 }; }
  getLastAssistantText(): string { return ""; }
  getFollowUpMessages(): string[] { return this.followUpMessages; }
  getSteeringMessages(): string[] { return this.steeringMessages; }
  getToolDefinition(name: string) { return null; }
  getUserMessagesForForking() { return []; }

  setModel(model: any): void { this.model = model; }
  setThinkingLevel(level: ThinkingLevel): void { this.thinkingLevel = level; }
  setScopedModels(models: any[]): void { this.scopedModels = models; }
  setAutoCompactionEnabled(enabled: boolean): void { this.autoCompactionEnabled = enabled; }
  setSteeringMode(enabled: boolean): void { this.steeringMode = enabled; }
  setFollowUpMode(mode: string): void { this.followUpMode = mode === "on"; }

  async cycleModel(): Promise<void> {}
  async cycleThinkingLevel(): Promise<void> {}
  async compact(opts?: any): Promise<any> { return undefined; }
  async reload(): Promise<void> {}
  async steer(text: string): Promise<void> {
    await this.prompt(text, { streamingBehavior: "steer" });
  }
  async followUp(text: string): Promise<void> {
    await this.prompt(text, { streamingBehavior: "followUp" });
  }
  async navigateTree(targetId: string, opts?: any): Promise<any> { return undefined; }
  async executeBash(command: string): Promise<any> { return ""; }
  async recordBashResult(result: any): Promise<void> {}
  async exportToHtml(path?: string): Promise<any> { return undefined; }
  async exportToJsonl(): Promise<any> { return undefined; }
  abortBash(): void {}
  abortCompaction(): void {}
  abortBranchSummary(): void {}
  abortRetry(): void {}
  async clearQueue(): Promise<void> {}
  async bindExtensions(opts?: any): Promise<void> {}
  async saveMessages(): Promise<void> {}

  // --- Event emission helper ---

  private emitEvent(event: AgentSessionEvent): void {
    for (const listener of this.listeners) {
      try { listener(event); } catch {}
    }
  }
}
