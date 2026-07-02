// Pux site backend — a thin HTTP route layer that exposes the PiClient wire
// contract over /api/pi/** so the browser-side createPiHttpClient() can drive
// Pi in-process.
//
// Wire contract (mirrors @assistant-ui/react-pi README):
//   GET    /api/pi/threads                  → PiThreadMetadata[]
//   POST   /api/pi/threads                  → PiThreadSnapshot
//   GET    /api/pi/threads/:id              → PiThreadSnapshot
//   PATCH  /api/pi/threads/:id              → 204     { title }
//   POST   /api/pi/threads/:id/messages     → 204     { content, attachments?, streamingBehavior? }
//   POST   /api/pi/threads/:id/cancel       → 204
//   POST   /api/pi/threads/:id/clear-queue  → { steering, followUp }
//   POST   /api/pi/threads/:id/model        → 204     { provider, modelId }
//   POST   /api/pi/threads/:id/thinking     → 204     { level }
//   POST   /api/pi/threads/:id/archive      → 204
//   POST   /api/pi/threads/:id/unarchive    → 204
//   DELETE /api/pi/threads/:id              → 204
//   POST   /api/pi/threads/:id/host-ui      → 204     { response }
//   GET    /api/pi/models                   → PiModelInfo[]
//   GET    /api/pi/threads/:id/events       → SSE PiClientEvent stream
//
// Also: in production, GET * falls through to the built React assets in dist/.

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { dirname, join, resolve, extname } from "node:path";
import { fileURLToPath } from "node:url";
import type { PiSendMessageInput } from "@assistant-ui/react-pi";
import { piClient } from "./pi.ts";
import { handleFilesRoute } from "./files.ts";

const here = dirname(fileURLToPath(import.meta.url));
const siteRoot = resolve(here, "..");
const distRoot = join(siteRoot, "dist");

const PORT = Number(process.env.PUX_SITE_PORT ?? 3001);
const HOST = process.env.PUX_SITE_HOST ?? "127.0.0.1";

const MIME: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "application/javascript; charset=utf-8",
  ".mjs": "application/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".ico": "image/x-icon",
  ".woff2": "font/woff2",
};

// ─── helpers ────────────────────────────────────────────────────────────────

async function readJsonBody(req: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  for await (const c of req) chunks.push(c as Buffer);
  if (chunks.length === 0) return {};
  return JSON.parse(Buffer.concat(chunks).toString("utf-8"));
}

function sendJson(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
  });
  res.end(payload);
}

function sendNoContent(res: ServerResponse, status = 204): void {
  res.writeHead(status, { "content-length": "0" });
  res.end();
}

function sendError(res: ServerResponse, status: number, message: string): void {
  sendJson(res, status, { error: message });
}

// ─── SSE event stream ───────────────────────────────────────────────────────

async function streamEvents(req: IncomingMessage, res: ServerResponse, threadId: string): Promise<void> {
  // Snapshot-first unless caller opts out via ?snapshot=false
  const url = new URL(req.url ?? "", "http://x");
  const includeSnapshot = url.searchParams.get("snapshot") !== "false";

  res.writeHead(200, {
    "content-type": "text/event-stream; charset=utf-8",
    "cache-control": "no-store",
    connection: "keep-alive",
    "x-accel-buffering": "no", // disable nginx buffering if fronted
  });
  res.write(": stream-open\n\n");

  // Heartbeat so load balancers don't kill the idle connection mid-run.
  const heartbeat = setInterval(() => {
    try {
      res.write(": hb\n\n");
    } catch {
      // socket already gone
    }
  }, 15_000);

  // Browser disconnect → just end our response. The supervisor keeps the
  // underlying run alive — that's the react-pi contract (a dropped SSE never
  // aborts). The next reconnect will get a fresh snapshot.
  const onAbort = () => {
    clearInterval(heartbeat);
    unsub();
    try {
      res.end();
    } catch {
      // already closed
    }
  };
  req.on("close", onAbort);

  const unsub = piClient.subscribe(
    threadId,
    (event) => {
      try {
        res.write(`data: ${JSON.stringify(event)}\n\n`);
      } catch {
        // write to a closed socket — best-effort
      }
    },
    { includeSnapshot },
  );
}

// ─── router ─────────────────────────────────────────────────────────────────

type Route = {
  method: string;
  pattern: RegExp;
  paramNames: string[];
  handler: (req: IncomingMessage, res: ServerResponse, params: Record<string, string>) => Promise<void>;
};

function r(method: string, path: string, handler: Route["handler"]): Route {
  const paramNames: string[] = [];
  const re = new RegExp(
    "^" +
      path.replace(/:([a-z-]+)/g, (_, name) => {
        paramNames.push(name);
        return "([^/]+)";
      }) +
      "$",
    "i",
  );
  return { method, pattern: re, paramNames, handler };
}

const routes: Route[] = [
  r("GET", "/api/pi/threads", async (_req, res) => {
    const threads = await piClient.listThreads();
    sendJson(res, 200, threads);
  }),
  r("POST", "/api/pi/threads", async (req, res) => {
    const body = (await readJsonBody(req)) as {
      workspacePath?: string;
      title?: string;
      initialMessage?: PiSendMessageInput;
    };
    const snapshot = await piClient.createThread({
      workspacePath: body.workspacePath,
      title: body.title,
      initialMessage: body.initialMessage,
    });
    // Pi's SDK model resolution falls through to priority 4 (first auth'd
    // provider in defaultModelPerProvider iteration order) when the project
    // settings.json's defaultModel carries a provider prefix. Pin to the
    // operator's choice via PU_SITE_DEFAULT_PROVIDER/MODEL when set, then
    // re-fetch so the response carries the post-pin snapshot.
    const defaultProvider = process.env.PU_SITE_DEFAULT_PROVIDER;
    const defaultModel = process.env.PU_SITE_DEFAULT_MODEL;
    const current = snapshot.metadata.config;
    let final = snapshot;
    if (
      defaultProvider &&
      defaultModel &&
      (current?.provider !== defaultProvider || current?.modelId !== defaultModel)
    ) {
      try {
        await piClient.setModel(snapshot.metadata.id, {
          provider: defaultProvider,
          modelId: defaultModel,
        });
        final = await piClient.getThread(snapshot.metadata.id);
      } catch {
        // non-fatal — the model pick will surface as a readiness error in the UI
      }
    }
    sendJson(res, 200, final);
  }),
  r("GET", "/api/pi/threads/:id", async (_req, res, p) => {
    const snapshot = await piClient.getThread(p.id);
    sendJson(res, 200, snapshot);
  }),
  r("PATCH", "/api/pi/threads/:id", async (req, res, p) => {
    const body = (await readJsonBody(req)) as { title: string };
    await piClient.renameThread(p.id, body.title);
    sendNoContent(res);
  }),
  r("POST", "/api/pi/threads/:id/messages", async (req, res, p) => {
    // The react-pi HTTP client wraps the input: { input: PiSendMessageInput }.
    const wrapped = (await readJsonBody(req)) as { input?: PiSendMessageInput } | PiSendMessageInput;
    const body: PiSendMessageInput =
      wrapped && typeof wrapped === "object" && "input" in wrapped && wrapped.input
        ? wrapped.input
        : (wrapped as PiSendMessageInput);
    try {
      await piClient.sendMessage(p.id, body);
    } catch (err) {
      console.error(`[api] POST /api/pi/threads/${p.id}/messages →`, err);
      // The synthetic "Pi rejected the prompt before running" masks the real
      // cause. Snapshot carries it on metadata.lastError — log that too.
      try {
        const snap = await piClient.getThread(p.id);
        const lastErr = (snap as { metadata?: { lastError?: string } }).metadata?.lastError;
        if (lastErr) console.error(`[api]   metadata.lastError: ${lastErr}`);
      } catch {
        // ignore
      }
      throw err;
    }
    sendNoContent(res);
  }),
  r("POST", "/api/pi/threads/:id/cancel", async (_req, res, p) => {
    await piClient.cancelRun(p.id);
    sendNoContent(res);
  }),
  r("POST", "/api/pi/threads/:id/queue/clear", async (_req, res, p) => {
    const cleared = await piClient.clearQueue(p.id);
    sendJson(res, 200, cleared);
  }),
  // Backwards-compat alias for the older clear-queue path.
  r("POST", "/api/pi/threads/:id/clear-queue", async (_req, res, p) => {
    const cleared = await piClient.clearQueue(p.id);
    sendJson(res, 200, cleared);
  }),
  r("POST", "/api/pi/threads/:id/model", async (req, res, p) => {
    const body = (await readJsonBody(req)) as { provider: string; modelId: string };
    await piClient.setModel(p.id, body);
    sendNoContent(res);
  }),
  r("POST", "/api/pi/threads/:id/thinking", async (req, res, p) => {
    const body = (await readJsonBody(req)) as { level: string };
    await piClient.setThinkingLevel(p.id, body.level as never);
    sendNoContent(res);
  }),
  r("POST", "/api/pi/threads/:id/archive", async (_req, res, p) => {
    await piClient.archiveThread(p.id);
    sendNoContent(res);
  }),
  r("POST", "/api/pi/threads/:id/unarchive", async (_req, res, p) => {
    await piClient.unarchiveThread(p.id);
    sendNoContent(res);
  }),
  r("DELETE", "/api/pi/threads/:id", async (_req, res, p) => {
    await piClient.deleteThread(p.id);
    sendNoContent(res);
  }),
  r("POST", "/api/pi/threads/:id/host-ui", async (req, res, p) => {
    const body = (await readJsonBody(req)) as { response: unknown };
    await piClient.respondToHostUiRequest(p.id, body.response as never);
    sendNoContent(res);
  }),
  r("GET", "/api/pi/threads/:id/events", async (req, res, p) => {
    await streamEvents(req, res, p.id);
  }),
  r("GET", "/api/pi/models", async (_req, res) => {
    const models = await piClient.getAvailableModels();
    sendJson(res, 200, models);
  }),
];

// ─── static file serving (production) ───────────────────────────────────────

async function serveStatic(req: IncomingMessage, res: ServerResponse): Promise<void> {
  let urlPath = (req.url ?? "/").split("?")[0];
  if (urlPath === "/") urlPath = "/index.html";

  const filePath = join(distRoot, urlPath);
  // Prevent path traversal
  if (!filePath.startsWith(distRoot)) {
    sendError(res, 400, "bad path");
    return;
  }

  try {
    const st = await stat(filePath);
    if (st.isDirectory()) {
      return serveStatic(req, res);
    }
    const buf = await readFile(filePath);
    res.writeHead(200, {
      "content-type": MIME[extname(filePath).toLowerCase()] ?? "application/octet-stream",
      "content-length": String(buf.length),
    });
    res.end(buf);
  } catch {
    // SPA fallback — any unknown path serves index.html so client-side routing works
    try {
      const idx = await readFile(join(distRoot, "index.html"));
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      res.end(idx);
    } catch {
      sendError(res, 404, "not found");
    }
  }
}

// ─── server bootstrap ───────────────────────────────────────────────────────

const server = createServer(async (req, res) => {
  const urlRaw = req.url ?? "/";
  const urlPath = urlRaw.split("?")[0];
  const query = new URLSearchParams(urlRaw.split("?")[1] ?? "");
  const method = (req.method ?? "GET").toUpperCase();

  // CORS for dev (Vite dev server runs on :5176, us on :3001)
  res.setHeader("access-control-allow-origin", "*");
  res.setHeader("access-control-allow-methods", "GET,POST,PATCH,DELETE,OPTIONS");
  res.setHeader("access-control-allow-headers", "content-type");
  if (method === "OPTIONS") {
    sendNoContent(res, 204);
    return;
  }

  // File ops BFF — handled before the pi routes since they're a different surface.
  if (urlPath.startsWith("/api/files/")) {
    try {
      const handled = await handleFilesRoute(req, res, urlPath, query);
      if (handled) return;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[api] ${method} ${urlPath} →`, err);
      if (!res.headersSent) sendError(res, 400, msg);
      else try { res.end(); } catch {}
      return;
    }
  }

  for (const route of routes) {
    if (route.method !== method) continue;
    const match = route.pattern.exec(urlPath);
    if (!match) continue;
    const params: Record<string, string> = {};
    route.paramNames.forEach((name, i) => {
      params[name] = decodeURIComponent(match[i + 1] ?? "");
    });
    try {
      await route.handler(req, res, params);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      // Don't log normal close-of-stream
      if (!/write EPIPE|aborted/i.test(msg)) {
        console.error(`[api] ${method} ${urlPath} →`, err);
      }
      if (!res.headersSent) sendError(res, 500, msg);
      else try { res.end(); } catch {}
    }
    return;
  }

  // No API route matched — serve static (production) or 404 (dev, vite handles /)
  if (process.env.NODE_ENV === "production") {
    return serveStatic(req, res);
  }
  sendError(res, 404, `no route for ${method} ${urlPath}`);
});

server.listen(PORT, HOST, () => {
  console.log(`[pux-site] API on http://${HOST}:${PORT}  (proxy from vite :5176)`);
});

// Keep the supervisor alive on signals
process.on("SIGINT", () => process.exit(0));
process.on("SIGTERM", () => process.exit(0));

// Re-export so workers / future split-out code can grab it
export { piClient };
