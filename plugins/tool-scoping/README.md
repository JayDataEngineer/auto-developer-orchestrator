# tool-scoping — the L2.5 MCP bridge for dcode

Scopes MCP tools away from no-MCP subagents **today**, through deepagents'
model-keyed harness profiles — the one per-subagent seam dcode can express
before the upstream `tools:` frontmatter PR lands.

- A subagent opts in with frontmatter `model: openai:glm-5-turbo` (a real
  model on the same z.ai gateway; `.env` mirrors it as
  `OPENAI_BASE_URL`/`OPENAI_API_KEY`).
- This package registers a harness profile under that key (entry-point group
  `deepagents.harness_profiles`) whose `excluded_tools` denies **every MCP
  tool name the workspace can serve** — scoped agents keep the built-ins
  (`execute`/`read`/`write`/`task`) and lose all MCP tools.
- The deny-list **fails open**, so `make scoping-check`
  (`profiles/scoping_check.py`) is the tripwire: it rebuilds every profile,
  asserts each scoped agent's effective MCP set is empty, that unscoped
  agents carry no exclusion middleware, and warns on declared-but-empty
  servers.

Install (this is a Python entry-point plugin, not a `dcode plugin install`
marketplace item — the marketplace carries skills, this needs the tool
venv's importlib.metadata):

```bash
uv pip install --python "$(uv tool dir)/deepagents-code/bin/python" ./plugins/tool-scoping
```

Re-run that after any dcode upgrade (same rule as `plugins/opensandbox`).

Maintain: after any `.mcp.json` change, `allowedTools` trim, or server
upgrade, regenerate, **reinstall** (the venv holds a copy, not a link —
editing the repo YAML alone changes nothing at runtime), and tripwire:

```bash
$(uv tool dir)/deepagents-code/bin/python plugins/tool-scoping/regenerate.py
uv pip install --python "$(uv tool dir)/deepagents-code/bin/python" ./plugins/tool-scoping
make scoping-check
```

Scoped agents, and why the list is short — agent files are shared across
profiles (symlink union), so scoping applies **everywhere an agent is
rostered**: `game-studio-docs-writer`, `task-planner` (game + coding),
`web-agent` (coding; its browser work rides the browser-specialist async
subagent, which is middleware, not MCP). `web-search` deliberately stays
unscoped — research needs its web_research tools.

Full mechanics, limits (fail-open, model-keyed, `general-purpose` hole,
`equibles` uncovered) and the retirement plan live in
`docs/isolation-patterns.md` ("The L2.5 bridge").
