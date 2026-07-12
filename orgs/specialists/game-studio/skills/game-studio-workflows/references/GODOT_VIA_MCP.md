# GODOT_VIA_MCP

How to drive the Godot editor from a sandbox via the IvanMurzak/Godot-MCP HTTP bridge.

## Pre-flight: Check the Bridge

```bash
python3 /sandbox/godot_client.py health
```

Responses:
- JSON with `status: ok` + editor state → bridge is up, Godot editor is connected
- `GODOT_MCP_DOWN` → server not reachable OR Godot editor not running with plugin

**On `GODOT_MCP_DOWN`:** Do NOT retry in a tight loop. The CTO needs to know. Return cleanly with a "GODOT_MCP_DOWN — falling back to CLI" message so it can route to gameplay_programmer for headless test runs instead.

## Common Workflows

### Open a scene for editing

```bash
python3 /sandbox/godot_client.py scene-open res://scenes/player.tscn
```

### Read a script (before editing)

```bash
python3 /sandbox/godot_client.py script-read res://scripts/player_controller.gd
```

### Update a script

Write the new content to a file first, then push:

```bash
cat > /sandbox/workspace/edits/player_controller.gd << 'EOF'
extends CharacterBody2D

# new content here
EOF

python3 /sandbox/godot_client.py script-update \
    res://scripts/player_controller.gd \
    --content /sandbox/workspace/edits/player_controller.gd
```

### Save the active scene

```bash
python3 /sandbox/godot_client.py scene-save
```

### Capture viewport for QA

```bash
python3 /sandbox/godot_client.py screenshot-viewport \
    --out /sandbox/workspace/qa/cycle-1/viewport.png
```

### Inspect runtime errors after a playtest

```bash
python3 /sandbox/godot_client.py runtime-errors-get
python3 /sandbox/godot_client.py console-logs
```

### Escape hatch — call any of the 39 tools

```bash
python3 /sandbox/godot_client.py call node-find --args '{"path":"res://scenes/player.tscn/Player"}'
python3 /sandbox/godot_client.py call resource-find --args '{"type":"Sprite2D"}'
python3 /sandbox/godot_client.py call editor-selection-get --args '{}'
```

The full tool list (39 tools, 11 families): scene-*, node-*, script-*, screenshot-*, resource-*, editor-*, console-*, reflection-*, runtime-errors-*. See [IvanMurzak/Godot-MCP](https://github.com/IvanMurzak/Godot-MCP) for argument schemas.

## Coordination with the CTO

- **Always health-check first.** Don't burn a delegation round on a dead server.
- **One scene change per call.** Don't batch 5 script-update calls in parallel — the editor serializes them anyway and you risk write conflicts.
- **Save after meaningful changes.** Don't accumulate 10 unsaved edits — if the editor crashes, all are lost.
- **Screenshot after every playtest.** QA needs the visual; vibes are scored from screenshots, not logs.

## Failure Modes

| Failure | Recovery |
|---------|----------|
| `GODOT_MCP_DOWN` | **Fall back to the headless harness** — see below. No editor bridge needed. |
| Editor hung (timeout on call) | Don't retry — likely the editor is mid-modal. Surface the error and stop the cycle |
| Script update rejected (parse error) | Read the error, fix the GDScript, retry once. Don't blind-retry. |
| Screenshot file not written | Check disk space; use `godot_test_screenshot` headless instead |

## MCP-Bridge-Down Fallback: Headless Godot Harness

When `godot_client.py health` returns `GODOT_MCP_DOWN`, the sandbox has a
**backup path** that downloads Godot from GitHub and runs it headlessly — no
editor, no MCP server needed.

### Step 1: Bootstrap the binary (once)

```
pux_sandbox_godot_bootstrap
```

Downloads the latest stable Godot 4.x Linux x86_64 binary from
`godotengine/godot-builds` GitHub releases into `/sandbox/.bin/`. Resolution
order: `godot` on PATH → cached binary → download. Idempotent — a warm cache
or PATH hit is zero network.

### Step 2: Use headless tools

| Tool | What it does | Godot flag |
|------|-------------|------------|
| `godot_test_version` | Print the binary version | `--version` |
| `godot_test_syntax` | Syntax-check all `.gd` files | `--check-gdscript` |
| `godot_test_import` | Import assets (generates `.godot/imported/`) | `--import` |
| `godot_test_screenshot` | Render a scene + capture PNG | `--screenshot` |
| `godot_test_validate` | Validate project (script errors) | `--editor --quit` |
| `godot_test_run` | Run GUT unit tests headlessly | `-s <script>` |

All tools pass `--headless` automatically. The same binary that runs headless
can also export builds (`--export-release`) if needed.

## What This Bridge Does NOT Do

- Run the game head-to-head with the previous build (use godot CLI for A/B)
- Author new scenes from scratch (use Godot editor manually)
- Profile performance (use Godot's built-in profiler via the editor)
- Package exports (use godot CLI `--export-release`)
