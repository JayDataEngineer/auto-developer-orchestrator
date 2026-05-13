// PuxAgentSession — bridges Go SSE backend to pi-mono InteractiveMode AgentSessionEvent types.
// Satisfies the interface InteractiveMode expects (duck-typed, no TypeScript enforcement at runtime).

import type { AgentSessionEvent, AgentSessionEventListener } from "./agent-session.js";
import type { ThinkingLevel } from "../agent-core/types.js";
import type {
  AssistantMessage, AssistantMessageEvent,
  TextContent, ThinkingContent, ToolCall,
} from "../ai/types.js";
import type { SettingsManager } from "./settings-manager.js";
import { SSEParser } from "./sse-parser.js";
import { ChatState } from "./chat-state.js";

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
  private _pendingQueue: Array<{ text: string; options?: any }> = [];
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

  // ---- Tracked session state (updated from SSE events) ----
  public chatState = new ChatState();  // Contract 4: canonical client state
  private _isCompacting = false;
  private _turnCount = 0;
  private _messageCount = 0;
  private _userMessageCount = 0;
  private _assistantMessageCount = 0;
  private _toolCallCount = 0;
  private _toolResultCount = 0;
  private _totalInputTokens = 0;
  private _totalOutputTokens = 0;
  private _totalCacheTokens = 0;
  private _lastAssistantText = "";
  private _accText = "";  // current turn text accumulator
  private _pendingHooks = new Map<string, { hookId: string; hookPoint: string }>();

	public agentId: string = "";
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

  async prompt(text: string, options?: { images?: any[]; streamingBehavior?: string }): Promise<void> {
    // If streaming and steer/followUp requested, queue the message
    if (this.streaming && options?.streamingBehavior) {
      this._pendingQueue.push({ text, options });
      // Emit queue_update so TUI refreshes pending messages display
      this.emit({ type: "queue_update" } as any);
      return;
    }

    // 1. Emit user message so TUI renders it
    const usrMsg = userMessage(text);
    this.emit({ type: "message_start", message: usrMsg });
    this.sessionManager?.appendMessage?.(usrMsg);
    this._messageCount++;
    this._userMessageCount++;

    // 2. Emit agent_start (TUI shows loader)
    this.emit({ type: "agent_start" });
    this.streaming = true;
    this._accText = "";
    this._turnCount++;

    // 2b. Start ChatState tracking for this turn
    this.chatState.handleEvent({ type: "message_start", message: { role: "assistant", content: [] } });

    // 3. Emit message_start for assistant (creates streaming component)
    const assistantMsg = mkAssistant();
    this.emit({ type: "message_start", message: assistantMsg });

    this.abortCtrl = new AbortController();

    // 4. POST to Go backend
    const body = JSON.stringify({
      message: text,
      project: this.project,
      agentId: this.agentId || undefined,
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

      // 5. Parse SSE stream — shared parser with web chat-panel
      const reader = response.body!.getReader();
      const decoder = new TextDecoder();
      const sseParser = new SSEParser();
      let accText = "";
      let accThinking = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const events = sseParser.feed(decoder.decode(value, { stream: true }));
        for (const evt of events) {
          this.handleSSE(evt.event, evt.data, { accText, accThinking });
          // Update accumulators
          if (evt.event === "text_delta") {
            accText += evt.data.text || "";
            this._accText = accText;
          }
          if (evt.event === "thinking_delta") {
            accThinking += evt.data.text || "";
          }
          // Feed to ChatState (Contract 4 compliance)
          this.feedChatState(evt.event, evt.data, accText, accThinking);
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
      if (this._backgrounded) {
        // Task was backgrounded — emit backgrounded event, skip normal agent_end
        this._backgrounded = false;
        this.streaming = false;
        this.emit({ type: "task_backgrounded" } as any);
        this.emit({ type: "agent_end", messages: [...this.messages] });
      } else {
        this.emit({ type: "agent_end", messages: [...this.messages] });
        this.streaming = false;
        // Process pending queue — send next queued message
        if (this._pendingQueue.length > 0) {
          const next = this._pendingQueue.shift()!;
          // Fire-and-forget: the next prompt runs in its own promise
          this.prompt(next.text, next.options).catch(() => {});
        }
      }
    }
  }

  // ---- SSE event → pi-mono AgentSessionEvent ----

  private handleSSE(eventType: string, payload: any, acc: { accText: string; accThinking: string }): void {
    switch (eventType) {
      case "agent_start":
        break;

      case "thinking_delta": {
        if (payload.agentName) {
          // Sub-agent thinking — emit as dedicated event for SubAgentTracker
          this.emit({
            type: "subagent_thinking_delta",
            agentName: payload.agentName,
            text: payload.text || "",
          } as any);
        } else {
          const content: any[] = [];
          if (acc.accThinking || payload.text) content.push({ type: "thinking", thinking: (acc.accThinking || "") + (payload.text || "") });
          if (acc.accText) content.push({ type: "text", text: acc.accText });
          if (content.length > 0) this.emitMessageUpdate(content);
        }
        break;
      }

      case "text_delta": {
        if (payload.agentName) {
          // Sub-agent text — emit as dedicated event for SubAgentTracker
          this.emit({
            type: "subagent_text_delta",
            agentName: payload.agentName,
            text: payload.text || "",
          } as any);
        } else {
          const content: any[] = [];
          if (acc.accThinking) content.push({ type: "thinking", thinking: acc.accThinking });
          if (acc.accText || payload.text) content.push({ type: "text", text: (acc.accText || "") + (payload.text || "") });
          if (content.length > 0) this.emitMessageUpdate(content);
        }
        break;
      }

      case "tool_execution_start": {
        this._toolCallCount++;
        const toolCall: ToolCall = {
          type: "toolCall",
          id: payload.toolId || `tool_${Date.now()}`,
          name: payload.toolName || "unknown",
          arguments: payload.args || {},
        };
        // If this tool event comes from a sub-agent, emit it with agentName context
        if (payload.agentName) {
          this.emit({
            type: "tool_execution_start",
            toolCallId: toolCall.id,
            toolName: toolCall.name,
            args: toolCall.arguments,
            agentName: payload.agentName,
          } as any);
        } else {
          this.emitMessageUpdate([toolCall]);
        }
        break;
      }

      case "tool_execution_end": {
        this._toolResultCount++;
        // Normalize result to { content: [...] } format expected by TUI
        const rawResult = payload.result !== undefined ? payload.result : payload.error || "";
        const normalizedResult = typeof rawResult === "string"
          ? { content: [{ type: "text" as const, text: rawResult }] }
          : rawResult && typeof rawResult === "object" && !Array.isArray(rawResult) && !(rawResult as any).content
            ? { content: [{ type: "text" as const, text: this.extractToolResultText(rawResult, payload.toolName) }] }
            : rawResult;
        const endEvent: any = {
          type: "tool_execution_end",
          toolCallId: payload.toolId || "",
          toolName: payload.toolName || "",
          result: normalizedResult,
          isError: !!payload.error,
        };
        if (payload.agentName) {
          endEvent.agentName = payload.agentName;
        }
        this.emit(endEvent);
        break;
      }

      case "tool_update": {
        // Normalize partialResult to { content: [...] } format expected by TUI
        const rawPartial = payload.text || "";
        const updateEvent: any = {
          type: "tool_execution_update",
          toolCallId: payload.toolId || "",
          toolName: payload.toolName || "",
          args: {},
          partialResult: { content: [{ type: "text", text: rawPartial }] },
        };
        if (payload.agentName) {
          updateEvent.agentName = payload.agentName;
        }
        this.emit(updateEvent);
        break;
      }

      case "agent_end": {
        // Build final content from accumulators
        const content: any[] = [];
        if (acc.accThinking) content.push({ type: "thinking", thinking: acc.accThinking });
        if (acc.accText) content.push({ type: "text", text: acc.accText });

        // Track token counts from backend
        const inputTokens = Number(payload.input) || 0;
        const outputTokens = Number(payload.output) || 0;
        const cacheTokens = Number(payload.cache) || 0;
        this._totalInputTokens += inputTokens;
        this._totalOutputTokens += outputTokens;
        this._totalCacheTokens += cacheTokens;

        // Track message counts
        this._messageCount++;
        this._assistantMessageCount++;
        if (acc.accText) this._lastAssistantText = acc.accText;

        const msg = mkAssistant({
          content,
          usage: {
            input: inputTokens,
            output: outputTokens,
            cacheRead: cacheTokens,
            cacheWrite: 0,
            totalTokens: inputTokens + outputTokens,
          },
          stopReason: "stop",
        });
        this.emit({ type: "message_end", message: msg });
        this.sessionManager?.appendMessage?.(msg);

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
        this.sessionManager?.appendMessage?.(msg);
        break;
      }

      case "compaction_start":
        this._isCompacting = true;
        this.emit({ type: "compaction_start", reason: "manual" as any });
        break;

      case "compaction_end":
        this._isCompacting = false;
        // Preserve Go backend context metrics for extensions/footer
        (this as any)._lastCompactionMetrics = {
          compactionType: payload.compactionType || null,
          contextTokens: payload.contextTokens || 0,
          contextSize: payload.contextSize || 0,
          contextUtil: payload.contextUtil || 0,
        };
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
      case "approval_request":
        break;

      case "subagent_start": {
        const agentName = payload.agentName || "agent";
        const task = payload.task || "";
        this._activeSubAgents.set(agentName, { agentName, task, startTime: Date.now() });
        this.emit({
          type: "subagent_start" as any,
          agentName,
          task,
          toolName: payload.toolName || "delegate_to",
        } as any);
        break;
      }

      case "subagent_end": {
        const name = payload.agentName || "agent";
        this._activeSubAgents.delete(name);
        this.emit({
          type: "subagent_end" as any,
          agentName: name,
          status: payload.status || "completed",
          error: payload.error || "",
        } as any);
        break;
      }

      case "user_question": {
        // AI asks the user a question — emit to TUI for overlay
        this.emit({
          type: "user_question" as any,
          questionId: payload.questionId || "",
          question: payload.question || "",
          options: payload.options || [],
          allowFreeText: payload.allowFreeText !== false,
          defaultAnswer: payload.default || "",
        } as any);
        break;
      }

      case "plan_created": {
        this.emit({
          type: "plan_created" as any,
          planId: payload.planId || "",
          name: payload.name || "",
          content: payload.content || "",
          filePath: payload.filePath || "",
        } as any);
        break;
      }

      case "agent_spawned":
        if (payload?.agentId) {
          this.agentId = payload.agentId;
          // Also store on the session-like object for TUI display
          (this as any)._sessionId = payload.agentId;
        }
        break;

      case "step_start": {
        // Step boundary — optionally track for UI rendering
        break;
      }

      case "step_end": {
        // Step boundary — optionally track for UI rendering
        break;
      }

      case "hook_request": {
        // Agent loop is paused waiting for a hook response.
        // Forward to extension system — extensions can allow/block/modify.
        this.emit({
          type: "hook_request" as any,
          hookId: payload.hookId || "",
          hookPoint: payload.hookPoint || "",
          toolName: payload.toolName || "",
          args: payload.args || {},
          result: payload.result,
        } as any);

        // If no extension handles it, auto-allow after a short delay
        // Extensions call submitHookResponse() to respond explicitly
        setTimeout(() => {
          if (this._pendingHooks.has(payload.hookId)) {
            this._pendingHooks.delete(payload.hookId);
            this.submitHookResponse(payload.hookId, "allow").catch(() => {});
          }
        }, 5000);
        break;
      }

      case "grind_attempt": {
        this.emit({
          type: "grind_attempt" as any,
          agentName: payload.agentName || "",
          task: payload.task || "",
          status: payload.status || "running",
        } as any);
        break;
      }

      case "grind_verify": {
        this.emit({
          type: "grind_verify" as any,
          agentName: payload.agentName || "",
          task: payload.task || "",
          status: payload.status || "fail",
        } as any);
        break;
      }

      case "grind_end": {
        this.emit({
          type: "grind_end" as any,
          status: payload.status || "failed",
          task: payload.task || "",
        } as any);
        break;
      }

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

  // ---- Background task support ----

  private _backgrounded = false;

  // Track active sub-agents for TUI visibility
  private _activeSubAgents = new Map<string, { agentName: string; task: string; startTime: number }>();

  /**
   * Send the current streaming task to the background.
   * Aborts the SSE reader (backend continues via context.Background()),
   * then emits a backgrounded event so the TUI can return to input mode.
   */
  background(): void {
    if (!this.streaming) return;
    this._backgrounded = true;
    // Abort the SSE reader — backend continues independently
    // The prompt() catch/finally will see _backgrounded and skip agent_end
    this.abortCtrl?.abort();
  }

  // ---- AgentSession stubs (no-ops for SSE-driven TUI) ----

  abort(): void { this.abortCtrl?.abort(); }
  dispose(): void { this.abort(); }
  cancelPendingRequests(): void { this.abort(); }
  get isStreaming(): boolean { return this.streaming; }
  get isCompacting(): boolean { return this._isCompacting; }
  get isBashRunning(): boolean { return false; }
  getCwd(): string { return this.sessionManager?.getCwd?.() || process.cwd(); }
  getContextUsage() {
    const cw = (this.model as any)?.contextWindow || 0;
    const used = this._totalInputTokens + this._totalOutputTokens;
    return { used, limit: cw, contextWindow: cw, percent: cw > 0 ? Math.min(used / cw, 1) : null };
  }
  getAvailableThinkingLevels(): ThinkingLevel[] { return ["none", "low", "high"]; }
  getSessionStats() {
    return {
      turns: this._turnCount,
      messages: this._messageCount,
      userMessages: this._userMessageCount,
      assistantMessages: this._assistantMessageCount,
      toolCalls: this._toolCallCount,
      toolResults: this._toolResultCount,
      totalMessages: this._messageCount,
      sessionFile: this.sessionFile,
      sessionId: this.agentId || "pux-session",
      tokens: {
        input: this._totalInputTokens,
        output: this._totalOutputTokens,
        cacheRead: this._totalCacheTokens,
        cacheWrite: 0,
        total: this._totalInputTokens + this._totalOutputTokens,
      },
      cost: 0,
    };
  }
  getLastAssistantText(): string {
    // Contract 4: prefer ChatState as canonical source
    const msgs = this.chatState.messages;
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role === "assistant" && msgs[i].text) return msgs[i].text;
    }
    return this._lastAssistantText;  // fallback
  }

  /** Feed raw SSE events into ChatState for contract-compliant state tracking */
  private feedChatState(eventType: string, payload: any, accText: string, accThinking: string): void {
    switch (eventType) {
      case "text_delta": {
        const content: any[] = [];
        if (accThinking) content.push({ type: "thinking", thinking: accThinking });
        content.push({ type: "text", text: accText });
        this.chatState.handleEvent({ type: "message_update", message: { content } });
        break;
      }
      case "thinking_delta": {
        const content: any[] = [];
        content.push({ type: "thinking", thinking: accThinking });
        if (accText) content.push({ type: "text", text: accText });
        this.chatState.handleEvent({ type: "message_update", message: { content } });
        break;
      }
      case "tool_execution_start":
        this.chatState.handleEvent({
          type: "tool_execution_start",
          toolCallId: payload.toolId || `ext_${Date.now()}`,
          toolName: payload.toolName || "tool",
          args: payload.args,
        });
        break;
      case "tool_execution_end":
        this.chatState.handleEvent({
          type: "tool_execution_end",
          toolCallId: payload.toolId || "",
          result: payload.result,
          isError: !!payload.error,
        });
        break;
      case "agent_end":
        this.chatState.handleEvent({ type: "message_end", message: { content: [] } });
        this.chatState.handleEvent({ type: "agent_end" });
        break;
      case "error":
        this.chatState.handleEvent({ type: "message_end", message: { stopReason: "error", errorMessage: payload.error } });
        break;
    }
  }

  /** Extract readable text from structured tool results instead of dumping JSON */
  private extractToolResultText(result: any, toolName?: string): string {
    // Delegate tools: show summary, not raw JSON
    if (result.agent_ref || result.agentId) {
      const parts: string[] = [];
      if (result.result) parts.push(typeof result.result === "string" ? result.result : this.extractToolResultText(result.result, toolName));
      if (result.summary) parts.push(result.summary);
      if (result.output) parts.push(typeof result.output === "string" ? result.output : JSON.stringify(result.output));
      if (result.error) parts.push(`Error: ${result.error}`);
      return parts.length > 0 ? parts.join("\n") : `Delegated to ${result.agent_ref || result.agentId || "agent"}`;
    }

    // Common patterns: { content: "..." } or { text: "..." } or { message: "..." }
    if (typeof result.content === "string") return result.content;
    if (typeof result.text === "string") return result.text;
    if (typeof result.message === "string") return result.message;
    if (typeof result.output === "string") return result.output;
    if (typeof result.summary === "string") return result.summary;
    if (typeof result.error === "string") return `Error: ${result.error}`;

    // Shell/bash results: { stdout, stderr, exit_code }
    if (result.stdout !== undefined) {
      let out = result.stdout || "";
      if (result.stderr) out += (out ? "\n" : "") + result.stderr;
      if (result.exit_code && result.exit_code !== 0) out += `\n(exit code ${result.exit_code})`;
      return out || "(no output)";
    }

    // { path, content } file results
    if (result.path && result.content) return `${result.path}:\n${result.content}`;

    // Array of items — join them
    if (Array.isArray(result)) return result.map(r => typeof r === "string" ? r : this.extractToolResultText(r, toolName)).join("\n");

    // Last resort: key=value pairs, sorted, skip empty/internal fields
    const skip = new Set(["_type", "type", "id", "tool_use_id"]);
    const entries = Object.entries(result).filter(([k, v]) => !skip.has(k) && v !== undefined && v !== null && v !== "");
    if (entries.length === 0) return "(empty result)";
    return entries.map(([k, v]) => `${k}: ${typeof v === "string" ? v : JSON.stringify(v)}`).join("\n");
  }
  getFollowUpMessages(): string[] { return []; }
  getSteeringMessages(): string[] { return []; }
  getToolDefinition(name: string) {
    if (this.extensionRunner) {
      const toolDef = this.extensionRunner.getToolDefinition(name);
      if (toolDef) return toolDef;
    }
    return null;
  }
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
  async compact(_opts?: any): Promise<any> {
    try {
      const res = await this._fetch(`${this.serverUrl}/api/pux/compact`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          project: this.project,
          agentId: this.agentId || "default",
        }),
      });
      if (!res.ok) return undefined;
      return await res.json();
    } catch {
      return undefined;
    }
  }
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
  async bindExtensions(opts?: any): Promise<void> {
    if (this.extensionRunner && opts?.uiContext) {
      this.extensionRunner.setUIContext(opts.uiContext);
    }
  }
  async saveMessages(): Promise<void> {}

  // ---- Session listing / resuming ----

  /** Fetch all available sessions from the backend */
  async getSessions(): Promise<Array<{ project: string; agentId: string; title: string; lastMessage: string; lastAt: string; messageCount: number }>> {
    try {
      const res = await this._fetch(`${this.serverUrl}/api/pux/conversations?project=${encodeURIComponent(this.project)}`);
      if (!res.ok) return [];
      const data = await res.json();
      return data.map((s: any) => ({
        project: s.project || "",
        agentId: s.agentId || "",
        title: s.title || s.lastMessage?.slice(0, 60) || "Untitled",
        lastMessage: s.lastMessage || "",
        lastAt: s.lastAt || "",
        messageCount: s.messageCount || 0,
      }));
    } catch {
      return [];
    }
  }

  /** Load message history for a given session and emit them for display */
  async loadHistory(agentId: string, project?: string): Promise<any[]> {
    this.agentId = agentId;
    try {
      const proj = project || this.project;
      const res = await this._fetch(`${this.serverUrl}/api/pux/history?project=${encodeURIComponent(proj)}&agentId=${encodeURIComponent(agentId)}`);
      if (!res.ok) return [];
      const msgs = await res.json();
      return msgs;
    } catch {
      return [];
    }
  }

  /** Delete a conversation from the backend */
  async deleteSession(project: string, agentId: string): Promise<boolean> {
    try {
      const res = await this._fetch(
        `${this.serverUrl}/api/pux/conversation?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(agentId)}`,
        { method: "DELETE" },
      );
      return res.ok;
    } catch {
      return false;
    }
  }

  /** Rename a conversation in the backend */
  async renameSession(project: string, agentId: string, title: string): Promise<boolean> {
    try {
      const res = await this._fetch(`${this.serverUrl}/api/pux/conversation/rename`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ project, agentId, title }),
      });
      return res.ok;
    } catch {
      return false;
    }
  }

  /** Submit a response to an ask_user question */
  async submitUserResponse(questionId: string, response: string): Promise<boolean> {
    try {
      const res = await this._fetch(`${this.serverUrl}/api/pux/user-response`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ questionId, response }),
      });
      return res.ok;
    } catch {
      return false;
    }
  }

  async submitPlanResponse(planId: string, action: string, feedback?: string): Promise<boolean> {
    try {
      const res = await this._fetch(`${this.serverUrl}/api/pux/plan-response`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ planId, action, feedback: feedback || "" }),
      });
      return res.ok;
    } catch {
      return false;
    }
  }

  /** Respond to a hook_request — allows the TUI/extensions to allow/block/modify agent loop actions */
  async submitHookResponse(hookId: string, action: "allow" | "block" | "modify", data?: Record<string, any>, reason?: string): Promise<boolean> {
    this._pendingHooks.delete(hookId);
    try {
      const res = await this._fetch(`${this.serverUrl}/api/pux/hook-response`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ hookId, action, data, reason }),
      });
      return res.ok;
    } catch {
      return false;
    }
  }

  /** Get the session tree for navigation */
  async getTree(project?: string): Promise<{ sessionId: string; currentNode: string; nodes: any[] } | null> {
    try {
      const proj = project || this.project;
      const res = await this._fetch(`${this.serverUrl}/api/pux/tree?project=${encodeURIComponent(proj)}&agentId=${encodeURIComponent(this.agentId || "default")}`);
      if (!res.ok) return null;
      return await res.json();
    } catch {
      return null;
    }
  }

  /** Fork the session at a given node */
  async forkSession(nodeId: string, project?: string): Promise<{ forkPath: string; forkId: string } | null> {
    try {
      const res = await this._fetch(`${this.serverUrl}/api/pux/fork`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          project: project || this.project,
          agentId: this.agentId || "default",
          nodeId,
        }),
      });
      if (!res.ok) return null;
      return await res.json();
    } catch {
      return null;
    }
  }
}
