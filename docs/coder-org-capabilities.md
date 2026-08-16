# Coder org — capability map

> So we don't research this again. Tested 2026-07-12 (pre-fold) with `toad 0.6.20`,
> coder base model `glm-5.2`. Post-fold (2026-08), surviving claims re-verified
> against the workspace; retired machinery is marked.

The **coder** org (`profiles/specialists/coder/`) is the Claude-Code-equivalent
dev-bot. This doc records the two capabilities operators ask about, the exact
wire path for each, and how to re-verify.

---

## (a) Web-browser subagent — `web-agent` (already present)

**Status: shipped.** The roster is exactly
[`coder-explorer`, `code-worker`, `web-agent`] and is pinned by a permanent
tripwire (`tests/dcode/test_dcode_native.py` — the real-coder-org test asserts
the roster, `web-agent`'s mcp-only tool set, and its `RubricMiddleware`).

### What it is

`profiles/specialists/coder/agents/web-agent.md` — an **E2E web verification
specialist**. It is NOT the generic shared browser agent; it is
purpose-built for the coding loop: load the deployed page, assert elements
exist, fill + submit forms, capture screenshot evidence, return a PASS/FAIL
report. The CTO delegates the live-browser verify step to it (see `AGENTS.md`
§Workflow step 4).

### Tool surface

The browser surface is the **native `sandbox_browser` MCP server** (in-container
SeleniumBase Chrome), referenced from the agent frontmatter as
`{kind: mcp, ref: sandbox_browser}` — `web-agent` is mcp-only (no REGISTRY
tools). Pre-fold this was the `pux_sandbox_browser_*` specialist family (27
REGISTRY tools: `browser_navigate`, `browser_click`, `browser_type`, …); the
fold deleted those specialists and the browser family migrated to the MCP
server. The server's container self-boots lazily on first call;
`profiles/_shared/sandbox/warmup_browser.py` pre-warms Chrome to skip the
SeleniumBase CDP-attach cold start.

### Why not the shared generic `browser.md`?

`profiles/_shared/agents/browser.md` exists as a broader web-browsing specialist
(search/read/interact). Coder's `web-agent` is intentionally narrower —
verify-and-report — to fit the dev-bot ship gate. Both exist; coder uses its
own. (If a future org wants the generic browser agent, roster the slug
`browser` and resolution falls through `profiles/_shared/agents/browser.md`
automatically — `org_agent_slugs` (`src/profiles/loaders.py`) is org-local
first, then `_shared`.)

### Verify

```bash
uv run pytest tests/dcode/test_dcode_native.py -q
# includes the roster pin + the web-agent wiring assertions (mcp-only tools,
# RubricMiddleware on the agent).
```

---

## (b) CodeInterpreterMiddleware — RETIRED (2026-08 fold)

**Historical.** The QuickJS interpreter lane (`CodeInterpreterMiddleware` from
`langchain-quickjs` — a sandboxed in-process JS REPL plus a `task(...)` global
so the CTO writes one short dispatch script instead of grinding items one
tool call at a time) died with the pre-fold harness at the 2026-08 fold:

- The auto-mount **strength gate** is gone with the harness's model module —
  `models.yaml`, `get_model`/`resolve_model_id`, `driver_strong_orchestrator`,
  and the `strength:` tiers are retired. Model config is now dcode's own
  `_get_default_model_spec()` (`src/run.py`) reading the operator's deepagents
  config (`[models].default`); there is no per-role tier table.
- Profile `middleware:` refs now resolve only to `[rubric]`
  (`src/middlewares/rubric.py` → deepagents `RubricMiddleware`); an
  `interpreter` ref raises `unknown middleware ref`. A leftover
  `middleware: {supervisor: {add: [interpreter]}}` block in a profile is dead
  config.

If you need an eval/interpreter capability today, mount it the dcode-native way
(a deepagents middleware or a tool), not through the retired lane.

### Verify (surviving equivalent)

```bash
uv run python src/run.py --org coder --dry-run
# prints org, model default, MCP servers, and per-subagent tools + middleware —
# the honest replacement for the pre-fold build_stack introspection.
```

### Security posture (what survives)

The `eval`-tool boundary is gone with the interpreter. The surviving posture:
`RubricMiddleware` gates post-CTO evidence (`pux_grader_*` tools), the agent's
workspace is the host filesystem via deepagents `LocalShellBackend` (no Docker
sandbox), and `sandbox_browser` runs in its own container. Untrusted-code
isolation is the operator's concern per transport, same as any MCP server.

---

## Related docs

- `docs/toad-integration.md` — the ACP/TUI story (toad / Aethna / Hermes consume
  the deepagents ACP server).
- `AGENTS.md` (repo root) — the per-org profile field reference.
- `src/run.py` — the single factory that compiles every org's graph
  (`build_org_agent`).
