import { test, expect, beforeAll, afterAll, describe } from "bun:test";
import React from "react";
import { render } from "ink-testing-library";
import App from "./app";

/**
 * Integration tests using ink-testing-library + mock Bun HTTP server.
 * Tests: render, typing, Enter→send, Shift+Enter, Ctrl+J, slash commands, SSE streaming.
 */

let mockPort = 0;
let mockServer: ReturnType<typeof Bun.serve> | null = null;

// ── Mock backend ────────────────────────────────────────────

beforeAll(() => {
  mockServer = Bun.serve({
    port: 0,
    async fetch(req) {
      const url = new URL(req.url);
      const path = url.pathname;

      if (path === "/api/health") {
        return new Response(JSON.stringify({
          version: "1.0.0",
          llm: "healthy",
          sandbox: "available",
        }), { headers: { "content-type": "application/json" } });
      }

      if (path === "/api/pux/conversations") {
        return new Response(JSON.stringify([
          { project: "test", agentId: "agent-test", title: "Test Chat", lastMessage: "hello", messageCount: 2, lastAt: new Date().toISOString() }
        ]), { headers: { "content-type": "application/json" } });
      }

      if (path === "/api/pux/history") {
        return new Response(JSON.stringify([
          { role: "user", content: "hello" },
        ]), { headers: { "content-type": "application/json" } });
      }

      // SSE prompt — must use correct Go backend SSE event types
      if (path === "/api/pux/prompt" && req.method === "POST") {
        const body = await req.text();
        const json = JSON.parse(body);

        // Encode SSE events as the Go backend does
        const lines = [
          `event: agent_spawned\ndata: ${JSON.stringify({ agentId: json.agentId || "agent-test" })}\n`,
          `event: thinking_delta\ndata: ${JSON.stringify({ text: "Let me think about this..." })}\n`,
          `event: text_delta\ndata: ${JSON.stringify({ text: "Hello! How can I help you?" })}\n`,
          `event: agent_end\ndata: ${JSON.stringify({ input: 30, output: 50 })}\n`,
          `event: done\ndata: ${JSON.stringify({})}\n\n`,
        ];

        const stream = new ReadableStream({
          async pull(controller) {
            for (const line of lines) {
              controller.enqueue(new TextEncoder().encode(line));
              await Bun.sleep(30); // simulate network latency
            }
            controller.close();
          },
        });

        return new Response(stream, {
          headers: {
            "content-type": "text/event-stream",
            "cache-control": "no-cache",
            connection: "keep-alive",
          },
        });
      }

      // Artifacts — returns array directly
      if (path === "/api/pux/artifacts") {
        return new Response(JSON.stringify([]), {
          headers: { "content-type": "application/json" },
        });
      }

      // Scheduler
      if (path === "/api/scheduler/" || path.startsWith("/api/scheduler")) {
        return new Response(JSON.stringify({ jobs: [] }), {
          headers: { "content-type": "application/json" },
        });
      }

      return new Response("not found", { status: 404 });
    },
  });
  mockPort = mockServer.port ?? 0;
  console.log(`\nMock server on port ${mockPort}`);
});

afterAll(() => {
  mockServer?.stop(true);
});

// ── Helpers ─────────────────────────────────────────────────

function mount() {
  return render(
    React.createElement(App, {
      serverUrl: `http://localhost:${mockPort}`,
      project: "test"
    })
  );
}

function lastText(result: ReturnType<typeof mount>): string {
  return result.lastFrame() ?? "";
}

// ── Tests ───────────────────────────────────────────────────

describe("App integration", () => {
  test("renders without crashing", () => {
    const { unmount } = mount();
    expect(true).toBe(true);
    unmount();
  });

  test("shows Activity sidebar on initial render", () => {
    const result = mount();
    expect(lastText(result)).toContain("Activity");
    result.unmount();
  });

  test("shows input placeholder", () => {
    const result = mount();
    expect(lastText(result)).toContain("Type a message");
    result.unmount();
  });

  test("shows pux branding in scheduler view", async () => {
    const result = mount();
    await Bun.sleep(100);

    // Navigate to scheduler
    for (const ch of "/jobs") {
      result.stdin.write(ch);
      await Bun.sleep(20);
    }
    await Bun.sleep(50);
    result.stdin.write("\r");
    await Bun.sleep(150);

    const text = lastText(result);
    // pux branding shows in the Sched header: "pux Scheduler"
    expect(text).toContain("pux Scheduler");
    result.unmount();
  });

  test("shows health footer with version", async () => {
    const result = mount();
    // Health is fetched async — wait for it
    await Bun.sleep(300);
    const text = lastText(result);
    expect(text).toContain("v1.0.0");
    result.unmount();
  });

  test("typing characters appears in output", async () => {
    const result = mount();
    await Bun.sleep(100);
    for (const ch of "hello") {
      result.stdin.write(ch);
      await Bun.sleep(30);
    }
    await Bun.sleep(100);
    expect(lastText(result)).toContain("hello");
    result.unmount();
  });

  test("Enter sends message and receives SSE response", async () => {
    const result = mount();
    await Bun.sleep(100);

    result.stdin.write("test message");
    await Bun.sleep(50);
    result.stdin.write("\r");
    await Bun.sleep(500); // let SSE stream complete

    const text = lastText(result);
    expect(text).toContain("test message");
    expect(text).toContain("Hello! How can I help you?");
    result.unmount();
  });

  test("Ctrl+J (\\n) inserts a newline", async () => {
    const result = mount();
    await Bun.sleep(100);

    result.stdin.write("line1");
    await Bun.sleep(50);
    result.stdin.write("\n");
    await Bun.sleep(50);
    result.stdin.write("line2");
    await Bun.sleep(100);

    const text = lastText(result);
    expect(text).toContain("line1");
    expect(text).toContain("line2");
    result.unmount();
  });

  test("Shift+Enter CSI-u \\x1b[13;2u inserts newline", async () => {
    const result = mount();
    await Bun.sleep(100);

    result.stdin.write("before");
    await Bun.sleep(50);
    result.stdin.write("\x1b[13;2u");
    await Bun.sleep(50);
    result.stdin.write("after");
    await Bun.sleep(100);

    const text = lastText(result);
    expect(text).toContain("before");
    expect(text).toContain("after");
    result.unmount();
  });

  test("Shift+Enter xterm \\x1b[27;2;13~ inserts newline", async () => {
    const result = mount();
    await Bun.sleep(100);

    result.stdin.write("xterm");
    await Bun.sleep(50);
    result.stdin.write("\x1b[27;2;13~");
    await Bun.sleep(50);
    result.stdin.write("test");
    await Bun.sleep(100);

    const text = lastText(result);
    expect(text).toContain("xterm");
    expect(text).toContain("test");
    result.unmount();
  });

  test("Shift+Enter xterm fmt2 \\x1b[13;2~ inserts newline", async () => {
    const result = mount();
    await Bun.sleep(100);

    result.stdin.write("fmt2");
    await Bun.sleep(50);
    result.stdin.write("\x1b[13;2~");
    await Bun.sleep(50);
    result.stdin.write("end");
    await Bun.sleep(100);

    const text = lastText(result);
    expect(text).toContain("fmt2");
    expect(text).toContain("end");
    result.unmount();
  });

  test("/help slash command shows keyboard reference", async () => {
    const result = mount();
    await Bun.sleep(100);

    for (const ch of "/help") {
      result.stdin.write(ch);
      await Bun.sleep(20);
    }
    await Bun.sleep(50);
    result.stdin.write("\r");
    await Bun.sleep(150);

    const text = lastText(result);
    expect(text).toContain("Keyboard & Slash Commands");
    result.unmount();
  });

  test("/scheduler slash command shows scheduler view", async () => {
    const result = mount();
    await Bun.sleep(100);

    for (const ch of "/jobs") {
      result.stdin.write(ch);
      await Bun.sleep(20);
    }
    await Bun.sleep(50);
    result.stdin.write("\r");
    await Bun.sleep(150);

    const text = lastText(result);
    expect(text).toContain("0 jobs");
    result.unmount();
  });

  test("/artifacts shows artifact view (empty state)", async () => {
    const result = mount();
    await Bun.sleep(100);

    for (const ch of "/artifacts") {
      result.stdin.write(ch);
      await Bun.sleep(20);
    }
    await Bun.sleep(50);
    result.stdin.write("\r");
    await Bun.sleep(300);

    const text = lastText(result);
    expect(text).toContain("No artifacts");
    result.unmount();
  });
});
