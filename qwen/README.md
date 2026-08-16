# Local Qwen — Qwen3.8-27B on the 4090, isolated to this repo

The workspace can run entirely on the local model:

```bash
make qwen                       # start the server (auto-clears GPU, ~50s load)
dcode -M local-qwen:qwen3.8-27b # any session, any profile
$(DCODE_PY) profiles/run.py coding -M local-qwen:qwen3.8-27b   # a profile on it
make qwen-status                # port, ctx, live clients, VRAM fit
make qwen-stop
```

## What this folder is (and is not)

- **Isolated**: port `8388` (8080 belongs to the OpenSandbox server) and all
  runtime state — client registry, locks, `state.json`, `server.log` — lives
  in `qwen/state/` (gitignored). Nothing touches `~/.cache/claude-qwen/`.
- **Not forked**: `qwen/qwen` execs `~/.claude/setup/qwen/qwen-server.sh`
  verbatim with the env contract (`QWEN_PORT`, `QWEN_CACHE_DIR`,
  `QWEN_LLAMA_BIN`). The manager's source of truth stays in the claude setup
  repo; this folder only points it at this workspace's port and state.
- **Shared assets, not duplicated**: the 20 GB GGUF
  (`/mnt/data/models/unsloth/Qwen3.8-27B-GGUF/Qwen3.8-27B-UD-Q5_K_XL.gguf`)
  and the CUDA llama.cpp build (`~/.cache/claude-qwen/llama.cpp/`).
- **dcode provider**: `~/.deepagents/config.toml`
  `[models.providers.local-qwen]` → `ChatOpenAI` against
  `http://127.0.0.1:8388/v1` (OpenAI-compatible endpoint; auth ignored —
  `QWEN_API_KEY` in `.env` exists only because the provider requires a
  non-empty env var). Custom `class_path` providers resolve for the main
  agent — `-M local-qwen:qwen3.8-27b` from the TUI or `profiles/run.py`.

## GPU contract (the manager's, unchanged)

ComfyUI (~18 GB resident) is asked via `POST /free` to unload weights — its
server stays alive. If that is not enough, the mode stops the ComfyUI
container and ALWAYS restarts it after; if still not enough, it fails loud
naming the holding PIDs. It never kills anything but its own llama-server.

**One llama-server fits the 4090**: do not run `claude-qwen` (port 8080) and
`make qwen` (8388) simultaneously.

## The llama.cpp boolean-schema patch (`patches/llamacpp-bool-schemas.patch`)

llama.cpp's JSON-Schema→grammar converter (as of the vendored checkout
`9d57ce4`) rejects boolean subschemas with
`JSON schema conversion failed: Unrecognized schema: true`. JSON Schema says
`true` is a schema that accepts any value (e.g. pydantic emits
`{"items": true}` for free-form array items), and several MCP tools in this
workspace's wire format carry them — so every tool-carrying dcode turn on
llama-server 400'd. The patch (11 lines, in `visit()`):

- `true` → the `value` primitive (any JSON value) — same rule the converter
  already uses for schema-less objects;
- `false` → fails loudly (`Boolean schema \`false\` accepts no value`) rather
  than emitting a wrong grammar — GBNF has no unsatisfiable production.

It is applied to the **shared** build (`~/.cache/claude-qwen/llama.cpp`) and
is upstream-quality (aegra-style): if that checkout is ever re-cloned or
updated (`install-llama`), re-apply and rebuild:

```bash
make qwen-patch    # patch --forward + rebuild llama-server
```

Proven: the offending shapes pass on both endpoints (`/v1/chat/completions`
and `/v1/messages`), and the real session that failed — 190 MCP tools bound,
including `surreal_run`'s `{"items": true}` — completes with the model turn.

