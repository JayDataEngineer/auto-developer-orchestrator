# pux-harness — deepagents-based Pux (Phase 0 pivot spike)

Replaces the pi-mono TS harness with a Python **deepagents** harness. Phase 0
proves: deepagents drives the existing pux sandbox (Go MCP server at
`127.0.0.1:9987`) via a direct JSON-RPC bridge, and does CTO→researcher
subagent delegation — on the `general` org, with real cost/output numbers.

See `memory/project_deepagents_pivot.md` for the full migration plan.

## Layout

```
pux_harness/
  bridge.py   # Go MCP server → LangChain StructuredTools (prefixed pux_sandbox_)
  model.py    # mimo-v2.5 via OpenCode Zen Go (OpenAI-compatible)
  orgs.py     # org AGENTS.md + .pi/agents/*.md loaders (port verbatim)
  main.py     # create_deep_agent wiring + run
```

## Run

```bash
# 0. Go MCP server must be live at 127.0.0.1:9987 (task start / task run)
# 1. install
cd harness && uv sync
# 2. bridge smoke (no model tokens)
uv run python -m pux_harness.main --check
# 3. full run (general's arch-summary task via researcher subagent)
set -a && . ../.env && set +a && uv run python -m pux_harness.main
```

## Env

- `OPENCODE_API_KEY` — required (OpenCode Zen Go).
- `PUX_MODEL` — default `mimo-v2.5`. mimo is a reasoning model; if it breaks the
  agent loop, set `PUX_MODEL=glm-5.2` (clean, non-reasoning, same endpoint).
- `PUX_MCP_URL` — default `http://127.0.0.1:9987/`.
