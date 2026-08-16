# GODOT_VIA_MCP

How to drive Godot from the game-studio org via the `godot-mcp-runtime` MCP server.

## The MCP surface

The LLM sees `godot-mcp-runtime_*` tools — 36 tools covering scene
editing, node management, scripting, screenshots, input simulation, project
management, autoloads, and more. This is the SINGLE Godot surface.

**Where Godot lives doesn't matter.** The `godot-mcp-runtime` MCP server
finds Godot via `GODOT_PATH` (env var). The resolution is:

1. Godot on PATH → use it, zero download.
2. Cached binary at `.cache/godot/` → use it.
3. Download latest stable 4.x from `godotengine/godot-builds` GitHub releases.

The `godot_bootstrap.py` script (run by the `host_setup` hook in
`policy.yaml` before the MCP session opens, and also resolved into `.env`)
handles this. The LLM never sees the resolution — it just sees the tools.

## Common workflows

### Launch the editor

```
godot-mcp-runtime_launch_editor
```

Launches the Godot editor (headless or GUI depending on the environment).

### Open / create / save scenes

```
godot-mcp-runtime_create_scene
godot-mcp-runtime_attach_project
godot-mcp-runtime_save_scene
godot-mcp-runtime_get_scene_tree
godot-mcp-runtime_get_scene_dependencies
```

### Node management

```
godot-mcp-runtime_add_node
godot-mcp-runtime_duplicate_node
godot-mcp-runtime_delete_nodes
godot-mcp-runtime_get_node_properties
godot-mcp-runtime_set_node_properties
godot-mcp-runtime_connect_signal
godot-mcp-runtime_disconnect_signal
```

### Scripts

```
godot-mcp-runtime_run_script       # live GDScript eval
godot-mcp-runtime_attach_script
```

### Screenshots and input

```
godot-mcp-runtime_take_screenshot
godot-mcp-runtime_simulate_input
godot-mcp-runtime_get_ui_elements
```

### Run and debug

```
godot-mcp-runtime_run_project
godot-mcp-runtime_stop_project
godot-mcp-runtime_get_debug_output
```

### Autoloads

```
godot-mcp-runtime_list_autoloads
godot-mcp-runtime_add_autoload
godot-mcp-runtime_remove_autoload
godot-mcp-runtime_update_autoload
```

### Project management

```
godot-mcp-runtime_list_projects
godot-mcp-runtime_get_project_info
godot-mcp-runtime_get_project_settings
godot-mcp-runtime_get_project_files
godot-mcp-runtime_search_project
godot-mcp-runtime_validate
```

## Coordination

- **One scene change per call.** Don't batch parallel edits — the editor
  serializes them and you risk write conflicts.
- **Save after meaningful changes.** Don't accumulate unsaved edits.
- **Screenshot after every playtest.** QA needs the visual.

## Failure modes

| Failure | Recovery |
|---------|----------|
| Server unreachable | The host_setup hook resolves GODOT_PATH before the session opens; if the download itself failed, check network + disk space, then re-run `python3 plugins/game-studio-workflows/skills/game-studio-workflows/scripts/godot_bootstrap.py` |
| Editor hung (timeout) | Don't retry — likely mid-modal. Surface the error and stop |
| Script error | Read the error, fix the GDScript, retry once |
