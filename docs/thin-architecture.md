# Thin Architecture — The Simplification Mandate

> **Status:** ACTIVE DECISION (2026-08-03) — **SUPERSEDED AND COMPLETED by the
> 2026-08-16 strip.** The repo now contains *zero* harness code: `src/`
> (compiler, launcher, middlewares, tool registry), `profiles/`, and `tests/`
> are deleted. dcode — the globally installed CLI — IS the harness: it loads
> the authored `.deepagents/` surface (agents, skills) and `.mcp.json` itself.
> The launch command is `dcode`, nothing else. The principles below are the
> governing philosophy; post-strip verdicts are annotated where the decision
> changed again after the fold.

## The Decision

Pux becomes a **thin wrapper over upstream deepagents.** We stop building
custom agent infrastructure that the upstream now does better. We strip
accumulated complexity. We keep only what upstream cannot provide: sandbox
isolation, game-dev-specific tools, and the editor interface layer.

**Post-fold:** sandbox isolation is now deepagents' own `LocalShellBackend`
(host fs — the Docker container was retired), game-dev tools are MCP servers,
and the interface layer is dcode's own TUI + upstream `deepagents-acp`.

## Why

We built Pux to solve agent delegation for creative work (game development).
The upstream — [langchain-ai/deepagents](https://github.com/langchain-ai/deepagents)
— now solves this better than we ever could on our own. Our bespoke additions
are not improvements. They are **context bloat** that degrades agent performance.

The proof: months of building Pux, and the game (Tech Noir) still isn't
shipped. The platform became the product. The platform was never the goal.
The game is the goal.

## Core Principles

### 1. Upstream First
Before building anything, check if deepagents, LangGraph, CopilotKit, or the
model itself already does it. If it does, use it. Don't reinvent.

### 2. Context Is a Cost
Every line in an agent's context must earn its place. If the model can work
without it, remove it. Context bloat causes tool avoidance, hallucination,
and unfocused output. Clean context produces sharp, tool-using agents.

### 3. Tools Are Self-Describing
Tools carry their own schemas (from Ray's `/v1/form-spec`). Agents learn
capabilities from tool definitions, not from prose. Adding a new model to
Ray's catalog automatically makes it available to every agent — no code
changes in Pux.

### 4. Skills Are On-Demand
Load ONE skill when relevant, not all skills at boot. Skills are markdown
documents that the model reads when it needs procedure. The agent sees a
one-line index, picks the relevant skill, loads the full doc only when needed.

### 5. State Is Externalized
SurrealDB holds state. Agents load fragments for the current task, never the
whole database. "Show me the merchant character" loads one record, not the
entire asset manifest.

### 6. Agent Prompts Are Thin
20 lines maximum: role, tools, key constraints. If you need more, the prompt
is doing the model's job. The model is smart — give it the task, the tools,
the relevant skill, and let it work.

### 7. The Game Is the Goal
Every architectural decision must answer: "does this help ship Tech Noir?"
If not, don't build it. We are not building a platform company. We are
making a game with AI assistance.

## What Stays (Infrastructure With Unique Value) — post-fold verdict

| Component | Why It Stays | Post-fold (2026-08) |
|-----------|-------------|---------------------|
| Docker sandbox | Isolated execution — upstream doesn't provide this | **RETIRED** — host fs via dcode's own backend; the repo carries no backend code |
| SurrealDB integration | Shared state across agents and sessions | unchanged (external — game infra, not repo code) |
| godot-mcp bridge | Godot editor control — game-specific, no upstream equivalent | survives as an MCP server the game-studio org declares |
| ACP (Agent Client Protocol) | UI-to-runtime separation — enables CopilotKit | **moved upstream** — the repo serves no ACP of its own; the surface is `deepagents-acp` |
| CopilotKit export | Web UI for visual creative work — text interfaces can't do game dev | **RETIRED** with the server lane; the TUI is dcode's own `run_textual_app` |
| Pux harness | The deepagents graph builder + CLI — this IS the thin wrapper | **DELETED** — even the thin wrapper was machinery dcode already ships; the repo authors `.deepagents/` + `.mcp.json`, dcode runs them |

## What Gets Stripped (Bloat Replaced by Upstream or Deleted) — all done

| Component | Replacement | Status |
|-----------|-------------|--------|
| Custom agent orchestration | deepagents graphs (the upstream pattern) | done — dcode's own graph builder; no `src/` remains |
| 200-line CTO overlay prompts | 20-line thin agent definitions | done — the authored `.deepagents/agents/<name>/AGENTS.md` files ARE the definitions |
| Preloaded skill system | On-demand skill loading by task type | done — deepagents `SkillsMiddleware` (name+description only; body via `read_file`) |
| Custom TUI (TOAD) | CopilotKit web UI | **changed** — the TUI is dcode's `run_textual_app` (upstream, not bespoke) |
| Bespoke delegation protocol | deepagents task() delegation | done |
| Accumulated "improvements" from 8 months | Deletion. The model doesn't need them. | done — the 2026-08 fold |
| Forge API client | Ray's `/v1/run` via MCP tool wrapper | done — Ray MCP server |
| Hardcoded model knowledge | Ray's `/v1/form-spec` — schema discovery | done; model config lives in `~/.deepagents/config.toml` (dcode's own) |

## The Target Stack — post-fold reality

```
dcode (globally installed CLI — the harness, the TUI, the graph)
  └── deepagents graph (dcode's own create_deep_agent)
      ├── Main agent (root AGENTS.md + .deepagents/AGENTS.md appended)
      ├── Subagents (.deepagents/agents/<name>/AGENTS.md — 30 specialists)
      ├── Skills (.deepagents/skills/ — on-demand, name+description index)
      └── Rubrics (folded into agent files as "## Quality bar" sections)
          │
          ▼
MCP Tools (self-describing — schema from Ray form-spec)
  ├── ray_inference (generate_image / audio / video / 3D → /v1/run)
  ├── godot-mcp-runtime (scenes, GDScript)
  ├── sandbox_browser (in-container mc_browser.py — browser/eval)
  └── web_research (search/fetch/research)
      │
      ▼
Ray (inference engine — localhost:33080)
  ├── /v1/form-spec ← single schema source
  ├── /v1/run ← single execution endpoint
  └── 50+ models (z-image, H3, moss-tts, ace-step, TRELLIS, ...)
      │
      ▼
Tech Noir (Godot 4.6 — the actual game)
  ├── 5 scenes (title, test_room, maintenance, club, demo_end)
  ├── 25+ GDScript files
  ├── Data-driven architecture (.tres resources)
  └── AI-driven asset pipeline
```

## The MCP Tool Pattern (The Shared Layer)

The single most important piece. ~100 lines. Makes every Ray model
available as a self-describing MCP tool:

```python
@mcp_tool("generate_image")
async def generate_image(model: str, prompt: str, **params):
    spec = await fetch(f"{RAY}/v1/form-spec?model={model}")
    payload = merge_defaults(spec, params)
    payload["model"] = model
    payload["prompt"] = prompt
    result = await post(f"{RAY}/v1/run", json=payload)
    return extract_media(result)
```

**No hardcoded model knowledge.** The schema comes from Ray. The skill tells
the agent HOW to use the tool. The tool just executes. Three separate
concerns, three separate layers, minimal context in each.

When Ray adds a new model, it automatically appears as an MCP tool. Zero
code changes in Pux.

## Context Discipline Rules

These are non-negotiable. Write them on the wall.

1. **One skill per task** — if the agent needs two skills, split into two tasks
2. **Tools carry their own schemas** — don't duplicate in prose what the tool def already says
3. **State loaded in fragments** — never `SELECT *` when `SELECT * WHERE id = ?` suffices
4. **Agent prompts ≤ 20 lines** — role + tools + constraints, nothing else
5. **History summarized, not replayed** — "seed 42: deformed arm, fixed with neg prompt" not 50 messages
6. **The model decides the workflow** — we provide tools and skills, not scripts
7. **Every prompt addition must justify itself** — "what goes wrong without this line?"

## Why deepseek-v4-flash Makes This Work

Cheap models are MORE sensitive to context quality, not less:

```
Claude   + bloated context  =  expensive + mediocre results
Flash    + clean context    =  cheap + good results
Flash    + bloated context  =  cheap + bad results (hallucinations, avoidance)
Claude   + clean context    =  expensive + excellent results
```

Context discipline is what makes the cheap model viable. Viable cheap models
are what make 50-iteration playtester loops affordable. Affordable playtester
loops are what turn "static assets flying everywhere" into a polished game.

## Migration Sequence — post-fold verdict

### Phase 1: Build Thin (alongside Pux — no stripping yet) ✅ EXECUTED
- [x] MCP wrapper: Ray form-spec → MCP tool
- [x] Thin agent definitions: `.deepagents/agents/<name>/AGENTS.md` (dcode loads them natively)
- [x] Skill index: on-demand loading by task type
- [x] CopilotKit components → **changed**: the TUI is dcode's `run_textual_app`

### Phase 2: Prove (on Tech Noir real problems) — ongoing product work
- [ ] "Generate club scene music" (the track that failed before)
- [ ] "Playtest the club scene" (Godot headless → vision QA → vibe.json)
- [ ] "Fix the detective sprite" (background removal → consistency check)

### Phase 3: Strip (Pux bespoke parts — only after Phase 2 proves thin works) ✅ EXECUTED
- [x] Delete bespoke orchestration — gone at the 2026-08 fold
- [x] Delete bloated agent prompts (replace with 20-line defs) — the profiles tree
- [x] Delete preloaded skill system — deepagents `SkillsMiddleware` on-demand
- [x] Keep: godot-mcp, sandbox_browser — MCP servers; Docker sandbox/SurrealDB/ACP/CopilotKit per the What-Stays verdicts above
- [x] **The 2026-08-16 strip (final)**: `src/`, `profiles/`, `tests/`, the
      `pux` CLI, and every emitted-surface indirection deleted — the repo
      authors `.deepagents/` + `.mcp.json` directly; dcode is the only runtime

## The Bet

A strong model with clean context and good tools will outperform the same
model drowning in context with the same tools.

This is backed by months of evidence: Pux's most bloated agents performed
worst. Its thinnest configurations performed best. The model was ready. The
tools were ready. The bloat was holding it back.

Stop building platform. Start making the game.

## Related Documents

- `.deepagents/AGENTS.md` — the workspace instructions (the distilled org overlay)
- `.deepagents/agents/` — the 30 thin specialist definitions
- `.deepagents/skills/game-studio-workflows/` — on-demand game-studio skills
- Ray repo `game-assets/PLAN.md` — the inference + pipeline layer (proven)
- Ray repo `game-assets/skills/` — skill docs that agents consume on-demand
