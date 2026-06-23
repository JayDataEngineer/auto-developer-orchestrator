# Self-Evolving Script Toolkit

You have `make_script`, `run_script`, `list_scripts`, `edit_script`, `read_script`, and `remove_script` tools to build a persistent Python toolkit. Scripts you author land in `/sandbox/workspace/scripts/` and survive across sessions.

## When to write a script

Write a script when:
- You're about to inline more than ~5 lines of Python in bash
- You'll call the same logic more than once
- A website changed its selectors and you need to update behavior in one place

Don't write a script for:
- One-off shell commands (just use bash)
- Anything that needs the full Go toolchain (use file_edit on Go files)
- Trivial pipelines (`jq` + `grep`)

## Author with hints

Every reusable script should carry an AI-authored `hints` section. Hints tell future-you (and future agents) when to reach for the helper, what it returns, and what pitfalls to avoid — without re-reading the code.

```python
"""
Get current stock price.

hints:
  - Use when the user asks for a ticker's current price.
  - Returns float; -1.0 means market closed.
  - Pitfall: tickers with dots (BRK.B) need URL encoding.
"""
```

Pass hints via `make_script(name=..., description=..., code=..., hints="...")` — multi-line, one bullet per line. The model on the next session sees them in `<available_scripts>` and `list_scripts` output, so it can pick the right helper without re-reading the code.

## Peek before you call

`list_scripts` shows name + description + first hint. `read_script(name)` returns the full hints + code. Call `read_script` before `run_script` when you're not 100% sure what the script does.

## Prefer edit over rewrite

When a script breaks (selector changed, API moved), use `edit_script` — your description and hints are preserved. `remove_script` + `make_script` wipes hints and forces you to re-author them.
