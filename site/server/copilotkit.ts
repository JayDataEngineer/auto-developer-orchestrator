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
 * PuxLangGraphAgent — forces ``streamSubgraphs: false`` on every run.
 *
 * WHY: deepagents' ``CodeInterpreterMiddleware`` stores a quickjs snapshot as
 * raw ``bytes`` in the graph-state field ``_quickjs_snapshot_payload``. The
 * payload starts ``b'QFGS\x01\x00\x00\x00\xe8…'`` — byte index 8 (``\xe8``)
 * is an invalid standalone UTF-8 byte, so Pydantic's Rust serializer inside
 * Aegra throws ``PydanticSerializationError: invalid utf-8 sequence of 1 bytes
 * from index 8`` whenever that subgraph state is included in the SSE stream.
 *
 * The snapshot ONLY appears in subgraph events (``CodeInterpreterMiddleware.
 * after_agent``), never in the top-level ``values`` / ``events`` stream. So
 * disabling ``streamSubgraphs`` eliminates the invalid bytes from the wire
 * format without losing any data the CopilotKit UI actually renders (the
 * top-level messages, tool calls, and state snapshots are all unaffected).
 *
 * This is the correct server-side fix — it is transparent to every frontend
 * client and requires no per-request configuration. The root cause (raw bytes
 * in a JSON-serialized state field) is a bug in langchain-quickjs and should
 * be fixed upstream (base64-encode the snapshot), but this override makes the
 * bridge production-clean regardless.
 */
class PuxLangGraphAgent extends LangGraphAgent {
  override run(input: Parameters<LangGraphAgent["run"]>[0]) {
    return super.run({
      ...input,
      forwardedProps: {
        ...(input?.forwardedProps ?? {}),
        streamSubgraphs: false,
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

/**
 * Route handler for /api/copilotkit(.**). Delegates to the CopilotKit runtime
 * which translates the client protocol ↔ langgraph-api (Aegra). Returns true
 * if the path matched (the runtime owned the response); false to fall through.
 */
export async function handleCopilotKitRoute(
  req: IncomingMessage,
  res: ServerResponse,
  urlPath: string,
): Promise<boolean> {
  if (urlPath !== "/api/copilotkit" && !urlPath.startsWith("/api/copilotkit/")) return false;
  await ckHandler(req, res);
  return true;
}
