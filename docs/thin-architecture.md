# Thin Architecture — The Simplification Mandate

> **Status:** ACTIVE DECISION (2026-08-03). Supersedes the accumulation
> approach of the prior 8 months. Every bespoke addition is suspect until
> proven necessary. The goal is the thinnest possible layer between a smart
> model and productive work.

## The Decision

Pux becomes a **thin wrapper over upstream deepagents.** We stop building
custom agent infrastructure that the upstream now does better. We strip
accumulated complexity. We keep only what upstream cannot provide: sandbox
isolation, game-dev-specific tools, and the CopilotKit interface layer.

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

## What Stays (Infrastructure With Unique Value)

| Component | Why It Stays |
|-----------|-------------|
| Docker sandbox | Isolated execution — upstream doesn't provide this |
| SurrealDB integration | Shared state across agents and sessions |
| godot-mcp bridge | Godot editor control — game-specific, no upstream equivalent |
| ACP (Agent Client Protocol) | UI-to-runtime separation — enables CopilotKit |
| CopilotKit export | Web UI for visual creative work — text interfaces can't do game dev |
| Pux harness (pux-harness/) | The deepagents graph builder + CLI — this IS the thin wrapper |

## What Gets Stripped (Bloat Replaced by Upstream or Deleted)

| Component | Replacement |
|-----------|-------------|
| Custom agent orchestration | deepagents graphs (the upstream pattern) |
| 200-line CTO overlay prompts | 20-line thin agent definitions |
| Preloaded skill system | On-demand skill loading by task type |
| Custom TUI (TOAD) | CopilotKit web UI |
| Bespoke delegation protocol | deepagents task() delegation |
| Accumulated "improvements" from 8 months | Deletion. The model doesn't need them. |
| Forge API client | Ray's `/v1/run` via MCP tool wrapper |
| Hardcoded model knowledge | Ray's `/v1/form-spec` — schema discovery |

## The Target Stack

```
CopilotKit (web UI)
  ├── Thin custom components (<AssetGallery>, <SpriteReviewer>, 
  │   <SubagentTree>, <GameViewport>, <VibeDashboard>)
  └── Chat → delegates to agents
      │
      ▼
deepagents (agent runtime — upstream, thin wrappers)
  ├── Director (orchestrates, delegates, decides iterate/yield)
  ├── Art Specialist (deepseek-v4-flash — cheap iteration)
  ├── Playtester (deepseek-v4-flash — runs Godot, reports bugs)
  ├── Audio Specialist (deepseek-v4-flash)
  ├── Programmer (Claude or flash)
  └── Narrative (flash)
      │
      ▼
MCP Tools (self-describing — schema from Ray form-spec)
  ├── generate_image(model, prompt, **params) → /v1/run
  ├── generate_audio(model, prompt, **params) → /v1/run
  ├── generate_video(model, prompt, **params) → /v1/run
  └── [new models appear automatically via form-spec discovery]
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

The single most important new piece. ~100 lines. Makes every Ray model
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
code changes in Pux. This is the opposite of the current Forge client, which
hardcodes endpoints and parameters.

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

## Migration Sequence

### Phase 1: Build Thin (alongside Pux — no stripping yet)
- [ ] MCP wrapper: Ray form-spec → MCP tool (~100 lines)
- [ ] Thin agent definitions: 20-line deepagents graphs
- [ ] Skill index: on-demand loading by task type
- [ ] CopilotKit components: SpriteReviewer, SubagentTree, AssetGallery
- [ ] Fix endpoints: game-studio org → localhost:33080

### Phase 2: Prove (on Tech Noir real problems)
- [ ] "Generate club scene music" (the track that failed before)
- [ ] "Playtest the club scene" (Godot headless → vision QA → vibe.json)
- [ ] "Fix the detective sprite" (background removal → consistency check)
- [ ] Measure: does thin outperform Pux's current bloat?

### Phase 3: Strip (Pux bespoke parts — only after Phase 2 proves thin works)
- [ ] Delete bespoke orchestration
- [ ] Delete bloated agent prompts (replace with 20-line defs)
- [ ] Delete preloaded skill system
- [ ] Keep: Docker sandbox, SurrealDB, godot-mcp, ACP, harness
- [ ] Pux becomes what it should always have been: thin deepagents wrapper + infra

## The Bet

A strong model with clean context and good tools will outperform the same
model drowning in context with the same tools.

This is backed by months of evidence: Pux's most bloated agents performed
worst. Its thinnest configurations performed best. The model was ready. The
tools were ready. The bloat was holding it back.

Stop building platform. Start making the game.

## Related Documents

- `game-studio/AGENTS.md` — will be rewritten to 20-line thin definitions
- `game-studio/skills/` — skills stay, loading mechanism changes to on-demand
- Ray repo `game-assets/PLAN.md` — the inference + pipeline layer (proven)
- Ray repo `game-assets/skills/` — skill docs that agents consume on-demand
