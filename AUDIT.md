# Post-Refactor Audit — `pi-pivot` (worked through 2026-07-05)

> Exhaustive sweep after the `sandbox/tools.py` → `sandbox/tools/` package split
> + `context/` rewrite + new `__main__.py` / `mcp_server.py`. Re-verified against
> the **live** tree before each fix — the snapshot this audit was
> drafted on was stale; outcomes below reflect ground truth at fix time.

## TL;DR — OUTCOMES

- **Suite is GREEN: 578 passed, 6 skipped. Contract GREEN: exit 0, 10/10 orgs OK.**
- §A (test regressions) — **already resolved** in the live tree (`test_browser_tools`
  patches `browser._sb_post`; `test_server._StubGraph` has `.nodes`; context layer
  re-baselined). No action was needed; the "RED" draft was a stale snapshot.
- §B1 (`mcp_server.py`) — wiring exists (`bin/pux` `mcp)` case), the 23-test
  `tests/test_mcp_server.py` exists and passes (drives real `Client(mcp)`). Fixed:
  the `bin/pux` **usage string** omitted `mcp`; added it. Documented the surface in
  both READMEs (was absent). Verified the `PUX_API_URL` default (:9988) matches `pux serve`.
- §C1 (`harness/README.md` layout) — **fixed**: `tools.py # 13 specialist` → the
  `tools/` package (REGISTRY 40 specialist + 7 native + 3 grader); added `mcp_server.py`,
  `agent/profile.py`, the `memory/` package; refreshed the stale 8-file test list →
  accurate count + coverage.
- §C2 (`README.md`) — **fixed**: 33→40 specialists (×5) + 260→578 tests + the branch
  layout "Phases 0–8i" → "0–18" + added `pux acp`/`pux mcp` to the Subcommands table.
- §D (legacy comments) — **fixed the model-facing ones**: the `_ADDENDUM` (appended to
  every system prompt) dropped the dead "not pi-mono" / "ignore `subagent(agent, task)`"
  bridges (NO AGENTS.md uses that wording anymore) but KEPT the live `task`-tool
  delegation instruction. Left harmless non-model-facing lineage docstrings (container.py,
  policy.py, tests) + `.gitleaks.toml` allowlist per the audit's own "keep lineage" call.
- §E — **resolved (user-approved)**: `site/README.md` was doubly stale (pi-mono +
  `@assistant-ui/react-pi`); **rewritten** to the real CopilotKit + AG-UI wiring
  (Node BFF on :3001 → harness AG-UI on :9988/agui/<org>; browser-direct Agent
  Protocol CRUD), and the main `README.md` got a new "Web UI (`site/`)" section.
  The 5.4 GB `orgs/specialists/video-production/.venv` (root-owned — created by
  uv-as-root inside the container, so a userland `rm` hit `Permission denied`)
  was **reclaimed via `sudo rm -rf`** (gitignored, regenerable via the policy
  `host_setup` hook; the 21 MB twitter-agent venv was left). **Bonus find from
  the E verify:** both READMEs told users `cd harness && uv run pytest -q`,
  which collects **zero** tests — `tests/` lives at the repo root (workspace
  pattern), not under `harness/`. Fixed to `uv run --project harness pytest -q`
  (run from the repo root) in both, + the stale "30 files" → 32.

Re-verified green after every edit batch: `uv run pytest -q` (578/6) +
`uv run pux check-contract` (exit 0).

---

## F. Claims investigated and REJECTED (do not re-chase)

1. **"specialists should be 37"** — FALSE at the time (registry pins the count);
   it is now **40** after 7 browser tools were added. README updated to 40.
2. **"`CONTRACT.md` is a blocker; rewrite it"** — MISFRAMED at the time. Its preamble froze it
   as the `master`-branch Go-server reference ("does not describe the pi-pivot branch").
   Intentionally frozen, not drift. The **live** contract on this branch is
   `pux-harness/pux_harness/agent/contract.py` (+ AGENTS.md spec). Left untouched **then**.
   **Reversed 2026-07-08:** `CONTRACT.md` was removed from this branch — the pi-pivot
   surface conforms to the upstream protocols (MCP / ACP / Agent Protocol / AG-UI), so a
   bespoke pux contract documenting deleted Go code was redundant. Recoverable via
   `git show HEAD~:CONTRACT.md` if master's frozen copy is ever needed.
3. **"`_demo` org has an agent-resolution violation"** — FALSE. `_demo`'s
   `researcher`/`browser` resolve via `orgs/_shared/agents/`, which the contract allows.
4. **"Committed multi-GB `.venv` under orgs"** — FALSE. `git ls-files` = 0 for both;
   gitignored local cruft only (video-production's is 5.4 GB on disk — §E).

---

## E. Repo-hygiene observations (RESOLVED 2026-07-05)

User chose **keep + fix + document** for `site/` and **reclaim** for the `.venv`:

- **`site/`** — 37 tracked files, referenced nowhere active. Its `README.md` was
  doubly stale ("drives **pi-mono** via `@assistant-ui/react-pi`" — pi-mono is
  deleted AND `package.json` actually uses `@copilotkit/*` + the harness's AG-UI
  endpoint). **Rewritten** from ground truth: it's a standalone React/Vite/CopilotKit
  workbench whose Node BFF (`site/server/`, :3001) proxies the chat sidebar to the
  harness AG-UI endpoint (`:9988/agui/<org>`) and owns the Node-only routes
  (files / sandbox / `node-pty` terminal / VNC reverse-proxy), while thread/run/agent
  CRUD goes browser-direct to the Agent Protocol. The main `README.md` got a new
  "Web UI (`site/`)" section so the frontend is discoverable from the top-level docs.
- **`orgs/specialists/video-production/.venv/`** — was 5.4 GB on disk (21 MB for
  twitter-agent's). Gitignored, NOT shipped, regenerable via the policy `host_setup`
  uv hook. **Reclaimed via `sudo rm -rf`** — a userland `rm` hit `Permission denied`
  because the venv was created by `uv` running as root *inside* the container, so the
  bind-mounted files were root-owned on the host. (Latent gotcha worth a memory: any
  org whose `host_setup` creates a venv in the bind-mount leaves root-owned files that
  need `sudo` to reclaim from the host.)
- **Bonus doc fix surfaced by the E verify** — both READMEs' test command was
  `cd harness && uv run pytest -q`, which collects **zero** tests: `tests/` (32 files)
  lives at the repo root (uv-workspace pattern — the harness is the sole workspace
  member), not under `harness/`. Fixed both to `uv run --project harness pytest -q`
  run from the repo root, and corrected the stale "30 files" → 32.
