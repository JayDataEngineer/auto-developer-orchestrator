# GODOT_VIA_MCP

How to drive Godot from the game-studio org via the `godot-mcp-runtime` MCP server.

## The MCP surface

The LLM sees `mcp__godot-mcp-runtime__*` tools — 36 tools covering scene
editing, node management, scripting, screenshots, input simulation, project
management, autoloads, and more. This is the SINGLE Godot surface.

**Where Godot lives doesn't matter.** The `godot-mcp-runtime` MCP server
finds Godot via `GODOT_PATH` (env var). The resolution is:

1. Godot on PATH → use it, zero download.
2. Cached binary at `.pux/godot/` → use it.
3. Download latest stable 4.x from `godotengine/godot-builds` GitHub releases.

The `godot_bootstrap.py` script (run by the `host_setup` hook in
`policy.yaml` before the MCP session opens, and also resolved into `.env`)
handles this. The LLM never sees the resolution — it just sees the tools.

## Common workflows

### Launch the editor

```
mcp__godot-mcp-runtime__launch_editor
```

Launches the Godot editor (headless or GUI depending on the environment).

### Open / create / save scenes

```
mcp__godot-mcp-runtime__create_scene
mcp__godot-mcp-runtime__attach_project
mcp__godot-mcp-runtime__save_scene
mcp__godot-mcp-runtime__get_scene_tree
mcp__godot-mcp-runtime__get_scene_dependencies
```

### Node management

```
mcp__godot-mcp-runtime__add_node
mcp__godot-mcp-runtime__duplicate_node
mcp__godot-mcp-runtime__delete_nodes
mcp__godot-mcp-runtime__get_node_properties
mcp__godot-mcp-runtime__set_node_properties
mcp__godot-mcp-runtime__connect_signal
mcp__godot-mcp-runtime__disconnect_signal
```

### Scripts

```
mcp__godot-mcp-runtime__run_script       # live GDScript eval
mcp__godot-mcp-runtime__attach_script
```

### Screenshots and input

```
mcp__godot-mcp-runtime__take_screenshot
mcp__godot-mcp-runtime__simulate_input
mcp__godot-mcp-runtime__get_ui_elements
```

### Run and debug

```
mcp__godot-mcp-runtime__run_project
mcp__godot-mcp-runtime__stop_project
mcp__godot-mcp-runtime__get_debug_output
```

### Autoloads

```
mcp__godot-mcp-runtime__list_autoloads
mcp__godot-mcp-runtime__add_autoload
mcp__godot-mcp-runtime__remove_autoload
mcp__godot-mcp-runtime__update_autoload
```

### Project management

```
mcp__godot-mcp-runtime__list_projects
mcp__godot-mcp-runtime__get_project_info
mcp__godot-mcp-runtime__get_project_settings
mcp__godot-mcp-runtime__get_project_files
mcp__godot-mcp-runtime__search_project
mcp__godot-mcp-runtime__validate
```

## Coordination

- **One scene change per call.** Don't batch parallel edits — the editor
  serializes them and you risk write conflicts.
- **Save after meaningful changes.** Don't accumulate unsaved edits.
- **Screenshot after every playtest.** QA needs the visual.

## Failure modes

| Failure | Recovery |
|---------|----------|
| Server unreachable | The host_setup hook resolves GODOT_PATH before the session opens; if the download itself failed, check network + disk space, then re-run `python3 profiles/specialists/game-studio/sandbox/godot_bootstrap.py` |
| Editor hung (timeout) | Don't retry — likely mid-modal. Surface the error and stop |
| Script error | Read the error, fix the GDScript, retry once |
