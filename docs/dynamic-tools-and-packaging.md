# Dynamic Tools (level c) + Agent Package Engine — RETIRED

**Status: RETIRED (2026-08 fold).** This design (2026-07-08) built a fourth tool
level — **dynamic, agent-authored functions** (`pux_dyn_*`: `make_function` /
`edit_function` / `list_functions` / `call_function`, persisted in
`profiles/<org>/lib/` with `index.yaml` bookkeeping, prunable to `.archive/`,
promotable to git-tracked `sandbox/functions/` via `pux promote-function`) — and
a professional packaging engine around it (default-deny manifest, `pux lock`
commit-pinning, pre-pack hook pipeline, OCI artifact via `oras`). Everything
shipped in phases P1–P5 as of 2026-07-08, then died with the pre-fold harness at
the 2026-08 fold.

## What shipped (historical record, pre-fold)

- **P1 — level (c) minimal slice:** `lib/` + the four `pux_dyn_*` tools +
  `sandbox.dynamic_tools` opt-in (24 unit tests incl. the bounded-result thesis).
- **P2 — pruning + graduation:** `pux promote-function` (c→b, lib→git-tracked
  `sandbox/functions/`) + `pux archive-function` (reversible) — 9 unit tests +
  live CLI round-trip on `invest`.
- **P3 — manifest + lockfile + default-deny pack:** `pux pack`, `pux lock`,
  `org.lock.yaml` (github MCP refs → commit SHAs via host-side `git ls-remote`);
  `pux export` HARD-ERRORS (Decision 5). 50 manifest/lockfile + 128 export/CLI
  tests; live lock resolved a real SHA.
- **P4 — hook pipeline:** `PACK_HOOK_REGISTRY` (`ast_check_hook` via stdlib
  `compile`, `gitleaks_hook` via the host CLI, reuse-first). A planted `ghp_`
  PAT REFUSED the pack (23 hook unit tests + 5 integration).
- **P5 — OCI artifact via `oras`:** `pux pack --oci` — `config` /
  `source-code` / `agent-library` layers, `provenance.json` with per-layer
  SHA-256 tamper anchors (10 injected-runner tests).

## Locked decisions (2026-07-08 — now moot)

1. Manifest schema → pux-owned, APS-shaped (conform-later, no hard dependency on
   the v0.1 `agentpackaging.org` spec).
2. `oras` binding → shell-out to the `oras` CLI binary; pack is a **host-side**
   operation.
3. Signing → P6 stretch; P5 reserves the slots.
4. `org.lock.yaml` → committed to Git by default.
5. `pux export` → `pux pack`: hard error on the old verb, **no silent alias**.
6. Sequence → P1 first.

## What replaced it (2026-08 fold)

- **The learned-function lane has no successor.** Agent-authored persistent
  functions are out of scope; the tool surface is the registry-keyed set in
  `src/tools/registry.py` plus MCP servers. `lib/`, `pux_dyn_*`,
  `pux promote-function`, and `pux archive-function` do not exist.
- **The level taxonomy survives only in part:** (a) MCP/universal tools — yes,
  unchanged; (b) pythonic/declared (`sandbox/tools/tools.yaml`) — only as
  dormant data files under `profiles/**/sandbox/`, no code reads them; (c)
  dynamic — gone.
- **Packaging survives only as marketplace emission:** `pux compile --marketplace`
  (`src/plugins/marketplace.py`, `emit_plugin` / `emit_marketplace`) emits an org
  as an installable dcode plugin — portable orgs are *installed*, not packed.
  The CLI is `src/compiler/cli.py` with exactly three subcommands (`sync`,
  `check`, `compile`); `uv run pux sync` emits the union `.deepagents/` +
  `.mcp.json` at the project root.

## Sources

- APS: https://agentpackaging.org/ (`apstool`, Apache-2.0, v0.1) · `.agent`: https://github.com/nomoticai/agentpk (patent-pending, ruled out)
- OCI artifacts / `oras`: https://oras.land/ · https://helm.sh/blog/storing-charts-in-oci/ · https://fluxcd.io/flux/cheatsheets/oci-artifacts/
- LangGraph manifest: https://docs.langchain.com/oss/javascript/langgraph/studio (`langgraph.json`)
