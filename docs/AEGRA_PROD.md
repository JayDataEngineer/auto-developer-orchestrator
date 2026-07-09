# Aegra — pux's free prod Agent Protocol runtime

**Status:** 2026-07-09. Phase A (local smoke) SHIPPED+PROVEN (submodule `e017ea6`).
Phase B (prod cutover artifacts) SHIPPED (commit `43e281c`). Phase C (reversible
prod cutover + E2E + reboot-safe systemd) SHIPPED+PROVEN (commits `8640a25` +
the unit). Phase D (retire `server.py`) deferred-by-design — `server.py` retained
as the disabled fallback runtime (see Phase status). Tracking task #28; this
resolves the deferred P3 in `docs/prod-come-back-to.md`.

**PROD IS NOW AEGRA.** `pux-aegra.service` (enabled) brings the Aegra stack up at
boot; `pux-prod.service` (server.py) is disabled. Verified cloud E2E
(`cloud → Tailscale → Aegra → dev-bot → glm-5.2` = `CLOUD-E2E-OK`) on the
systemd-managed stack.

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

### systemd (prod, reboot-safe)

`scripts/pux-aegra.service` (oneshot + `RemainAfterExit`, `Requires=docker.service`
for the sidecars, `After=tailscaled.service`) is the prod unit — installed to
`/etc/systemd/system/`, enabled, started. The matching `pux-prod.service`
(server.py) is **disabled** so a reboot brings up Aegra, not server.py.

```bash
# one-time install (from repo)
sudo cp scripts/pux-aegra.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl disable --now pux-prod.service     # reboot -> Aegra, not server.py
sudo systemctl enable --now pux-aegra.service
```

Standard unit verbs then drive the stack: `systemctl restart pux-aegra`,
`systemctl stop pux-aegra` (runs `start_pux_aegra.sh stop` → process-group kill
+ port sweep). **Rollback to server.py:** `sudo systemctl disable --now
pux-aegra.service && sudo systemctl enable --now pux-prod.service`.

### manual (scripts/)

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
2. **Prepare/warmup — RESTORED via `PrepareWarmupMiddleware`.** Aegra owns the
   run loop (no pux entry point to call `prepare()` from, unlike `pux direct` /
   `server.py`), so the warmup seam initially did not fire. **Fixed:** the
   `before_agent` hook (`context/prepare_warmup.py`, armed by
   `runtime/upstream.py` via `RuntimeFacts(prepare_warmup=True)`) runs
   `prepare()` once per run, offloaded to a worker thread (`asyncio.to_thread`)
   so the loop keeps serving `/events` while Docker I/O runs. `warmup_browser` /
   `warmup_webhook` fire again, warn-and-continue. PROVEN LIVE: `prepare(general)`
   ran 2 jobs (`warmup_webhook` OK; `warmup_browser` failed warn-and-continue —
   `sb_server` not pre-running, agent cold-starts by design); run succeeded.
   `[[browser-warmup]]`, `[[run-event-stream]]`.
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
- **B — prod artifacts** ✅ (commit `43e281c`): compose (pg18 mount fixed), env
  template, launcher. Sidecar stack proven (pg18.4 + pgvector 0.8.4).
- **C — reversible prod cutover + E2E + systemd** ✅ (commit `8640a25` + unit):
  cutover proven (Aegra serves the real prod graph on :9988, 11 assistants,
  glm-5.2 run); reversibility proven (server.py rebinds :9988 + serves healthy
  after Aegra stop); cloud E2E proven (`cloud→Tailscale→Aegra→dev-bot→glm-5.2` =
  `CLOUD-E2E-OK`); reboot-safe via `pux-aegra.service` (enabled) with
  `pux-prod.service` disabled. **Gotcha fixed:** `aegra serve` spawns uvicorn as a
  child, so the launcher now `setsid`s the process and `stop` kills the whole
  process group (+ port-derived sweep) — otherwise uvicorn is orphaned on :9988
  and server.py cannot rebind on rollback.
- **D — retire `server.py`** (C3) — **deferred-by-design.** `server.py` stays
  installed as the **disabled fallback runtime** (rollback target), per the
  standing `[[langgraph-api-license-gate]]` decision to retain it. Retiring it
  requires a full REST endpoint-parity contract test first
  (`[[no-legacy-left-behind]]` — proven equivalent on EVERY endpoint before the
  old form becomes a permanent contract failure). Teed up as task #32.
