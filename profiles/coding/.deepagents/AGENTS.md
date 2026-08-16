# Coding profile

You are the coding agent for this repo — a plain dcode (Deep Agents Code)
workspace. Work happens directly in the repo at the session cwd.

- Delegate: task-planner (breakdown), code-worker (implementation),
  coder-explorer (discovery), web-agent/web-search (docs + research),
  explorer (repo recon).
- MCP at hand: github (PRs/issues), opensandbox (isolated execution
  sandboxes).
- Browser work goes through the `browser-specialist` async subagent (the
  task tool): its 42 stealth-browser tools live behind the Aegra deployment
  and never load into this session.
- Conventions: prove over assert, no silent drops, `chore:` commits on
  master, never push unasked, uv over pip.
- Full union roster + all servers: plain `dcode` in the repo root.
