# Pux — a dcode workspace

**This repo IS a [dcode](https://docs.langchain.com/oss/python/deepagents) (Deep
Agents Code) workspace.** The org tree (`profiles/`) projects onto dcode's
native surface — `.deepagents/` agents + skills and `.mcp.json` servers — via a
small compiler in `src/`, and every org runs inside dcode's own graph builder
(`create_deep_agent`) and TUI (`run_textual_app`). There is no separate harness
library, no dual track, no re-implementation: a bare `dcode` run in this repo
shows the full union roster.

- **`profiles/`** — the declarative SSOT. Per-org `org.yaml` roster,
  `AGENTS.md` CTO prompt, `agents/*.md` specialists (frontmatter: name /
  description / tools / model), `skills/`, `policy.yaml` (egress, creds,
  budgets), and `sandbox/` data.
- **`src/`** — the projection layer + launch:
  - `compiler/` — `pux sync` / `pux check` / `pux compile` (profiles/ → dcode
    surface). `emit_union` emits every rostered agent + skill across all
    non-underscore orgs; `check` drift-compares the checked-in surface.
  - `run.py` — `build_org_agent` = `create_deep_agent` (dcode's own), `launch`
    = `run_textual_app` (dcode's own TUI). Model default is dcode's own
    `_get_default_model_spec()`.
  - `profiles/` loaders (`build_system_prompt`, `discover_orgs`,
    `org_agent_slugs`, `_load_agent_spec`), `tools/` (the 11-tool registry),
    `middlewares/` (deepagents `RubricMiddleware`), `sandbox/` (deepagents
    `LocalShellBackend`), `protocol/` (.mcp.json projection), `plugins/`
    (the `pux-orgs` plugin marketplace).
- **`.deepagents/` + `.mcp.json`** — dcode's discovered surface. **Checked in
  and sync-tested**: `uv run pux check` exits 1 on any drift from the compiler
  output.
- **`infra/`** — host-side services (SurrealDB + media-mcp); `media-mcp` is
  its own submodule.

The server lane is deepagents' own: the **deepagents-acp package + a JSON
adapter file**. No repo server code, no custom overlay.

## Quick start

```bash
# 1. Sync the workspace venv (deepagents 0.7.5 + deepagents-code 0.1.55 — dcode's own pins)
uv sync

# 2. Start host-side infra (SurrealDB + media-mcp) — one command
make infra                 # or: make infra-core (SurrealDB only, lighter)
                           # GPU: MEDIA_DEVICE=cuda TORCH_VARIANT=cu124 make infra

# 3. Run an org inside dcode's own TUI
uv run python src/run.py --org coder          # dcode's run_textual_app
uv run python src/run.py --org coder --dry-run  # the plan: model, MCP servers,
                                               # subagents + tools + middleware
# …or just run dcode in the repo — .deepagents/ is the union roster
dcode
```

| Service | Port | Used by |
|---------|------|---------|
| **SurrealDB** | `localhost:8000` | deep-research-engine (ns: research, db: main), game-studio, social-media-pipeline. The shared knowledge graph — persists across runs. |
| **media-mcp** | `localhost:8101` | deep-research-engine (ASR + diarization + vision). Built from the `infra/media-mcp` submodule. |
| **ollama** | `localhost:11434` | Optional (`make infra-embeddings`). Embedding model for SurrealDB vector search. |

Orgs declare host-side service URLs in their `policy.yaml` `sandbox.env` block.
Ray cluster (LLM, TTS, 3D, music) is NOT managed here — bring your own GPU box
or set `OPENROUTER_API_KEY` for LLM fallback.

## The `pux` compiler

| Subcommand | What it does |
|------------|-------------|
| `uv run pux sync` | Emit the union dcode surface at the project root — `.deepagents/` agents + skills + merged `.mcp.json` (foreign entries preserved, `${VAR}` pass-through). |
| `uv run pux check` | Drift-check the checked-in surface (exit 1 on drift). |
| `uv run pux compile --org <name> --out <dir>` | Emit one org's dcode layout into a staging dir. |
| `uv run pux compile --marketplace --out <dir>` | Emit every org as a dcode plugin + the `pux-orgs` marketplace catalog (`dcode plugin marketplace add <dir> && dcode plugin install <org>@pux-orgs`). |

## Org system

Orgs are markdown-driven. Drop a directory under `profiles/<name>/`:

```
profiles/<name>/
├── AGENTS.md       # CTO system prompt body (prose only — no frontmatter)
├── org.yaml        # specialist roster: `agents: [slug, …]`
├── policy.yaml     # optional: egress ACLs, creds, budgets
├── agents/*.md     # one file per specialist (frontmatter: name/description/tools/model)
└── skills/         # org skills (union-emitted to .deepagents/skills/)
```

`uv run python src/run.py --org <name>` appends the org's prompt to the base
system prompt — the main agent becomes that org's CTO and delegates to its
declared specialists via the `task` tool. Cross-org agents live under
`profiles/_shared/agents/`; an org specializes one by dropping a same-named
`<slug>.md` in its own `agents/` dir. Underscore-prefixed orgs (`_shared`,
`_demo`) are internal and never emitted.

**`coder` is the Claude-Code-equivalent coding org.** Its `profile.yaml` opts
into the **`RubricMiddleware` verify-gate**: after the agent implements, a
grader sub-agent runs the test suite + reads the diff + greps for regressions
and gates the deliverable on a ship rubric (`satisfied` / `needs_revision` /
revise, up to `max_iterations`).

## Tool surface

The sandbox is **deepagents' `LocalShellBackend`** (the same backend dcode's
CLI uses — no container, no gateway). The 11 tools under `src/tools/` are
registry-keyed and resolved per subagent `tools:` refs:

| Tool | Backed by |
|------|----------|
| `python` | `LocalShellBackend` — execute `python3 -c` (src/tools/python.py) |
| `skills` | host FS `profiles/_shared/skills/` + each `profiles/<name>/skills/` |
| `describe_image` | driving-model PRIMARY (multimodal) → in-sandbox ONNX fallback |
| `multimodal` / `multimodal_mega` | image/audio/video + a PROMPT → the multimodal model; honest errors, tiered waterfall, **no silent fallback** |
| `desktop_screenshot` / `_click` / `_type` / `_key` | `xdotool`-driven desktop observation |
| `grader` | rubric evaluation (used by `RubricMiddleware`) |

An `mcp:` ref in a subagent must name a server the org has actually declared —
unknown refs raise at build time; the org's declared capability surface can
never silently shrink.

## Architecture

```
dcode (the SDK, unpinned by us)
 ├─ create_deep_agent          ← src/run.py build_org_agent (dcode's own graph builder)
 ├─ run_textual_app            ← src/run.py launch (dcode's own TUI)
 ├─ LocalShellBackend          ← src/sandbox/local.py (dcode's own sandbox backend)
 ├─ RubricMiddleware           ← src/middlewares/rubric.py (dcode's own rubric gate)
 └─ _get_default_model_spec    ← src/run.py model default (dcode's own config)
        │
        └── profiles/  ──(compiler)──▶  .deepagents/  .mcp.json   (checked in)
```

The compiler (`src/compiler/`) is pure data → format projection: profiles/
tree → dcode's file surface. Zero monkey patches, zero re-implementations.

The server lane (when a server is needed) is deepagents' own **ACP package +
a JSON adapter file** pointing at the dcode-native graph — never custom code.

## Tests

```bash
uv run pytest -q        # workspace suite: compiler, loaders, tools, dcode-native
uv run pux check        # the checked-in .deepagents/ + .mcp.json match the compiler
```

## History

This repo previously hosted a dual-track harness (a custom deepagents graph
assembly + a custom Agent Protocol server overlay) pinned as a git submodule.
On 2026-08-16 the submodule, the overlay, and the legacy lanes were deleted:
the repo became the dcode workspace it is now. `orgs/` was renamed
`profiles/` (folder-only; the concept vocabulary — `--org`, `org.yaml`,
`org_agent_slugs`, `discover_orgs`, `PUX_ORG_PATHS`, `pux-orgs` — is
unchanged). See `docs/` for the engineering history.
