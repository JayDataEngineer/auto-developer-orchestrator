# Aegra — pux's free prod Agent Protocol runtime

**Status:** 2026-07-09. Phase A (local smoke) SHIPPED+PROVEN (submodule `e017ea6`).
Phase B (prod cutover artifacts — this doc + the three `scripts/aegra-*` files)
SHIPPED. Phase C (reversible prod cutover + E2E) + Phase D (retire `server.py`)
pending. Tracking task #28; this resolves the deferred P3 in
`docs/prod-come-back-to.md`.

## What + why

**Aegra** (`github.com/aegra/aegra`, OSS Apache-2.0) is a self-hosted
**LangGraph-Platform / langgraph-api drop-in** — FastAPI + Postgres + optional
Redis, exposing the identical `threads/runs/stream/store` wire format as
`langgraph_sdk`. It is pux's **free prod Agent Protocol (HTTP REST) runtime**,
replacing the hand-rolled `pux_harness/runtime/server.py`.

This resolves the **langgraph-api license gate** (see
`[[langgraph-api-license-gate]]`): the upstream `langchain/langgraph-api` prod
image is LangGraph Platform **commercial** — it fails the license check in BOTH
licensed+local variants without a paid LangGraph Cloud key, and the free
LangSmith tier lacks Cloud access. Only `langgraph dev` (in-memory) is
keyless-free. Aegra gives us the prod Postgres-backed runtime for free.

**Aegra ≠ Agent Client Protocol.** Aegra is the Agent *Protocol* (HTTP REST)
gateway. ACP (local stdio) stays as `deepagents-acp` / `pux acp` — untouched.
Governing context: `[[upstream-protocol-pivot]]`, `[[rely-on-upstream]]`.

## Topology (HOST-API, path-transparent)

Identical binding + ports to `server.py`, so consumers (Hermes `/events`+stream,
dev-bot, MCP) need **no URL change** on cutover:

| Process | Bind | Port | Notes |
|---|---|---|---|
| `aegra serve` (AP HTTP) | Tailscale `100.99.57.110` | `9988` | prod mode, no reload; reads `pux-harness/aegra.json` |
| `pux mcp` (FastMCP SSE) | Tailscale `100.99.57.110` | `9987` | unchanged; proxies the Aegra backend |
| postgres sidecar | `127.0.0.1` ONLY | `5432` | `pgvector/pgvector:pg18` |
| redis sidecar | `127.0.0.1` ONLY | `6379` | `redis:7-alpine`, run broker |

DBs bind `127.0.0.1` ONLY — never exposed to Tailscale/LAN; the host API is the
sole client. Matches the existing prod topology (host process, Tailscale-bound)
for a reversible cutover. The full-containerized form (`aegra up` — API in
Docker too) remains the k3s-ready target.

## Artifacts (phase B)

- **`scripts/aegra-prod.compose.yml`** — postgres + redis sidecars only.
  **Gotcha:** pg18 uses `pg_ctlcluster` and manages data in a version subdir, so
  the volume mounts at the PARENT `/var/lib/postgresql` (matching Aegra's own
  generated compose), NOT `/var/lib/postgresql/data` (which pg18 refuses to
  init with `Error: … appears to be PostgreSQL data … (unused mount/volume)`).
- **`scripts/aegra-prod.env.template`** — runtime env template (NO secrets).
  Secrets come from the project `.env`, merged in by the launcher. Sets
  `AEGRA_CONFIG=aegra.json`, `RUN_MIGRATIONS_ON_STARTUP=true`, `POSTGRES_*`,
  `REDIS_*`. **`PUX_UPSTREAM_GRAPH` is intentionally UNSET** (runtime =
  `build_graph` + Docker specialists + real model).
- **`scripts/start_pux_aegra.sh`** — prod launcher (mirrors
  `start_pux_prod.sh`). Sources the env template + `.env`, unsets
  `PUX_UPSTREAM_GRAPH`, brings up sidecars, waits on postgres, starts `aegra
  serve` + `pux mcp`, waits on `/events/health`. `stop` subcommand kills serve +
  mcp (leaves sidecars). Idempotent.
- **`pux-harness/aegra.json`** (phase A, submodule `e017ea6`) — Aegra
  auto-discovers `aegra.json` with PRIORITY over `langgraph.json`. Its graph
  loader is FILE-PATH-only (no module-import branch), so graph specs are
  `./pux_harness/runtime/upstream.py:graph__<slug>` — pointing at the same
  `upstream.py` whose top-level factory-registration loop exposes every
  `graph__<slug>` attr. Downstream config accommodation only; we do NOT fork
  Aegra. `http.app` mounts `pux_harness.runtime.custom_app:app` (EventBus).

## Cutover procedure

**Start (server.py → Aegra on :9988):**

```bash
# 1. stop the old stack (server.py + mcp on :9988/:9987)
scripts/start_pux_prod.sh stop   # or: pkill -f '[p]ux_harness serve'
# 2. bring up Aegra on the SAME :9988 + sidecars + mcp
scripts/start_pux_aegra.sh
```

**Rollback (Aegra → server.py):**

```bash
scripts/start_pux_aegra.sh stop        # kills aegra serve + mcp; sidecars left running
scripts/start_pux_prod.sh              # server.py back on :9988
# (optional) docker compose -f scripts/aegra-prod.compose.yml down   # remove DBs
```

**Verify after cutover:** `scripts/aegra_smoke.py` (5-surface live proof —
assistants.search / threads.create / runs.stream / store put+get /
events.health) plus the prod E2E (phone→Hermes→MCP→dev-bot).

## Known deltas vs `server.py` (accept + document, do not paper over)

1. **Persistence sqlite → Postgres.** Existing ephemeral threads do NOT migrate
   (acceptable — Hermes/dev-bot carry per-conversation state; no durable threads
   relied on). `[[no-legacy-left-behind]]` does not apply: server.py is being
   retired, not migrated alongside; its thread store is ephemeral-by-design.
2. **No prepare/warmup hook.** Aegra has no `prepare()`/warmup job seam, so
   `warmup_browser` / `warmup_webhook` (Chrome cold-start absorption) do not
   fire. Browser orgs absorb the cold-start on first run instead of pre-warm.
   KNOWN LOSS on cutover (first browser run is slower); not a correctness gap.
3. **Run-completion push via EventBus, not external webhook.** Aegra has no
   outbound run-completion webhook, BUT the `custom_app` EventBus (mounted via
   `http.app`) serves `/events` + `/events/stream` — Hermes's actual SSE
   consumption model (webhook-less clients). Push notifications still work via
   the receiver-of-last-resort. `[[run-event-stream]]`.
4. **No DISPLAY passthrough / cookies_env dedup** — those are harness concerns
   that live in the `prepare()` seam server.py exposed; they are inert under
   Aegra (no desktop/browser host-tier jobs). Acceptable for the AP HTTP lane;
   ACP/desktop work continues to use `pux acp`.

## Phase status

- **A — local smoke** ✅ (sub `e017ea6`): `aegra dev` serves org-mode graph +
  custom_app; 11 assistants, run→final ai msg, store round-trip, /events/health.
  6 static contract tests pin the manifest.
- **B — prod artifacts** ✅ (this doc + 3 scripts): compose (pg18 mount fixed),
  env template, launcher. Sidecar stack proven (pg18.4 + pgvector 0.8.4).
- **C — reversible prod cutover + E2E**: stop server.py, run launcher, verify
  swapped stack, rollback proven.
- **D — retire `server.py`** (C3): delete after prod E2E green; only then does
  `[[no-legacy-left-behind]]` flip server.py's REST surface to a permanent
  contract failure (it stays as the proven fallback runtime until then).
