# Prod build — come-back-to list

Status as of **2026-07-08**, written before the Hermes→dev-bot prod E2E build.
These are the deferred items the upstream-contract pivot left open, parked so the
prod path (phone→Hermes→ACP/Agent-Protocol→dev-bot) could ship first. Each is
scoped, sized, and tagged with its tracking task.

**Post-fold verdict (2026-08):** the prod path's HTTP/server half died with the
pre-fold harness. The workspace is a dcode workspace: the only graph surface is
deepagents' own ACP package (`deepagents-acp`) plus a JSON adapter file — there
is no repo server code, no AG-UI lane, no langgraph-api deployment. Every item
below is therefore retired-with-the-lane or superseded; **nothing here is open
work**. Each section records its disposition so nobody re-researches.

---

## P3 — Retire the hand-rolled `server.py` REST lane (task #15) — MOOT

The lane it would retire is gone. The hand-rolled Agent Protocol REST surface
(assistants/threads/runs/store, served by `pux serve` / uvicorn) was retired
with the harness at the 2026-08 fold. The Agent Protocol surface is now
deepagents' own `deepagents-acp` package plus a JSON adapter file in the
workspace — no repo server code. The old contract-test-first plan
(`/threads`, `/runs/stream`, `/store/items` SSE shapes) never needed to land:
there is no cutover because there is no hand-rolled app.

---

## P4 — Rework the Export lane → MDA project structure (task #16) — SUPERSEDED

The export lane died with the harness, and the interim marketplace-emission
successor (`pux compile --marketplace`, `src/plugins/marketplace.py`) died in
turn with the 2026-08-16 strip — plugin packaging was machinery too. If
portability is ever needed again, the dcode-native answer is shipping the
authored `.deepagents/` skills/agents directories themselves.
The MDA shape (`agent.py` + `instructions.md` + `tools/` + `middleware/` +
`connectors/mcp.*`), the `mda dev`/`mda deploy` consumer path, and the level (c)
dynamic-tools half (agent-authored `lib/`, `oras` OCI, gitleaks/ruff/uv,
APS-shaped manifest) are all retired — see `docs/dynamic-tools-and-packaging.md`
for the full disposition.

---

## AG-UI defects #5 / #6 / #7 (task #9) — HISTORICAL

The AG-UI web SSE lane (CopilotKit `/agui/{org}`, the ag-ui-langgraph adapter)
was part of the retired server lane — there is no AG-UI mount post-fold. The
defect analysis is historical: the "incremental `TEXT_MESSAGE_*` streaming
broken, deltas pass through as `RAW`" defect was MiMo-specific, and `glm-5.2`
streamed a proper `TEXT_MESSAGE_START`→`CONTENT`→`END` lifecycle. The
`test_agui_general_streams_text` permanent-contract test retired with the
`tests/server/` suite.

---

## k3s build artifacts — MOOT

The langgraph-api/k3s deployment lane retired with the server lane: no
`langgraph build` OCI image, no multi-org `langgraph.json` deployment, no
sqlite→Postgres cutover. Model configuration is dcode's own (`~/.deepagents/config.toml`) — the old multi-provider k8s Secrets wiring (ZAI `glm-5.2` / OpenRouter
`mimo`) is historical.

---

## Prod-path decisions baked in this build (for reference, NOT come-back-to)

Pre-fold decisions, all superseded by the fold except where noted:
- **Two-protocol split** (ACP=local, Agent Protocol=`pux serve` HTTP, one shared
  sqlite): retired — only the ACP leg survives, as deepagents' own
  `deepagents-acp` (stdio).
- **dev-bot container** (`sandbox.tier: bridged`, `workspace.mounts`,
  `sandbox.deps.apt`): retired with the container sandbox (`LocalShellBackend`,
  no container).
- **Hermes→dev-bot seam = MCP** (`pux mcp`, FastMCP SSE :9987): the FastMCP
  wrapper was part of the retired server lane.
- **GLM-5.2 prod = ZAI Anthropic-compat** (multi-provider model layer): retired;
  model config is dcode's own, per the operator's deepagents config.
