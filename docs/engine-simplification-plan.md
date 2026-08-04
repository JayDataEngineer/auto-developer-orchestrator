# Pux Engine Simplification — The Thin Compiler

> **Goal:** Pux becomes a thin compiler over deepagents. The engine goes from
> 31K LOC to ~200. Orgs stay exactly as they are. Tools move to MCP servers.
> The end state is a CopilotKit website that drives game/waifu creation.

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
    config = read_yaml(f"orgs/{org}/org.yaml")
    prompt = read(f"orgs/{org}/AGENTS.md")

    # 2. Parse agent definitions
    subagents = [parse_agent_md(f) for f in glob(f"orgs/{org}/agents/*.md")]

    # 3. Connect MCP servers declared in capabilities
    mcp_tools = await open_org_mcp(org)

    # 4. Load model
    model = get_model(config.get("model", "glm-5.2"))

    # 5. Compile
    return create_deep_agent(
        model=model,
        tools=mcp_tools,
        subagents=subagents,
        system_prompt=prompt,
    )
```

Everything else either becomes an MCP server, a stock deepagents middleware
passed directly, or goes away.

---

## What Stays (Legitimate Bespoke)

| Layer | Module | LOC | Why it stays |
|---|---|---|---|
| **Org system** | orgs.py, profile.py, kit/loaders.py | ~2,300 | The multi-org config layer — this IS the product |
| **Docker sandbox** | docker_exec.py, container.py, backend.py | ~2,300 | Implements deepagents' SandboxBackendProtocol — your IP |
| **MCP client** | mcp_client.py, tool_servers.py | 790 | Already a thin wrapper over langchain_mcp_adapters |
| **Model factory** | model.py | 773 | Multi-provider routing + fallback chains |
| **Aegra integration** | upstream.py, custom_app.py | 454 | Already thin — generates graph__{org} per org |

These are genuine infrastructure. They stay.

---

## What Moves (Engine → MCP Server)

Tools currently baked into the engine move OUT to MCP servers. The org
declarations stay the same — only the tool source changes.

| Today (baked in engine) | Tomorrow (MCP server) | LOC moved | Orgs that use it |
|---|---|---|---|
| `sandbox/tools/` (13 specialists) | `pux-sandbox` MCP | ~4,000 | coder, deep-research-engine |
| `sandbox/tools/browser.py` | `pux-browser` MCP | 1,326 | browser-agent |
| `context/` (EventStore, ctx_recall/search) | `pux-context` MCP or optional middleware | ~2,000 | deep-research-engine |

Each MCP server wraps existing code — the Docker exec client, the SeleniumBase
browser, the EventStore. The tools work identically; they just live outside
the engine.

---

## What Gets Cut (Framework → Direct)

| Module | LOC | Today | Tomorrow |
|---|---|---|---|
| `stack.py` | 1,331 | Middleware registry + resolver + factory | ~50 LOC pure function returning create_deep_agent kwargs |
| `contract.py` | 1,695 | Build-time org validation | Simplified to ~200 LOC or moved to test suite |
| `prompt_parts.py` | 371 | Multi-source prompt assembly | Direct system_prompt from AGENTS.md + profile suffix |
| `agent/profile.py` middleware override system | ~300 | Per-org middleware add/remove | Direct middleware list per org (simpler) |

**Net cut: ~3,000 LOC** from the engine. Not 10K — that would require killing
orgs. But 3K of genuine indirection that deepagents 0.7.x handles natively.

---

## Migration Phases

### Phase 0 — game-studio (ALREADY WORKS) ✅

game-studio uses ONLY MCP servers (Ray inference, Godot, web-research).
Zero dependency on baked-in engine tools. Proven today:

- AsyncSubAgent: supervisor → Aegra → generate(comfyui_video) → 235KB MP4
- Waifu pipeline: ComfyUI sprite → Godot scene → screenshot

**Nothing to migrate. game-studio works on the thin engine today.**

### Phase 1 — pux-sandbox MCP server

Wrap `docker_exec.py` + specialist tools as a stdio MCP server.

```
orgs/specialists/coder/org.yaml changes:
  capabilities:
-   - {kind: tool, ref: python}        # was baked in engine
+   - {kind: mcp, ref: pux-sandbox}    # now an MCP server
```

Unblocks: coder, deep-research-engine, any org that needs file/shell tools.

### Phase 2 — pux-browser MCP server

Wrap `sandbox/tools/browser.py` (SeleniumBase) as a stdio MCP server.

Unblocks: browser-agent.

### Phase 3 — Thin the engine

With tools in MCP servers, simplify the engine:

1. Replace `stack.py:build_stack()` with a ~50 LOC pure function
2. Pass middleware directly to `create_deep_agent()` (no registry)
3. Simplify prompt assembly (read AGENTS.md + profile suffix, done)
4. Move contract validation to the test suite

### Phase 4 — pux-context MCP (optional)

If deep-research-engine needs context management (ctx_recall/search), wrap
the EventStore as an MCP server. Only research-heavy orgs arm it.

---

## The End State

```
┌─────────────────────────────────────────────────────┐
│                  CopilotKit Website                  │
│            (AG-UI → streaming, Gen UI, HITL)        │
└────────────────────────┬────────────────────────────┘
                         │ AG-UI / SSE
┌────────────────────────▼────────────────────────────┐
│              Aegra :9988 (Agent Protocol)            │
│         langgraph-api — threads/runs/store           │
└────────────────────────┬────────────────────────────┘
                         │ graph__{org}
┌────────────────────────▼────────────────────────────┐
│           Thin Engine (~200 LOC compiler)            │
│    org.yaml + agents/*.md → create_deep_agent()     │
└────────────────────────┬────────────────────────────┘
                         │ MCP tools
┌────────────────────────▼────────────────────────────┐
│                   MCP Servers                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌─────────┐│
│  │Ray (CUI) │ │Godot MCP │ │pux-sandbox│ │web-research││
│  │images    │ │scenes    │ │file/shell │ │search   ││
│  │video     │ │sprites   │ │code exec  │ │fetch    ││
│  │audio     │ │screens   │ │           │ │         ││
│  │3D        │ │GDScript  │ │           │ │         ││
│  └──────────┘ └──────────┘ └──────────┘ └─────────┘│
└─────────────────────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────┐
│              Langfuse Observability                  │
└─────────────────────────────────────────────────────┘
```

**User flow on the website:**
1. User types: "Make a cyberpunk waifu with silver hair"
2. CopilotKit streams the request to Aegra → game-studio graph
3. Art-specialist calls Ray MCP → generates character sprite
4. Gameplay-programmer calls Godot MCP → loads sprite into scene
5. User sees the character appear in a Godot viewport, streamed back
6. Iterates: "give her a red jacket" → art-specialist regenerates

---

## What NOT to Touch

- **Org files** — `org.yaml`, `agents/*.md`, `AGENTS.md`, `profile.yaml` stay
- **Docker sandbox core** — `docker_exec.py`, `container.py`, `backend.py` stay
- **MCP client** — `mcp_client.py`, `tool_servers.py` stay
- **Model factory** — `model.py` stays
- **All orgs** — game-studio, coder, deep-research-engine, browser-agent,
  twitter-agent, telegram-agent, orchestrator, video-production, etc. ALL stay

The orgs are the product. The engine is plumbing. We're fixing the plumbing.
