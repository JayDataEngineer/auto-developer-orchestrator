# pux-harness — deepagents Pux agent + sandbox layer

The Python harness is the whole agent + sandbox layer: per-org
[deepagents](https://docs.langchain.com/oss/python/deepagents) graphs served
over the LangChain Agent Protocol, driving a Docker sandbox directly over the
SDK (no Go server, no JSON-RPC hop). The Go MCP tree + its bridge client were
deleted in Phase 8i. The harness owns the **full** sandbox lifecycle —
`pux sandbox start` runs any `policy.yaml host_setup` hooks + builds the
`sandbox.build` image before container create (Phase 13); there is no operator
`bootstrap.sh` / `docker-compose.yml` (the per-org shadow lifecycle was deleted
+ made a permanent contract failure).

## Layout

```
pux_harness/
  cli.py              # [entry] unified `pux` CLI (console_scripts) — dispatches all commands
  __main__.py         # [entry] `python -m pux_harness`
  server.py           # Agent Protocol server (FastAPI, :9988) — `pux serve`
  acp.py              # ACP stdio server — `pux acp` (editor = TUI, Phase 9)
  main.py             # in-process runner — `pux direct` + sandbox lifecycle
  agent/              # assembly layer — builds the deepagents graph
    graph.py          # build_graph(org) -> compiled graph (1 DockerExecClient + backend/process)
    orgs.py           # system-prompt builder + subagent loader (orgs/<org>/agents/<slug>.md
                      # frontmatter+body + orgs/_shared/agents + org.yaml rosters; resolves tools/skills/model)
    model.py          # provider/model factory (PUX_MODEL)
    contract.py       # declarative org-contract enforcer (rules 1-8 + legacy tripwires)
  sandbox/            # Docker sandbox layer — self-contained (no agent/context import)
    backend.py        # PuxSandboxBackend(BaseSandbox) -> native fs tools
    docker_exec.py    # DockerExecClient: direct `docker exec`
    container.py      # SandboxContainer: create/start/stop/remove + policy enforce
    tools.py          # 13 specialist StructuredTools (python/skills/vision/browser/desktop)
    policy.py         # declarative policy resolver
    host_setup.py     # host-side hooks (cached uv venv, stdout → env exports)
  context/            # unified context-saving layer (one store, one middleware, all agents)
    events.py        # EventStore (.pux/events.sqlite): structured events + offloaded blobs, FTS5/BM25
    middleware.py    # ContextMiddleware — capture + offload in one wrap_tool_call pass
    tools.py         # ctx_recall (full blob by handle) + ctx_search (BM25 over events+blobs)
    layer.py         # build_context_layer() — the seam imported by graph.py + orgs._build_sub
    snapshot.py      # cross-session resume snapshot (reads events)
    session_guide.py # session_guide middleware (reads events)
    sandbox_routing.py  # RoutingMiddleware (org-aware exec routing)
tests/
  test_org_contract.py    test_server.py    test_acp.py    test_policy.py
  test_container.py       test_context_offload.py    test_load_subagents.py
  test_describe_image.py  test_host_setup.py
```

**Layering:** the unified CLI (`cli.py`) dispatches all commands through one
entry point; `agent/` depends on `sandbox/` + `context/`; `sandbox/` and
`context/` are each self-contained.

## Run

```bash
# 0. the repo root is the uv workspace — run `uv sync` THERE, not in harness/.
#    (the root pyproject's [tool.uv.workspace] declares harness/ as its sole
#    member; one `uv sync` materializes one .venv for the whole workspace.
#    harness/.venv and harness/.python-version were pre-workspace orphans and
#    are gone — the root .python-version + both pyprojects' requires-python
#    govern the interpreter now.)
# 1. install (from repo root)
uv sync
# 2. native-surface smoke (no model tokens, no Go server needed)
uv run pux check
# 3. validate all 10 orgs against the declarative contract (offline)
uv run pux check-contract
# 4. full in-process run (requires --task; see tests/integration/default_tasks.py for forcing tasks)
set -a && . .env && set +a && uv run pux direct --org general --task "How many Python modules ship under /sandbox/workspace/harness/pux_harness/?"
#    (mimo exhausts on rate limits; use PUX_MODEL=glm-5.2 if so)
```

The Agent Protocol server (the canonical executor) runs from the repo root via
`pux serve` (FastAPI on `http://127.0.0.1:9988`); the `pux` client drives it.

To expose an org to an ACP-speaking editor (Zed / VS Code via vscode-acp /
Neovim) — the editor IS the TUI:

```bash
pux acp --org general            # stdio ACP server; sandbox self-boots lazily
```

## Env

- `OPENCODE_API_KEY` — required (OpenCode Zen Go, OpenAI-compatible).
- `PUX_MODEL` — default `mimo-v2.5`. mimo is a reasoning model that exhausts
  on rate limits; `PUX_MODEL=glm-5.2` is the clean non-reasoning alternative.
- `PUX_API_HOST` / `PUX_API_PORT` / `PUX_API_DB` / `PUX_API_LOG` — server bind
  + SQLite path + log level (defaults `127.0.0.1:9988`,
  `<project>/.pux/agent-protocol.sqlite`, `info`).
- `PUX_ORG` — when set, the sandbox container is created with that org's
  policy applied (egress ACLs, creds, image/tier, cookies).

See the repo `CLAUDE.md` for the full architecture, tool-surface table, and
pivot roadmap.
