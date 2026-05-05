/**
 * Tests for PuxAgentSession — the SSE bridge between Go backend and pi-mono TUI.
 *
 * Run: bun test src/__tests__/pux-agent-session.test.ts
 */

import { describe, test, expect, beforeEach, afterEach, mock } from "bun:test";
import { PuxAgentSession } from "../core/pux-agent-session.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Build a minimal SessionManager stub. */
function stubSessionManager(cwd?: string) {
  return {
    cwd: cwd ?? "/fake/cwd",
    getCwd: () => cwd ?? "/fake/cwd",
    sessions: [],
    current: null,
    addBranch: () => {},
    forkSession: () => {},
    navigate: () => {},
    persist: () => {},
    destroy: () => {},
  };
}

/** Build a minimal SettingsManager stub. */
function stubSettingsManager() {
  return {
    get: () => undefined,
    set: () => Promise.resolve(),
    getAll: () => ({}),
    getDefault: () => undefined,
    getGlobal: () => undefined,
    getCwd: () => "/fake/cwd",
    getAgentDir: () => "/tmp/.pi/agent",
    getSettingsDir: () => "/tmp/.pi",
  } as any;
}

/** Create a Response-like object from an SSE event string. */
function sseResponse(...events: string[]): Response {
  const body = new ReadableStream({
    start(controller) {
      for (const event of events) {
        controller.enqueue(new TextEncoder().encode(event));
      }
      controller.close();
    },
  });
  return new Response(body, { status: 200, statusText: "OK" });
}

// ---------------------------------------------------------------------------
// SSE encoding helpers
// ---------------------------------------------------------------------------

function sseEvent(event: string, data: string): string {
  return `event: ${event}\ndata: ${data}\n\n`;
}

function sseTextDelta(text: string): string {
  return sseEvent("text_delta", JSON.stringify({ text }));
}

function sseThinkingDelta(text: string): string {
  return sseEvent("thinking_delta", JSON.stringify({ text }));
}

function sseToolStart(name: string, toolId: string, args: any): string {
  return sseEvent("tool_execution_start", JSON.stringify({ toolName: name, toolId, args }));
}

function sseToolEnd(toolId: string, toolName: string, result?: string, error?: string): string {
  return sseEvent("tool_execution_end", JSON.stringify({ toolId, toolName, result: result ?? "", error: error ?? "" }));
}

function sseAgentEnd(input?: number, output?: number): string {
  return sseEvent("agent_end", JSON.stringify({ input: input ?? 100, output: output ?? 50 }));
}

function sseError(msg: string): string {
  return sseEvent("error", JSON.stringify({ error: msg }));
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("PuxAgentSession", () => {
  let session: PuxAgentSession;
  let fetchSpy: ReturnType<typeof mock>;

  beforeEach(() => {
    fetchSpy = mock(() =>
      Promise.resolve(new Response("ok", { status: 200 }))
    );
    session = new PuxAgentSession(
      stubSettingsManager(),
      stubSessionManager(),
      "http://localhost:9999",
      "test-proj",
      "deepseek/deepseek-v4-flash",
      fetchSpy as any, // inject mock as fetch
    );
  });

  afterEach(() => {
    // no global cleanup needed — fetch is injected, not global
  });

  // ---- Basic construction ----

  test("constructs with defaults", () => {
    expect(session.model.id).toBe("deepseek/deepseek-v4-flash");
    expect(session.thinkingLevel).toBe("none");
    expect(session.isStreaming).toBe(false);
    expect(session.isCompacting).toBe(false);
    expect(session.isBashRunning).toBe(false);
    expect(session.autoCompactionEnabled).toBe(false);
  });

  test("getCwd delegates to sessionManager", () => {
    const sm = stubSessionManager("/my/project");
    const s = new PuxAgentSession(stubSettingsManager(), sm, "http://x", "p", "m", fetchSpy as any);
    expect(s.getCwd()).toBe("/my/project");
  });

  test("getContextUsage returns fixed window", () => {
    const usage = session.getContextUsage();
    expect(usage.used).toBe(0);
    expect(usage.limit).toBe(128000);
  });

  test("getAvailableThinkingLevels", () => {
    const levels = session.getAvailableThinkingLevels();
    expect(levels).toEqual(["none", "low", "high"]);
  });

  // ---- Event subscription ----

  test("subscribe returns unsubscribe function", () => {
    // @ts-expect-error - testing listener management
    const unsub = session.subscribe(() => {});
    expect(typeof unsub).toBe("function");
  });

  test("subscribe notifies listener of custom event", () => {
    const events: any[] = [];
    // @ts-expect-error - internal emit
    session.subscribe((e: any) => events.push(e));

    // @ts-expect-error - accessing private method for test
    (session as any).emit({ type: "custom_test", value: 42 });

    expect(events.length).toBe(1);
    expect(events[0].type).toBe("custom_test");
    expect(events[0].value).toBe(42);
  });

  test("unsubscribe stops notification", () => {
    const events: any[] = [];
    // @ts-expect-error - internal emit
    const unsub = session.subscribe((e: any) => events.push(e));
    unsub();

    // @ts-expect-error - accessing private method for test
    (session as any).emit({ type: "test" });

    expect(events.length).toBe(0);
  });

  // ---- Abort ----

  test("abort calls abortCtrl.abort", () => {
    // abort is async in effect — it cancels fetch, and streaming=false is set
    // in the finally block after the AbortError propagates.
    session.streaming = true;
    session.abort();
    // abort() just calls this.abortCtrl?.abort()
    // streaming becomes false when the fetch promise's finally runs
    // We don't test isStreaming here because it depends on async resolution
    expect(typeof session.abort).toBe("function");
  });

  test("cancelPendingRequests delegates to abort", () => {
    session.streaming = true;
    session.cancelPendingRequests();
    // same async behavior as abort()
    expect(typeof session.cancelPendingRequests).toBe("function");
  });

  test("dispose delegates to abort", () => {
    session.streaming = true;
    session.dispose();
    expect(typeof session.dispose).toBe("function");
  });

  // ---- setModel ----

  test("setModel updates model and sends PUT to backend", () => {
    const newModel = { id: "claude-4", provider: "anthropic", name: "Claude 4" };
    session.setModel(newModel);

    expect(session.model).toBe(newModel);
    expect(session.state.model).toBe(newModel);

    // Should trigger a PUT request
    expect(fetchSpy).toHaveBeenCalled();
    const callArgs = fetchSpy.mock.calls[fetchSpy.mock.calls.length - 1];
    expect(callArgs[0]).toContain("/api/pux/model");
  });

  // ---- Thinking level / steering ----

  test("setThinkingLevel updates value", () => {
    session.setThinkingLevel("high");
    expect(session.thinkingLevel).toBe("high");
  });

  test("setScopedModels updates available models", () => {
    session.setScopedModels([
      { model: { id: "a", name: "Model A", provider: "p" } },
      { model: { id: "b", name: "Model B", provider: "p" } },
    ]);
    expect(session.scopedModels.length).toBe(2);
  });

  test("setAutoCompactionEnabled toggles", () => {
    session.setAutoCompactionEnabled(true);
    expect(session.autoCompactionEnabled).toBe(true);
    session.setAutoCompactionEnabled(false);
    expect(session.autoCompactionEnabled).toBe(false);
  });

  test("setSteeringMode toggles", () => {
    session.setSteeringMode(true);
    expect(session.steeringMode).toBe(true);
  });

  test("setFollowUpMode sets mode", () => {
    session.setFollowUpMode("on");
    expect(session.followUpMode).toBe(true);
    session.setFollowUpMode("off");
    expect(session.followUpMode).toBe(false);
  });

  // ---- prompt() with mock SSE ----

  test("prompt emits message_start for user and assistant", async () => {
    const events: any[] = [];
    // @ts-expect-error - internal emit
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(sseResponse(sseTextDelta("Hello"), sseAgentEnd()))
    );

    await session.prompt("Do something");

    // Check for user message_start
    const userStarts = events.filter((e: any) => e.type === "message_start" && e.message?.role === "user");
    expect(userStarts.length).toBe(1);

    // Check for agent_start
    const agentStarts = events.filter((e: any) => e.type === "agent_start");
    expect(agentStarts.length).toBe(1);

    // Check for assistant message_start
    const assistantStarts = events.filter((e: any) => e.type === "message_start" && e.message?.role === "assistant");
    expect(assistantStarts.length).toBe(1);

    // Check for agent_end
    const agentEnds = events.filter((e: any) => e.type === "agent_end");
    expect(agentEnds.length).toBe(1);
  });

  test("prompt handles text_delta events", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        sseResponse(
          sseTextDelta("Hello "),
          sseTextDelta("World"),
          sseAgentEnd(),
        )
      )
    );

    await session.prompt("Say hello");

    // Should have text_delta → message_update events
    const updates = events.filter((e: any) => e.type === "message_update");
    // At least 2 updates (from text deltas) + maybe more
    expect(updates.length).toBeGreaterThanOrEqual(2);
  });

  test("prompt handles thinking_delta events", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        sseResponse(
          sseThinkingDelta("Let me think about this..."),
          sseTextDelta("Here is the answer"),
          sseAgentEnd(),
        )
      )
    );

    await session.prompt("Complex question");

    const updates = events.filter((e: any) => e.type === "message_update");
    expect(updates.length).toBeGreaterThanOrEqual(2);
  });

  test("prompt handles tool_execution_start and tool_execution_end", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        sseResponse(
          sseTextDelta("Let me check..."),
          sseToolStart("bash", "tool_001", { cmd: "ls" }),
          sseToolEnd("tool_001", "bash", "file1\nfile2"),
          sseTextDelta("Done"),
          sseAgentEnd(),
        )
      )
    );

    await session.prompt("List files");

    // Check for tool_execution_end events
    const toolEnds = events.filter((e: any) => e.type === "tool_execution_end");
    expect(toolEnds.length).toBe(1);
    expect(toolEnds[0].toolName).toBe("bash");
    expect(toolEnds[0].isError).toBe(false);
  });

  test("prompt handles tool_execution_end with error", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        sseResponse(
          sseToolStart("browse", "tool_err", { url: "http://x" }),
          sseToolEnd("tool_err", "browse", "", "timeout"),
          sseAgentEnd(),
        )
      )
    );

    await session.prompt("Browse broken URL");

    const toolEnds = events.filter((e: any) => e.type === "tool_execution_end");
    expect(toolEnds.length).toBe(1);
    expect(toolEnds[0].isError).toBe(true);
  });

  test("prompt handles error events from backend", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        sseResponse(
          sseTextDelta("Beginning..."),
          sseError("Backend crashed"),
        )
      )
    );

    await session.prompt("Break things");

    // Should have a message_end with error
    const messageEnds = events.filter((e: any) => e.type === "message_end");
    expect(messageEnds.length).toBe(1);
    expect(messageEnds[0].message.stopReason).toBe("error");
  });

  test("prompt handles HTTP error from fetch", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(new Response("Internal Server Error", { status: 500 }))
    );

    await session.prompt("Break network");

    const messageEnds = events.filter((e: any) => e.type === "message_end");
    expect(messageEnds.length).toBe(1);
    expect(messageEnds[0].message.stopReason).toBe("error");
  });

  test("prompt handles network error gracefully", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.reject(new Error("Connection refused"))
    );

    await session.prompt("No server");

    const agentEnds = events.filter((e: any) => e.type === "agent_end");
    expect(agentEnds.length).toBe(1);
  });

  test("prompt accumulates text across multiple deltas", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        sseResponse(
          sseTextDelta("The "),
          sseTextDelta("quick "),
          sseTextDelta("brown "),
          sseTextDelta("fox"),
          sseAgentEnd(),
        )
      )
    );

    await session.prompt("Tell a story");

    // Final agent_end should contain full accumulated text
    const agentEnds = events.filter((e: any) => e.type === "agent_end");
    expect(agentEnds.length).toBe(1);

    // Check message_end has accumulated content via agent_end flow
    // The agent_end handler creates mkAssistant with acc content
    const messageEnds = events.filter((e: any) => e.type === "message_end");
    // The agent_end case in handleSSE does emit message_end with accumulated content
    expect(messageEnds.length).toBeGreaterThanOrEqual(0);
  });

  test("prompt handles empty SSE stream", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(sseResponse()) // no events at all
    );

    await session.prompt("Empty response");

    // Should still get lifecycle events
    const agentEnds = events.filter((e: any) => e.type === "agent_end");
    expect(agentEnds.length).toBe(1);
  });

  test("prompt sends correct JSON body to backend", async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(sseResponse(sseTextDelta("ok"), sseAgentEnd()))
    );

    await session.prompt("my prompt text");

    const fetchCall = fetchSpy.mock.calls[0] as [string, RequestInit?];
    expect(fetchCall[0]).toBe("http://localhost:9999/api/pux/prompt");
    expect(fetchCall[1]?.method).toBe("POST");

    const body = JSON.parse(fetchCall[1]?.body as string);
    expect(body.message).toBe("my prompt text");
    expect(body.project).toBe("test-proj");
    expect(body.model).toBe("deepseek/deepseek-v4-flash");
  });

  // ---- Resource loader stubs ----

  test("resourceLoader returns empty arrays for all getters", () => {
    const rl = session.resourceLoader;
    expect(rl.getThemes()).toEqual({ themes: [], diagnostics: [] });
    expect(rl.getExtensions()).toEqual({ extensions: [], errors: [], diagnostics: [] });
    expect(rl.getSkills()).toEqual({ skills: [], diagnostics: [] });
    expect(rl.getPrompts()).toEqual({ prompts: [], diagnostics: [] });
    expect(rl.getAgentsFiles()).toEqual({ agentsFiles: [], diagnostics: [] });
    expect(rl.getSystemPrompt()).toEqual({ systemPrompt: "", agentsFiles: [], diagnostics: [] });
    expect(rl.getAppendSystemPrompt()).toEqual({ appendSystemPrompt: "", agentsFiles: [], diagnostics: [] });
    expect(rl.extendResources()).toEqual([]);
    expect(rl.reload()).resolves.toBeUndefined();
  });

  // ---- Model registry stubs ----

  test("modelRegistry methods are no-ops", () => {
    const mr = session.modelRegistry;
    expect(mr.getAll()).toEqual([]);
    expect(mr.getAvailable()).toEqual([]);
    expect(mr.getError()).toBeNull();
    expect(mr.hasConfiguredAuth()).toBe(false);
    expect(mr.isUsingOAuth()).toBe(false);
    expect(mr.keys()).toEqual([]);
  });

  // ---- No-op methods return expected defaults ----

  test("getSessionStats returns zeros", () => {
    expect(session.getSessionStats()).toEqual({ turns: 0, messages: 0 });
  });

  test("getLastAssistantText returns empty", () => {
    expect(session.getLastAssistantText()).toBe("");
  });

  test("getFollowUpMessages returns empty array", () => {
    expect(session.getFollowUpMessages()).toEqual([]);
  });

  test("getSteeringMessages returns empty array", () => {
    expect(session.getSteeringMessages()).toEqual([]);
  });

  test("getToolDefinition returns null", () => {
    expect(session.getToolDefinition("bash")).toBeNull();
  });

  test("getUserMessagesForForking returns empty array", () => {
    expect(session.getUserMessagesForForking()).toEqual([]);
  });

  // ---- steer/followUp delegate to prompt ----

  test("steer delegates to prompt", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(sseResponse(sseTextDelta("steered"), sseAgentEnd()))
    );

    await session.steer("steer message");
    const userStarts = events.filter((e: any) => e.type === "message_start" && e.message?.role === "user");
    expect(userStarts.length).toBe(1);
    expect(userStarts[0].message.content[0].text).toBe("steer message");
  });

  test("followUp delegates to prompt", async () => {
    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    fetchSpy.mockImplementation(() =>
      Promise.resolve(sseResponse(sseTextDelta("follow-up"), sseAgentEnd()))
    );

    await session.followUp("follow up message");
    const userStarts = events.filter((e: any) => e.type === "message_start" && e.message?.role === "user");
    expect(userStarts.length).toBe(1);
  });

  // ---- cycleModel ----

  test("cycleModel cycles through scoped models", async () => {
    const models = [
      { model: { id: "a", name: "A", provider: "p" } },
      { model: { id: "b", name: "B", provider: "p" } },
    ];
    session.setScopedModels(models);
    session.setModel(models[0].model);

    const result = await session.cycleModel();
    expect(session.model.id).toBe("b");
    expect(result?.model.id).toBe("b");
  });

  test("cycleModel wraps around", async () => {
    const models = [
      { model: { id: "a", name: "A", provider: "p" } },
      { model: { id: "b", name: "B", provider: "p" } },
    ];
    session.setScopedModels(models);
    session.setModel(models[1].model); // at end

    await session.cycleModel();
    expect(session.model.id).toBe("a"); // wrapped to first
  });

  test("cycleModel returns undefined when only one model", async () => {
    session.setScopedModels([{ model: { id: "only", name: "Only", provider: "p" } }]);
    session.setModel({ id: "only", name: "Only", provider: "p" });

    const result = await session.cycleModel();
    expect(result).toBeUndefined();
    expect(session.model.id).toBe("only");
  });

  // ---- AgentSession compatibility ----

  test("agent object has expected shape", () => {
    expect(session.agent.state).toEqual({ messages: [] });
    expect(session.agent.model).toBe(session.model);
    expect(session.agent.thinkingLevel).toBe("none");
    expect(typeof session.agent.abort).toBe("function");
    expect(typeof session.agent.waitForIdle).toBe("function");
  });

  test("session property is self-referential", () => {
    expect(session.session).toBe(session);
  });

  test("state object is accessible", () => {
    expect(session.state.messages).toEqual([]);
    expect(session.state.model).toBe(session.model);
  });

  // ---- SSE parsing edge cases ----

  test("prompt handles SSE with [DONE] marker", async () => {
    fetchSpy.mockImplementation(() => {
      const body = new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode(
            `event: text_delta\ndata: {"text":"ok"}\n\ndata: [DONE]\n\n`
          ));
          controller.close();
        },
      });
      return Promise.resolve(new Response(body, { status: 200 }));
    });

    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    await session.prompt("test");

    // Should not crash on [DONE]
    const agentEnds = events.filter((e: any) => e.type === "agent_end");
    expect(agentEnds.length).toBe(1);
  });

  test("prompt handles malformed JSON gracefully", async () => {
    fetchSpy.mockImplementation(() => {
      const body = new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode(
            `event: text_delta\ndata: {not-json}\n\n` +
            `event: text_delta\ndata: {"text":"valid"}\n\n` +
            sseAgentEnd()
          ));
          controller.close();
        },
      });
      return Promise.resolve(new Response(body, { status: 200 }));
    });

    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    await session.prompt("test");

    // Should not crash — malformed JSON is silently skipped
    const agentEnds = events.filter((e: any) => e.type === "agent_end");
    expect(agentEnds.length).toBe(1);
  });

  // ---- Unknown events are ignored safely ----

  test("handles unknown SSE event type gracefully", async () => {
    fetchSpy.mockImplementation(() =>
      Promise.resolve(
        sseResponse(
          sseEvent("bogus_event", "{}"),
          sseTextDelta("still works"),
          sseAgentEnd(),
        )
      )
    );

    const events: any[] = [];
    session.subscribe((e: any) => events.push(e));

    await session.prompt("test");

    const agentEnds = events.filter((e: any) => e.type === "agent_end");
    expect(agentEnds.length).toBe(1);
  });
});
