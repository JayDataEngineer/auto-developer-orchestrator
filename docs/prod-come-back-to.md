# Prod build — come-back-to list

Status as of **2026-07-08**, written before the Hermes→dev-bot prod E2E build.
These are the deferred items the upstream-contract pivot left open, parked so the
prod path (phone→Hermes→ACP/Agent-Protocol→dev-bot) could ship first. Each is
scoped, sized, and tagged with its tracking task.

The governing context: `[[upstream-protocol-pivot]]` — bind ALL protocol surfaces
UPSTREAM. P1+P2+P2.5 SHIPPED+PROVEN (keystone + real `compile_org` org graphs both
serve upstream via `langgraph dev`; full `langgraph_sdk` surface green). The items
below are P3/P4 + known defects.

---

## P3 — Retire the hand-rolled `server.py` REST lane  (task #15)

**Why it's deferred:** the prod build reuses the EXISTING `pux serve` (uvicorn +
FastAPI `server.py`) as the Agent Protocol HTTP backend. It works today and is
proven. Retiring it is a clean-up, not a blocker.

**The work:**
- `pux_harness/runtime/server.py` reimplements the Agent Protocol REST surface
  (assistants/threads/runs/store) that the UPSTREAM `langgraph-api` runtime (launched
  by `langgraph dev` / `langgraph build`) already serves — proven by
  `scripts/upstream_keystone.py` driving the full `langgraph_sdk` surface green.
- Retire = make `pux serve` a THIN launcher of the upstream runtime
  (`langgraph dev --config langgraph.json` style) instead of the hand-rolled app,
  OR delete it and point the MCP wrapper + AG-UI mount straight at the upstream ASGI app.
- MUST land a contract test first that pins the exact HTTP surface (`/threads`,
  `/runs/stream`, `/store/items`, SSE event shapes) so the cutover is verifiable
  (no-legacy-left-behind: the OLD form becomes a permanent contract failure only
  AFTER the upstream form is proven equivalent on every endpoint).

**Sizing:** medium. The risk is the SSE event-shape parity + the
shared-`BaseStore` seam (already fixed — ONE store on `app.state.base_store`, see
`[[store-rest-surface]]`).

---

## P4 — Rework the Export lane → MDA project structure  (task #16, feeds `[[plan-dynamic-tools-and-export]]`)

**Why it's deferred:** export already ships (the export.py data/ leak is a permanent
contract — `[[export-data-credential-leak-fixed]]`). The rework changes the TARGET
shape, not whether export works.

**The work (canonical design lives in `docs/dynamic-tools-and-packaging.md`):**
- Export an org as a **Managed Deep Agents project** (`[[managed-deep-agents]]`):
  `agent.py` emitting `define_deep_agent(...)` + `instructions.md` + `skills/` +
  `tools/` + `middleware/` + `schedules/` + `sandbox/` + `connectors/mcp.*` + `.env`.
- Consumer runs `mda dev` (local, = `langgraph-cli`, same wire format as our AP lane)
  or `mda deploy` (hosted beta). Zero lock-in: identical to what `pux serve` serves.
- Level (c) Dynamic Tools (agent-authored `lib/`, prunable, graduates→sandbox) is the
  other half of P4 — reuse-first: `oras` (OCI), gitleaks/ruff/uv (in-repo),
  APS-*shaped* manifest (v0.1, not dependent), `.agent` ruled out (patent).
- Own only thin glue (`PACK_HOOK_REGISTRY`).

**Sizing:** large. Blocked on nothing for the local `mda dev` path (package is public);
hosted `mda deploy` waits on private-beta access.

---

## AG-UI defects #5 / #6 / #7  (task #9) — ✅ RESOLVED 2026-07-08

**Resolved by verify-against-current-version.** Re-derived the defects by
driving the LIVE AG-UI lane against the current stack (ag-ui-langgraph `0.0.42`
+ the prod model layer's default `glm-5.2`). All 4 `tests/server/test_agui_live.py`
tests green (29.4s), including the ask_user interrupt→resume round-trip.

The headline defect — "incremental `TEXT_MESSAGE_*` streaming broken, deltas
pass through as `RAW`" — is **moot**: it was MiMo-specific (MiMo's langgraph
deltas didn't map); `glm-5.2` streams a proper `TEXT_MESSAGE_START`→`CONTENT`→
`END` lifecycle. The `RAW` events that still appear are benign graph-lifecycle
passthrough (`on_chain_start`, etc.), not untranslated message deltas.

**no-legacy-left-behind:** `test_agui_general_streams_text` previously declined
to assert incremental streaming (its docstring said "out of scope"). Now that
streaming works it's a **permanent contract** — the test asserts the
`TEXT_MESSAGE_*` lifecycle and prefers streamed deltas over the
`MESSAGES_SNAPSHOT` fallback, so a RAW-passthrough regression fails loud. A
pux-side event-rewriter was correctly rejected (`[[rely-on-upstream]]` — it
would fight the upstream adapter; the real fix was the model/version advance).

**Why it was deferred (context):** AG-UI (the web SSE lane, CopilotKit
`/agui/{org}`) is NOT on the prod path — Hermes speaks MCP, not AG-UI. The
phone→Hermes flow never touches AG-UI.

---

## k3s build artifacts

**Why deferred:** prod runs on the local docker lane (`pux serve` + `pux mcp` on the
ubuntu-desktop host, dev-bot containerized). The k3s path (the eventual Agent Protocol
production target per the two-protocol split: ACP=local, AP=k3s) needs its build pipeline.

**The work:**
- `langgraph build` produces the OCI image for the multi-org `langgraph.json`
  (every org = one graph_id = one assistant). k3s manifest + ingress + the shared
  sqlite→Postgres cutover for real multi-replica.
- The model layer is now k3s-ready (multi-provider: glm-5.2 via ZAI anthropic-secret,
  mimo via OpenRouter secret — two separate k8s Secrets, not one).
- `[[aegra-verified]]` Gate 2a/2b (compile + compose smoke) already green; the k3s
  cutover is the packaging, not the graph.

**Sizing:** medium. Postgres-only for k3s (localhost stays ACP/sqlite).

---

## Prod-path decisions baked in this build (for reference, NOT come-back-to)

These are DONE and should NOT be revisited unless they break:
- **Two-protocol split:** ACP=local (`pux acp` / deepagents-acp stdio),
  Agent Protocol=`pux serve` HTTP. Both share ONE sqlite.
- **dev-bot container:** `sandbox.tier: bridged` (host-net → reaches cloud over
  Tailscale) + `workspace.mounts` (`/home/ubuntu`→`/host`) + `sandbox.deps.apt:
  [openssh-client]`. Config-only, no harness code change.
- **Hermes→dev-bot seam = MCP:** `pux mcp` (FastMCP SSE :9987) wraps the Agent
  Protocol HTTP. Hermes config adds an `mcp_servers:` entry (HTTP `url:`).
- **GLM-5.2 prod = ZAI Anthropic-compat** (`ANTHROPIC_AUTH_TOKEN`); mimo via OpenRouter.
  Multi-provider model layer shipped (submodule `56bdea1`).
