// CopilotKit runtime — translates the CopilotKit client protocol ↔ langgraph-api.
//
// Aegra (the langgraph-api server on :9988) serves every Pux org as a graph.
// Each org name IS a langgraph graphId. CopilotKit's built-in LangGraphAgent
// speaks STANDARD langgraph-api (/threads, /runs/stream), so NO custom /agui/
// endpoint is needed — the protocol translation is owned by the runtime's own
// adapter. This replaces the raw-proxy that 404'd against the non-existent
// /agui/<org> path.
//
// The org→graph map is derived from pux-harness/aegra.json (the single source
// of truth) at startup — adding an org there auto-registers it here on next
// restart. No code change, no second hand-maintained list (PRO-PATTERN).

import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile } from "node:fs/promises";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { CopilotRuntime, copilotRuntimeNodeHttpEndpoint } from "@copilotkit/runtime";
import { LangGraphAgent } from "@copilotkit/runtime/langgraph";

/**
 * PuxLangGraphAgent — enables ``streamSubgraphs: true`` on every run.
 *
 * Subgraph streaming surfaces the FULL activity hierarchy to CopilotKit: when
 * the supervisor delegates to a subagent via the ``task`` tool, every step the
 * subagent takes (reasoning, tool calls, intermediate messages) appears in the
 * SSE stream and renders natively in the chat UI. Without it, only the top-
 * level tool-call result is visible — the subagent's internal work is invisible.
 *
 * HISTORY: This was previously ``false`` to work around a crash in
 * ``CodeInterpreterMiddleware`` — the ``mode="thread"`` QuickJS heap snapshot
 * stored raw ``bytes`` in ``_quickjs_snapshot_payload`` (invalid UTF-8 at byte
 * 8) which crashed Aegra's Pydantic serializer. That root cause is now fixed
 * upstream in pux-harness: ``_build_interpreter`` uses ``mode="turn"`` so
 * ``after_agent`` returns ``None`` — no bytes ever enter the graph state.
 * With the bytes gone, subgraph streaming is safe and the override flips to
 * ``true`` to give CopilotKit the full subagent event hierarchy.
 */
class PuxLangGraphAgent extends LangGraphAgent {
  override run(input: Parameters<LangGraphAgent["run"]>[0]) {
    return super.run({
      ...input,
      forwardedProps: {
        ...(input?.forwardedProps ?? {}),
        streamSubgraphs: true,
      },
    });
  }
}

const AEGRA_URL = process.env.PUX_HARNESS_URL ?? "http://127.0.0.1:9988";
const LANGSMITH_KEY = process.env.LANGSMITH_API_KEY ?? "";

// Resolve aegra.json — the single source of truth for the org→graph map.
// PUX_AEGRA_JSON overrides; else PUX_PROJECT_ROOT/pux-harness/aegra.json;
// else derive from this module's own location (site/server/ → repo root).
const here = dirname(fileURLToPath(import.meta.url));
const aegraJsonPath =
  process.env.PUX_AEGRA_JSON ??
  join(process.env.PUX_PROJECT_ROOT ?? resolve(here, "..", ".."), "pux-harness", "aegra.json");

// Fallback list — ONLY used when aegra.json is unreadable (e.g. the BFF
// running outside the repo). Must match aegra.json; drift is logged loudly.
const FALLBACK_ORGS = [
  "browser-agent", "coder", "deep-research-engine", "fs-explorer",
  "game-studio", "general", "invest", "orchestrator", "social-media-pipeline",
  "telegram-agent", "twitter-agent", "video-production", "web-search",
];

async function loadOrgGraphs(): Promise<string[]> {
  try {
    const raw = await readFile(aegraJsonPath, "utf-8");
    const graphs = Object.keys(JSON.parse(raw).graphs ?? {});
    if (graphs.length > 0) {
      console.log(`[copilotkit] loaded ${graphs.length} graphs from ${aegraJsonPath}`);
      return graphs.sort();
    }
    console.warn("[copilotkit] aegra.json has no `graphs` key — falling back");
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    console.warn(`[copilotkit] could not read ${aegraJsonPath}: ${msg} — using fallback list`);
  }
  return FALLBACK_ORGS;
}

function makeAgent(graphId: string): PuxLangGraphAgent {
  return new PuxLangGraphAgent({
    deploymentUrl: AEGRA_URL,
    graphId,
    ...(LANGSMITH_KEY ? { langsmithApiKey: LANGSMITH_KEY } : {}),
  });
}

// --- build the runtime (one LangGraphAgent per org graph) ------------------
// Top-level await — ESM, target ES2022. The server cannot serve until the
// agent map is ready, so this correctly gates startup.
const orgs = await loadOrgGraphs();

const agents: Record<string, PuxLangGraphAgent> = {};
for (const name of orgs) agents[name] = makeAgent(name);

// "default" alias → "general" so CopilotKit's default-agent path works when
// the frontend doesn't specify an agent explicitly (it expects "default").
if (orgs.includes("general")) agents.default = makeAgent("general");

const runtime = new CopilotRuntime({ agents });
const agentNames = Object.keys(agents).sort().join(", ");
console.log(`[copilotkit] CopilotRuntime ready — ${Object.keys(agents).length} agents [${agentNames}]`);
console.log(`[copilotkit] forwarding to ${AEGRA_URL} via standard langgraph-api`);

// The CopilotKit node-http endpoint handler — owns the full response cycle
// (headers, body, SSE stream). We delegate (req, res) to it verbatim.
const ckHandler = copilotRuntimeNodeHttpEndpoint({
  runtime,
  endpoint: "/api/copilotkit",
});

interface AegraThread {
  thread_id: string;
  status?: string;
  created_at: string;
  updated_at: string;
  metadata?: { thread_name?: string | null };
}

/**
 * Thread list proxy — bridges CopilotKit's ``useThreads()`` to Aegra.
 *
 * CopilotKit's runtime does NOT serve ``/threads`` — thread management is a
 * CopilotKit Cloud (Intelligence Platform) feature. We self-host on LangGraph
 * (Aegra), which has its own native ``/threads`` endpoint. This proxy
 * intercepts ``GET /api/copilotkit/threads`` BEFORE the CopilotKit runtime
 * handler (which would 405), fetches from Aegra, and transforms the response
 * to the format ``useThreads()`` expects (``data.threads`` → objects with
 * ``id/name/createdAt/updatedAt/…``). This makes the native CopilotKit hook
 * work against our self-hosted backend — no Cloud subscription needed.
 */
async function handleThreadListProxy(req: IncomingMessage, res: ServerResponse): Promise<void> {
  try {
    const url = new URL(req.url ?? "", AEGRA_URL);
    const limit = url.searchParams.get("limit") ?? "50";
    const agentId = url.searchParams.get("agentId") ?? "general";
    const aegraRes = await fetch(`${AEGRA_URL}/threads/?limit=${encodeURIComponent(limit)}`);
    if (!aegraRes.ok) {
      res.writeHead(aegraRes.status, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: `Aegra /threads → ${aegraRes.status}` }));
      return;
    }
    const data = (await aegraRes.json()) as { threads?: AegraThread[] };
    const threads = (data.threads ?? []).map((t) => ({
      id: t.thread_id,
      agentId,
      name: t.metadata?.thread_name ?? "",
      archived: false,
      createdAt: t.created_at,
      updatedAt: t.updated_at,
      lastRunAt: t.updated_at,
    }));
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ threads, nextCursor: null }));
  } catch (e) {
    res.writeHead(502, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: `Thread proxy: ${e}` }));
  }
}

/**
 * Enable thread endpoints in the runtime info response.
 *
 * The CopilotKit runtime reports ``threadEndpoints.list = false`` unless it
 * detects CopilotKit Cloud (Intelligence Platform) OR a runner with
 * ``ɵsupportsLocalThreadEndpoints``. We self-host on LangGraph (Aegra), so
 * neither condition is met — but we DO serve ``/threads`` via
 * :func:`handleThreadListProxy`. This wrapper intercepts the runtime info
 * JSON response and flips ``list``/``inspect`` to ``true`` so the native
 * ``useThreads()`` hook proceeds to fetch (and hits our proxy). It touches
 * ONLY ``application/json`` responses; SSE streams (``agent/run``) pass
 * through untouched.
 */
function enableThreadEndpointsInInfo(res: ServerResponse): void {
  const origWrite = res.write.bind(res);
  const origEnd = res.end.bind(res);
  const buf: Buffer[] = [];
  let buffering = false;

  const isJson = (): boolean => {
    const ct = res.getHeader("content-type");
    return typeof ct === "string" && ct.includes("application/json");
  };

  (res as { write: typeof res.write }).write = (chunk: unknown, ...args: unknown[]): boolean => {
    if (isJson() && typeof chunk === "string" || Buffer.isBuffer(chunk)) {
      buffering = true;
      buf.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk)));
      return true;
    }
    return origWrite(chunk as Parameters<typeof origWrite>[0], ...args as Parameters<typeof origWrite>[1][]);
  };

  (res as { end: typeof res.end }).end = (chunk?: unknown, ...args: unknown[]): ServerResponse => {
    if (chunk != null && isJson()) {
      buffering = true;
      buf.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk)));
    }
    if (buffering && buf.length > 0) {
      const body = Buffer.concat(buf).toString("utf-8");
      try {
        const d = JSON.parse(body) as Record<string, unknown>;
        if (d && typeof d === "object" && "threadEndpoints" in d) {
          const te = d.threadEndpoints as Record<string, boolean>;
          te.list = true;
          te.inspect = true;
          // mutations stay false — we don't proxy create/rename/delete yet
        }
        const modified = JSON.stringify(d);
        if (!res.headersSent) {
          res.setHeader("Content-Length", Buffer.byteLength(modified));
        }
        return origEnd(modified);
      } catch {
        return origEnd(body);
      }
    }
    return origEnd(chunk as Parameters<typeof origEnd>[0], ...args as Parameters<typeof origEnd>[1][]);
  };
}

/**
 * Route handler for /api/copilotkit(.**). Delegates to the CopilotKit runtime
 * which translates the client protocol ↔ langgraph-api (Aegra). Returns true
 * if the path matched (the runtime owned the response); false to fall through.
 *
 * Two self-hosted intercepts BEFORE the CopilotKit runtime handler:
 *   - ``GET /api/copilotkit/threads`` → proxied to Aegra (thread listing)
 *   - ``POST /api/copilotkit`` (info) → ``threadEndpoints.list/inspect`` flipped
 *     to ``true`` so the native ``useThreads()`` hook works without Cloud.
 */
export async function handleCopilotKitRoute(
  req: IncomingMessage,
  res: ServerResponse,
  urlPath: string,
): Promise<boolean> {
  if (urlPath !== "/api/copilotkit" && !urlPath.startsWith("/api/copilotkit/")) return false;
  if (urlPath === "/api/copilotkit/threads" && req.method === "GET") {
    await handleThreadListProxy(req, res);
    return true;
  }
  // Wrap res for the main endpoint so the info response enables thread endpoints.
  if (urlPath === "/api/copilotkit" && req.method === "POST") {
    enableThreadEndpointsInInfo(res);
  }
  await ckHandler(req, res);
  return true;
}
