/**
 * E2E tests for the full TUI pipeline — backend → SSE → PuxAgentSession → events.
 * Starts ONE in-process HTTP server, tests all scenarios against it.
 *
 * Run: bun test src/__tests__/e2e.test.ts
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { PuxAgentSession } from "../core/pux-agent-session.js";

let serverUrl: string;
let capturedRequests: { body: any; url: string; method: string }[] = [];

// SSE helpers
function evt(event: string, data: any): string {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}
function textDelta(text: string): string { return evt("text_delta", { text }); }
function thinkingDelta(text: string): string { return evt("thinking_delta", { text }); }
function toolStart(name: string, toolId: string, args: any): string {
  return evt("tool_execution_start", { toolName: name, toolId, args });
}
function toolEnd(toolId: string, toolName: string, result?: string, error?: string): string {
  const p: any = { toolId, toolName };
  if (result !== undefined) p.result = result;
  if (error) p.error = error;
  return evt("tool_execution_end", p);
}
function agentEnd(input?: number, output?: number): string {
  return evt("agent_end", { input: input ?? 100, output: output ?? 50 });
}
function errEvent(msg: string): string {
  return evt("error", { error: msg });
}
function sseOK(...events: string[]): Response {
  return new Response(events.join(""), {
    headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" },
  });
}

beforeAll(async () => {
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url);
      if (url.pathname.startsWith("/api/pux/")) {
        let b: any = null;
        try { b = await req.clone().json(); } catch {}
        capturedRequests.push({ body: b, url: url.pathname, method: req.method });
      }
      if (url.pathname === "/api/health") return Response.json({ status: "ok" });
      if (url.pathname === "/api/pux/models") return Response.json([
        { id: "test/a", name: "A", provider: "test" },
        { id: "test/b", name: "B", provider: "test" },
      ]);
      if (url.pathname === "/api/pux/model") return new Response("ok");
      if (url.pathname !== "/api/pux/prompt") return new Response("not found", { status: 404 });

      const body: any = await req.json();
      const msg: string = body.message || "";

      if (msg === "e2e:simple") return sseOK(textDelta("Hello from e2e test!"), agentEnd());
      if (msg === "e2e:thinking") return sseOK(
        thinkingDelta("I need to analyze this..."),
        thinkingDelta(" Let me check."),
        textDelta("Here is my analysis:"),
        textDelta(" Everything looks good."),
        agentEnd(250, 120),
      );
      if (msg === "e2e:bash-tool") return sseOK(
        textDelta("Let me list the files."),
        toolStart("bash", "call_001", { cmd: "ls -la" }),
        toolEnd("call_001", "bash", "total 24\ndrwxr-xr-x"),
        textDelta(" Found the files."),
        agentEnd(180, 90),
      );
      if (msg === "e2e:multi-tool") return sseOK(
        thinkingDelta("Multi-step task."),
        toolStart("read_file", "call_01", { path: "main.go" }),
        toolEnd("call_01", "read_file", "package main"),
        toolStart("bash", "call_02", { cmd: "go build" }),
        toolEnd("call_02", "bash", "Build succeeded"),
        textDelta("All steps completed."),
        agentEnd(350, 150),
      );
      if (msg === "e2e:tool-error") return sseOK(
        textDelta("Attempting browser nav..."),
        toolStart("browse", "call_err", { url: "http://invalid" }),
        toolEnd("call_err", "browse", undefined, "Connection timeout after 30s"),
        textDelta(" Navigation failed."),
        agentEnd(200, 100),
      );
      if (msg === "e2e:large-response") {
        const big = "A".repeat(5000);
        return sseOK(textDelta(big), agentEnd(500, 250));
      }
      if (msg === "e2e:empty") return sseOK(agentEnd());
      if (msg === "e2e:abort") {
        return new Response(new ReadableStream({
          async start(ctrl) {
            ctrl.enqueue(new TextEncoder().encode(textDelta("starting...")));
            await new Promise(r => setTimeout(r, 5000));
            ctrl.enqueue(new TextEncoder().encode(textDelta("too slow")));
            ctrl.close();
          },
        }), { headers: { "Content-Type": "text/event-stream" } });
      }
      if (msg === "e2e:backend-error") return sseOK(
        textDelta("Processing..."),
        errEvent("Internal server error: nil pointer dereference"),
      );
      return sseOK(textDelta("ok"), agentEnd());
    },
  });
  serverUrl = `http://localhost:${server.port}`;
  await new Promise(r => setTimeout(r, 100));
});

afterAll(() => { capturedRequests = []; });

// Stubs
function stubSettings(): any {
  return { get: () => undefined, set: () => Promise.resolve(), getAll: () => ({}),
    getCwd: () => "/fake/e2e", getAgentDir: () => "/tmp/.pux/e2e", getSettingsDir: () => "/tmp/.pux/e2e" };
}
function stubSessions(): any {
  return { cwd: "/fake/e2e", getCwd: () => "/fake/e2e", sessions: [], persist: () => {}, addBranch: () => {} };
}
async function collect(s: PuxAgentSession, msg: string): Promise<any[]> {
  const events: any[] = [];
  const unsub = s.subscribe((e: any) => events.push(e));
  await s.prompt(msg);
  unsub();
  return events;
}

// Tests
describe("E2E: Simple response", () => {
  test("receives text deltas and agent_end", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:simple");
    expect(e.filter((x: any) => x.type === "agent_end").length).toBe(1);
    const re = e.find((x: any) => x.type === "message_end");
    const tc = re.message.content.find((c: any) => c.type === "text");
    expect(tc.text).toBe("Hello from e2e test!");
  });
});

describe("E2E: Thinking + text", () => {
  test("accumulates thinking and text", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:thinking");
    const re = e.find((x: any) => x.type === "message_end");
    expect(re.message.content.some((c: any) => c.type === "thinking")).toBe(true);
    expect(re.message.content.some((c: any) => c.type === "text")).toBe(true);
  });
});

describe("E2E: Tool calls", () => {
  test("bash tool with result", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:bash-tool");
    const tes = e.filter((x: any) => x.type === "tool_execution_end");
    expect(tes.length).toBe(1);
    expect(tes[0].toolName).toBe("bash");
    expect(tes[0].isError).toBe(false);
    expect(tes[0].result).toContain("total");
  });
  test("multiple tools in one turn", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:multi-tool");
    expect(e.filter((x: any) => x.type === "tool_execution_end").length).toBe(2);
  });
  test("tool execution with error", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:tool-error");
    const tes = e.filter((x: any) => x.type === "tool_execution_end");
    expect(tes.length).toBe(1);
    expect(tes[0].isError).toBe(true);
    expect(tes[0].result).toContain("timeout");
  });
});

describe("E2E: Edge cases", () => {
  test("empty SSE stream", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:empty");
    expect(e.filter((x: any) => x.type === "agent_end").length).toBe(1);
  });
  test("large text response", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:large-response");
    const re = e.find((x: any) => x.type === "message_end");
    expect(re.message.content.find((c: any) => c.type === "text").text.length).toBe(5000);
  });
  test("backend error event", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const e = await collect(s, "e2e:backend-error");
    const re = e.find((x: any) => x.type === "message_end");
    expect(re.message.stopReason).toBe("error");
    expect(re.message.errorMessage).toContain("nil pointer");
  });
  test("abort during streaming", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    const events: any[] = [];
    s.subscribe((e: any) => events.push(e));
    const p = s.prompt("e2e:abort");
    await new Promise(r => setTimeout(r, 200));
    s.abort();
    await p;
    expect(events.filter((x: any) => x.type === "agent_end").length).toBe(1);
  });
  test("multiple sequential prompts", async () => {
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "e2e", "test/a");
    expect((await collect(s, "e2e:simple")).filter((x: any) => x.type === "agent_end").length).toBe(1);
    expect((await collect(s, "e2e:bash-tool")).filter((x: any) => x.type === "tool_execution_end").length).toBe(1);
    expect((await collect(s, "e2e:thinking")).filter((x: any) => x.type === "agent_end").length).toBe(1);
    expect((await collect(s, "e2e:empty")).filter((x: any) => x.type === "agent_end").length).toBe(1);
  });
});

describe("E2E: HTTP contract", () => {
  test("sends correct request body fields", async () => {
    capturedRequests = [];
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "my-project", "claude/sonnet");
    s.setThinkingLevel("high");
    await s.prompt("test request body");
    const pr = capturedRequests.find((r: any) => r.url === "/api/pux/prompt");
    expect(pr).toBeTruthy();
    expect(pr!.body.message).toBe("test request body");
    expect(pr!.body.project).toBe("my-project");
    // agentId is omitted when empty (not "default")
    expect(pr!.body.agentId).toBeUndefined();
    expect(pr!.body.model).toBe("claude/sonnet");
    expect(pr!.body.thinkingLevel).toBe("high");
  });
  test("model switching sends PUT to backend", async () => {
    capturedRequests = [];
    const s = new PuxAgentSession(stubSettings(), stubSessions(), serverUrl, "proj", "old-model");
    s.setModel({ id: "new-model", provider: "test", name: "New" });
    await new Promise(r => setTimeout(r, 500));
    const mp = capturedRequests.find((r: any) => r.url === "/api/pux/model" && r.method === "PUT");
    expect(mp).toBeTruthy();
    expect(mp!.body.provider).toBe("test");
  });
});
