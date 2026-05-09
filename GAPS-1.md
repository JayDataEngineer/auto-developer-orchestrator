# GAPS-1: Known Integration Gaps

## Infrastructure Wiring

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 1 | **Prometheus metrics isolated** — Go backend `/metrics` on port 3847 not pushed to cluster. | No cluster-level observability for orchestrator perf, token usage, error rates. | Prometheus Pusher pushes to cluster Pushgateway every 30s. | ✅ Done |
| 2 | **Forge has no generation routes** — Only health check exists, no generate endpoint. | Agents cannot generate images, 3D models, or music. | `POST /api/cluster/forge/generate` with mode/prompt/params. | ✅ Done |
| 3 | **S3 CRUD incomplete** — Bucket listing works, but no upload/download/delete routes. | Agents cannot persist files to S3. | Full CRUD: upload, download, delete, list objects. | ✅ Done |
| 4 | **Infisical not wired** — Cluster has secrets manager, backend uses env vars. | No centralized secret management. | `services.InfisicalClient` resolves secrets from cluster on startup. | ✅ Done |
| 5 | **Postgres AGE not integrated** — Backend uses Neo4j for graph queries. | Org-mode projects must run Neo4j separately. | `services.AGEClient` runs Cypher via Postgres AGE extension. | ✅ Done |
| 6 | **Database is SQLite** — Primary storage is local SQLite, not cluster Postgres. | No cross-machine persistence. | `storage.NewDatabase()` detects `postgres://` URLs and uses pgx driver. | ✅ Done |

## Performance

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 7 | **`AllTools()` makes HTTP calls every invocation** — no cache. | Every tool listing hits MCP hub API. | `InitializeAll()` caches tool list. `ResetTools()` for invalidation. | ✅ Done |

## Reliability

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 8 | **MCP hub Bad Gateway** — Cluster MCP hub at `:30080/mcp/{web,media}` intermittently returns 502. | Vision chain, web research, and all MCP tools unavailable when hub is down. | Cluster-side fix (Traefik routing to MCP servers). | 🔄 Cluster ops |
| 9 | **Cluster LLM vision not detected** — `HasVision()` checks `/props` endpoint only. | When cluster engine active, vision returns false. | Two-phase probe: `/props` → `/v1/models` keyword scan. | ✅ Done |
| 10 | **Cluster models toggle off** — Qwen3.6 LLM, TTS, Whisper ASR show `loaded: false`. | Engine falls through to cloud providers. | Cluster-side model lifecycle management. | 🔄 Cluster ops |
| 11 | **Neural TTS models not loaded** — Only eSpeak works. | Limited to robotic voice. | Cluster-side: load neural TTS models. | 🔄 Cluster ops |

## Configuration

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 12 | **No default S3 credentials** | S3 storage routes return "not configured". | Set in `.env` or Infisical auto-resolves. | ✅ `.env` populated |
| 13 | **No default Langfuse keys** | Langfuse tracing disabled. | Both keys set in `.env`. | ✅ `.env` populated |
| 14 | **DATABASE_URL hardcoded to SQLite** — Postgres URL would crash. | Cannot switch to cluster Postgres. | `storage.DetectDriver()` selects pgx for `postgres://` URLs. | ✅ Done |

## Legend

- ✅ Done — fixed in codebase
- 🔄 Cluster ops — needs cluster-side fix (model loading, MCP routing, Pushgateway)
- ❌ Not started — code change needed
