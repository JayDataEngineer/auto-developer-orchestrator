# Pux Engine Simplification — The Thin Compiler

> **Goal:** Pux becomes a thin compiler over deepagents. The engine goes from
> 31K LOC to ~200. Orgs stay exactly as they are. Tools move to MCP servers.
> The end state is a CopilotKit website that drives game/waifu creation.
>
> **Status:** This plan (2026-07) was executed — and then exceeded — by the
> 2026-08 fold: the workspace IS a dcode workspace, the engine is gone, and the
> compiler + launch live in `src/`. Post-fold reality is annotated inline.

---

## The Diagnosis

**Orgs are thin. The engine got fat.**

```
  What you wrote (per org):     ~1,700 LOC of markdown + YAML
  What accreted beneath:       31,163 LOC of Python harness engine
```

Each org is just markdown files (system prompts), YAML (which agents, which
MCP servers), and a profile. That's exactly right. The orgs didn't grow —
the **engine** underneath them accreted framework features that should be
MCP servers or direct deepagents parameters.

The engine tried to be a framework. It should be a compiler.

---

## The Principle

```
Engine  = compiler:  org config → create_deep_agent() graph   (~200 LOC)
Tools   = MCP servers (each org declares what it needs in org.yaml)
Orgs    = UNCHANGED markdown + YAML (the files you already wrote)
```

A thin compiler does exactly five things:

```python
def build_graph(org: str):
    # 1. Read org config
    config = read_yaml(f"profiles/{org}/org.yaml")
    prompt = read(f"profiles/{org}/AGENTS.md")

    # 2. Parse agent definitions
    subagents = [parse_agent_md(f) for f in glob(f"profiles/{org}/agents/*.md")]

    # 3. Connect MCP servers declared in capabilities
    mcp_tools = await open_org_mcp(org)

    # 4. Load model
    model = _get_default_model_spec()   # dcode's own config — no pux model layer

    # 5. Compile
    return create_deep_agent(
        model=model,
        tools=mcp_tools,
        subagents=subagents,
        system_prompt=prompt,
    )
```

This is now literally `src/run.py` (`build_org_agent`), with steps 3–4 through
dcode's own machinery: `resolve_and_load_mcp_tools` and `_get_default_model_spec()`.
Everything else either became an MCP server, a stock deepagents middleware
passed directly, or went away.

---

## What Stays (Legitimate Bespoke) — post-fold verdict

| Layer | Pre-fold module | Post-fold (2026-08) |
|---|---|---|
| **Org system** | orgs.py, profile.py, kit/loaders.py (~2,300 LOC) | `src/profiles/loaders.py` + `src/profiles/_paths.py` (`discover_orgs`, `org_agent_slugs`, `build_system_prompt`, `_load_agent_spec`) — this IS the product |
| **Sandbox** | docker_exec.py, container.py, backend.py (~2,300) | **RETIRED** — deepagents `LocalShellBackend` (`src/sandbox/local.py`), no Docker container; `src/sandbox/exec.py` selects the backend (`PUX_SANDBOX`) |
| **MCP client** | mcp_client.py, tool_servers.py (790) | `src/run.py` (`_load_mcp` → dcode's `resolve_and_load_mcp_tools`), `src/protocol/mcp.py` (`_org_mcp_servers`) |
| **Model factory** | model.py (773) | **RETIRED** — dcode's own `_get_default_model_spec()` (`src/run.py`) reads the operator's deepagents config |
| **Aegra integration** | upstream.py, custom_app.py (454) | **RETIRED** — the TUI is dcode's own `run_textual_app` |

---

## What Moves (Engine → MCP Server) — what actually moved

| Planned | Actual (2026-08) | Org that uses it |
|---|---|---|
| `sandbox/tools/browser.py` → `pux-browser` MCP | **DONE** — browser family migrated to the native `sandbox_browser` MCP server (in-container SeleniumBase) | browser-agent, coder |
| `sandbox/tools/` specialists → `pux-sandbox` MCP | **SUPERSEDED** — the specialists stayed in-process as the registry-keyed surface (`src/tools/registry.py`, `pux_sandbox_*` prefix retained); file/shell ops come from deepagents' `FilesystemMiddleware` | all |
| `context/` (EventStore, ctx_recall/search) → `pux-context` MCP | **never built** — the context lane retired with the fold | — |

---

## What Gets Cut (Framework → Direct) — post-fold verdict

| Module | LOC | Today | Tomorrow → post-fold |
|---|---|---|---|
| `stack.py` | 1,331 | Middleware registry + resolver + factory | `src/run.py` `build_org_agent` — a pure org→graph projection, no registry |
| `contract.py` | 1,695 | Build-time org validation | **Moved to the test suite** as planned: `tests/guards/tripwire_checks.py` (kit-import-isolation + the no-harness-refs gate) |
| `prompt_parts.py` | 371 | Multi-source prompt assembly | `src/middlewares/rubric.py` (`RubricMiddleware`) + `src/profiles/loaders.py` (`build_system_prompt`; the `_shared` addenda are dormant prose behind `load_shared_prompt_body`) |
| `agent/profile.py` middleware override system | ~300 | Per-org middleware add/remove | Middleware refs now resolve to `[rubric]` only (`src/middlewares/rubric.py`) |

**Net cut: ~3,000 LOC** from the engine — and the fold then cut the rest.

---

## Migration Phases — post-fold verdict

### Phase 0 — game-studio (ALREADY WORKS) ✅

game-studio uses ONLY MCP servers (Ray inference, Godot, web-research).
Zero dependency on baked-in engine tools. Verified pre-fold:

- AsyncSubAgent: supervisor → (pre-fold: Aegra) → generate(comfyui_video) → 235KB MP4
- Waifu pipeline: ComfyUI sprite → Godot scene → screenshot

**Nothing to migrate. game-studio works on the thin engine today.**

### Phase 1 — pux-sandbox MCP server — SUPERSEDED

Plan: wrap `docker_exec.py` + specialist tools as a stdio MCP server.

```
profiles/specialists/coder/org.yaml changes:
  capabilities:
-   - {kind: tool, ref: python}        # was baked in engine
+   - {kind: mcp, ref: pux-sandbox}    # now an MCP server
```

What happened instead: the specialists stayed in-process under
`src/tools/registry.py`, and the Docker exec client was retired with the
container — file/shell capability is deepagents' native `FilesystemMiddleware`.

### Phase 2 — pux-browser MCP server ✅ DONE

Plan: wrap `sandbox/tools/browser.py` (SeleniumBase) as a stdio MCP server.
Done as the **`sandbox_browser` MCP server** (in-container SeleniumBase),
referenced per-agent via `{kind: mcp, ref: sandbox_browser}`.

### Phase 3 — Thin the engine ✅ DONE (2026-08 fold)

1. `stack.py:build_stack()` → `src/run.py` `build_org_agent` (~50 LOC pure function)
2. Middleware passed directly to `create_deep_agent()` (rubric only)
3. Prompt assembly from AGENTS.md + profile suffix (`src/profiles/loaders.py`)
4. Contract validation moved to the test suite (`tests/guards/`)

### Phase 4 — pux-context MCP (optional) — never built

The context/EventStore lane retired with the fold; no successor.

---

## The End State

```
┌─────────────────────────────────────────────────────┐
│                  CopilotKit Website                  │
│            (AG-UI → streaming, Gen UI, HITL)        │
└────────────────────────┬────────────────────────────┘
                         │ AG-UI / SSE
┌────────────────────────▼────────────────────────────┐
│       dcode TUI + deepagents ACP (deepagents-acp)   │
│              run_textual_app — the graph surface    │
└────────────────────────┬────────────────────────────┘
                         │ graph__{org}
┌────────────────────────▼────────────────────────────┐
│           Thin Compiler (src/compiler + src/run.py)  │
│  profiles/<org>/{org.yaml, AGENTS.md} → create_deep_agent() │
└────────────────────────┬────────────────────────────┘
                         │ MCP tools
┌────────────────────────▼────────────────────────────┐
│                   MCP Servers                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────────┐ ┌───────┐│
│  │Ray (CUI) │ │Godot MCP │ │sandbox_browser│ │web-research││
│  │images    │ │scenes    │ │browser/eval  │ │search ││
│  │video     │ │sprites   │ │screenshots   │ │fetch  ││
│  │audio     │ │GDScript  │ │              │ │       ││
│  │3D        │ │          │ │              │ │       ││
│  └──────────┘ └──────────┘ └──────────────┘ └───────┘│
└─────────────────────────────────────────────────────┘
```

**User flow on the website:**
1. User types: "Make a cyberpunk waifu with silver hair"
2. CopilotKit streams the request to the game-studio graph
3. Art-specialist calls Ray MCP → generates character sprite
4. Gameplay-programmer calls Godot MCP → loads sprite into scene
5. User sees the character appear in a Godot viewport, streamed back
6. Iterates: "give her a red jacket" → art-specialist regenerates

---

## What NOT to Touch — post-fold verdict

- **Org files** — now under `profiles/`: `org.yaml`, `agents/*.md`, `AGENTS.md`,
  `profile.yaml`, `policy.yaml` stay exactly as they are
- **Docker sandbox core** — **RETIRED** with the container; the backend is
  deepagents' `LocalShellBackend` (`src/sandbox/local.py`)
- **MCP client** — replaced by dcode's `resolve_and_load_mcp_tools`
  (`src/run.py`); `src/protocol/mcp.py` projects org refs onto `.mcp.json`
- **Model factory** — **RETIRED**; dcode's `_get_default_model_spec()` rules
- **All orgs** — game-studio, coder, deep-research-engine, browser-agent,
  twitter-agent, telegram-agent, orchestrator, video-production, etc. ALL stay

The orgs are the product. The compiler is plumbing. The plumbing is now thin.
