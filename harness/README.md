# pux-harness — deepagents Pux agent + sandbox layer

The Python harness is the whole agent + sandbox layer: per-org
[deepagents](https://docs.langchain.com/oss/python/deepagents) graphs served
over the LangChain Agent Protocol, driving a Docker sandbox directly over the
SDK (no Go server, no JSON-RPC hop). The Go MCP tree + its bridge client were
deleted in Phase 8i.

## Layout

```
pux_harness/
  server.py           # [entry] Agent Protocol server (FastAPI, :9988)
  cli.py              # [entry] `pux` client (httpx -> server)
  acp.py              # [entry] ACP stdio server (`pux acp`) — editor = TUI (Phase 9)
  main.py             # [entry] in-process runner (`pux direct`) + sandbox lifecycle
  agent/              # assembly layer — builds the deepagents graph
    graph.py          # build_graph(org) -> compiled graph (1 DockerExecClient + backend/process)
    orgs.py           # system-prompt builder + subagent loader (.pi/agents/<slug>.py
                      # SUBAGENT dicts + org.yaml rosters; resolves tools/skills/model)
    model.py          # provider/model factory (PUX_MODEL)
    contract.py       # declarative org-contract enforcer (rules 1-8 + legacy tripwires)
  sandbox/            # Docker sandbox layer — self-contained (no agent/context import)
    backend.py        # PuxSandboxBackend(BaseSandbox) -> native fs tools
    docker_exec.py    # DockerExecClient: direct `docker exec`
    container.py      # SandboxContainer: create/start/stop/remove + policy enforce
    tools.py          # 13 specialist StructuredTools (python/skills/vision/browser/desktop)
    policy.py         # declarative policy resolver
  context/            # proactive offload layer
    offload.py        # ContextOffloadMiddleware + ctx_recall/ctx_search
    store.py          # host-side stash for offloaded tool output
tests/
  test_org_contract.py    test_server.py    test_acp.py    test_policy.py
  test_container.py       test_context_offload.py    test_load_subagents.py
  test_describe_image.py
```

**Layering:** the four `[entry]` modules are invoked as `python -m
pux_harness.<name>`, so they stay top-level. `agent/` depends on `sandbox/`
+ `context/`; `sandbox/` and `context/` are each self-contained.

## Run

```bash
# 1. install
cd harness && uv sync
# 2. native-surface smoke (no model tokens, no Go server needed)
uv run python -m pux_harness.main --check
# 3. validate all 10 orgs against the declarative contract (offline)
uv run python -m pux_harness.main --check-contract
# 4. full in-process run (general's forcing task via researcher subagent)
set -a && . ../.env && set +a && uv run python -m pux_harness.main
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
