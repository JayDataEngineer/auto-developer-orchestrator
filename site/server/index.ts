// Pux site backend — a thin HTTP route layer for file ops, sandbox lifecycle,
// terminal PTY, VNC reverse-proxy, CopilotKit runtime, and thread inspection.
//
// The Agent Protocol harness runs separately (default http://127.0.0.1:9988).
// The frontend talks ONLY to this BFF — it never reaches Aegra directly:
//
//   CopilotKit: /api/copilotkit(.**) → CopilotRuntime → Aegra langgraph-api
//   Thread:     /api/thread/:id      → Aegra /threads/:id (state for metrics)
//   File ops:   /api/files/**
//   Sandbox:    /api/sandbox/**
//   Terminal:   /api/terminal/ws (WebSocket upgrade)
//   VNC:        /api/sandbox/vnc/** (HTTP + WebSocket upgrade)
//
// In production, GET * falls through to the built React assets in dist/.

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { dirname, join, resolve, extname } from "node:path";
import { fileURLToPath } from "node:url";
import { handleFilesRoute } from "./files.ts";
import { handleSandboxRoute } from "./sandbox.ts";
import { handleVncHttpRoute, attachVncUpgrade } from "./vnc-proxy.ts";
import { attachTerminalUpgrade } from "./terminal.ts";
import { handleCopilotKitRoute } from "./copilotkit.ts";

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

  // CopilotKit runtime — translates client protocol ↔ Aegra langgraph-api.
  if (urlPath === "/api/copilotkit" || urlPath.startsWith("/api/copilotkit/")) {
    try {
      const handled = await handleCopilotKitRoute(req, res, urlPath);
      if (handled) return;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[api] ${method} ${urlPath} →`, err);
      if (!res.headersSent) sendError(res, 502, msg);
      else try { res.end(); } catch {}
      return;
    }
  }

  // Thread list proxy — single-ingress-point passthrough to Aegra's
  // LangGraph thread search. Drives the site's thread-history sidebar:
  // GET /api/threads?limit=50 → GET <aegra>/threads/?limit=50.
  if (urlPath === "/api/threads" && method === "GET") {
    try {
      const aegraUrl = process.env.PUX_HARNESS_URL ?? "http://127.0.0.1:9988";
      const upstream = await fetch(`${aegraUrl}/threads/?${query.toString()}`);
      const body = await upstream.text();
      res.writeHead(upstream.status, {
        "content-type":
          upstream.headers.get("content-type") ?? "application/json",
        "cache-control": "no-store",
      });
      res.end(body);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (!res.headersSent) sendError(res, 502, msg);
      else try { res.end(); } catch {}
    }
    return;
  }

  // Thread-state proxy — single-ingress-point passthrough to Aegra.
  // The frontend metrics footer reads thread state (values, status, next)
  // from this endpoint; routing through the BFF avoids a direct Aegra
  // dependency in the browser.
  if (urlPath.startsWith("/api/thread/") && method === "GET") {
    const threadId = decodeURIComponent(urlPath.slice("/api/thread/".length));
    if (threadId) {
      try {
        const aegraUrl = process.env.PUX_HARNESS_URL ?? "http://127.0.0.1:9988";
        const upstream = await fetch(
          `${aegraUrl}/threads/${encodeURIComponent(threadId)}`,
        );
        const body = await upstream.text();
        res.writeHead(upstream.status, {
          "content-type":
            upstream.headers.get("content-type") ?? "application/json",
          "cache-control": "no-store",
        });
        res.end(body);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (!res.headersSent) sendError(res, 502, msg);
        else try { res.end(); } catch {}
      }
      return;
    }
  }

  // File ops BFF — handled before the sandbox routes since they're a different surface.
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

  // Sandbox lifecycle BFF.
  if (urlPath === "/api/sandbox" || urlPath.startsWith("/api/sandbox/")) {
    // VNC reverse-proxy lives at /api/sandbox/vnc/** — try it first.
    if (urlPath.startsWith("/api/sandbox/vnc")) {
      try {
        const handled = await handleVncHttpRoute(req, res, urlPath);
        if (handled) return;
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        console.error(`[api] ${method} ${urlPath} →`, err);
        if (!res.headersSent) sendError(res, 502, msg);
        else try { res.end(); } catch {}
        return;
      }
    }
    try {
      const handled = await handleSandboxRoute(req, res, urlPath);
      if (handled) return;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error(`[api] ${method} ${urlPath} →`, err);
      if (!res.headersSent) sendError(res, 500, msg);
      else try { res.end(); } catch {}
      return;
    }
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

// WebSocket upgrade routes — terminal PTY + VNC reverse proxy.
attachTerminalUpgrade(server);
attachVncUpgrade(server);

// Keep the supervisor alive on signals
process.on("SIGINT", () => process.exit(0));
process.on("SIGTERM", () => process.exit(0));
