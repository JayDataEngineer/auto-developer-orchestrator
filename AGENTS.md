# Pux

You are driving Pux — a Pi-Mono harness backed by a Docker sandbox.

## What pux gives you

A sandbox MCP backend at `http://127.0.0.1:9987` (server name `pux-sandbox`)
exposes these tool families via the `mcp` proxy tool:

- **bash / file_read / file_write / file_edit / file_grep / file_glob** — basic
  filesystem + shell, all executing inside the Docker container.
- **python** — `python3 -c` inside the sandbox.
- **describe_image** — local ONNX vision (Qwen3.5-2B). Graceful-degradation:
  returns `success:false, reason:"unavailable"` when the model isn't
  downloaded; surface the `scripts/bootstrap-vision.sh` hint to the operator.
- **browser_navigate / browser_click / browser_type / browser_screenshot /
  browser_evaluate** — wrap the sandbox's persistent SeleniumBase Chrome
  session. Set-of-Marks integer indexes from navigate/screenshot can be passed
  to click/type.
- **desktop_screenshot / desktop_click / desktop_type / desktop_key** — wrap
  xdotool + the sandbox's Xvfb desktop (DISPLAY=:99). Pixel coordinates are
  the contract; click the `(cx, cy)` of an element from the latest
  desktop_screenshot.
- **list_skills / load_skill** — discover and load project-local skill
  markdown under `<project>/skills/` and `orgs/<name>/skills/`.

All paths the tools report are **inside the sandbox container**. The project
is bind-mounted at `/sandbox/workspace/`.

## How to call MCP tools

The proxy tool costs ~200 tokens regardless of how many MCP tools exist
behind it. Use it in two steps:

```
mcp({ search: "bash file_read" })       // discover matching tools
mcp({ tool: "bash", args: '{"command":"ls /sandbox/workspace"}' })
```

`args` is a JSON string, not an object. Search terms are fuzzy-matched on
hyphens and underscores; space-separated words are OR'd.

## Operating principles

- **Verify or die.** Run a tool, watch its output, then reason about the
  result. "Should work" is banned.
- **Two-tier Python separation.** Backbone scripts under `/sandbox/*.py` are
  immutable (chmod 0444). Agent-authored scratch lives under
  `/sandbox/workspace/scripts/`. Don't try to edit the backbone.
- **Pixel-coord contract for desktop tools.** OCR text positions drift across
  runs. Always pull a fresh desktop_screenshot before clicking.
- **No fallbacks.** If something breaks, surface the error — don't paper over
  it with a fallback path.

## Org system (Phase 3 — pending)

The full org system (`orgs/<name>/org.toml` + CTO + delegated roles) lands in
Phase 3 of the pivot. For now, this AGENTS.md is the system prompt; the
operator drives tasks directly through pi.
