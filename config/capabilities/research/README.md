# Research Capability

Deep web research — search, scrape, crawl, map.

## Two tiers

This is a **polymorphic capability**: it declares two `implementations[]` and
the `CapabilityResolver` picks one at boot based on health checks. The chosen
tier's prompt is injected into the worker's system message, and the worker's
tool list is rewritten to match. Sticky for kernel lifetime.

| Tier | Priority | Tools | Health check | When it wins |
|------|----------|-------|--------------|--------------|
| `cloud` | 1 | MCP `web` server | `mcp-available` for `web` | Cluster up, MCP hub reachable |
| `bash-ddg` | 99 | `bash` + `/sandbox/ddg.py` | `always-true` | Fallback — always wins if cloud is down |

### Cloud tier

Full web-research MCP (`mcp__web__research`, `mcp__web__scrape`, `mcp__web__crawl`,
`mcp__web__map`, etc.). See `prompts/cloud.md` for strategy.

### Bash-DDG tier

Zero-dependency DuckDuckGo HTML search via `python3 /sandbox/ddg.py`. Returns
JSON `[{title,url,snippet}]`. Snippets only — no page fetching. See
`prompts/bash-ddg.md` for the degraded-tier playbook.

## Orgs opting into the bash-ddg fallback

The bash-ddg tier needs `ddg.py` available at `/sandbox/ddg.py` inside the
sandbox. Add it to your org's `pux.yaml` via the `@shared/` resolver:

```yaml
sandbox:
  init_files:
    - "@shared/ddg.py"  # → /sandbox/ddg.py
```

The kernel does not auto-merge init_files from capabilities — orgs that import
`research` must opt in explicitly. This keeps capability code self-contained
and the manifest the single source of truth for what runs in a sandbox.

## Adding a `lite` tier (future)

When `web-lite` MCP exists, add a third implementation with priority ~50:

```yaml
  - name: lite
    type: mcp
    priority: 50
    mcp_servers: [web-lite]
    prompt_file: prompts/lite.md
    health: {kind: mcp-available, server: web-lite}
```

No code changes required — resolver picks the highest-priority live tier.
