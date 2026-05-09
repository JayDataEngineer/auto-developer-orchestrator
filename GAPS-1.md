# GAPS-1: Known Integration Gaps

## Infrastructure Wiring

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 1 | **Prometheus metrics isolated** — Go backend `/metrics` on port 3847 is not scraped by cluster Prometheus at `:9090`. No remote write configured. | No cluster-level observability for orchestrator perf, token usage, error rates. | Add Prometheus remote write or register scrape target. | ❌ Not started |
| 2 | **Forge has no generation routes** — `/forge/` on cluster routes 3D/music/image gen, but Go backend only has health check. No invoke/generate endpoints. | Agents cannot generate images, 3D models, or music through the system. | Add `/api/cluster/forge/generate` route. | ❌ Not started |
| 3 | **S3 CRUD incomplete** — Bucket listing works via Garage/MinIO, but no upload/download/delete routes. | Agents cannot persist files to S3, no artifact storage to cluster. | Add `/api/cluster/storage/upload`, `/download`, `/delete` routes. | ✅ Done |
| 4 | **Infisical not wired** — Cluster has secrets manager at `:30080/infisical`, but Go backend uses env vars and local `settings.json`. | No centralized secret management; creds scattered across env files. | Add Infisical client for secret resolution. | ❌ Not started |
| 5 | **Postgres AGE not integrated** — Cluster Postgres at `:30432` has Apache AGE extension for graph queries, but backend uses Neo4j directly for org-mode graphs. | Org-mode projects must run Neo4j separately; cannot use cluster Postgres for graph. | AGE driver or AGE-over-Postgres connector for graph tools. | ❌ Not started |
| 6 | **Database is SQLite** — Primary storage is local SQLite, not cluster Postgres. | No cross-machine persistence, data lost on machine wipe. | Add Postgres driver support to `storage.Database`. | ❌ Not started |

## Performance

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 7 | **`AllTools()` makes HTTP calls every invocation** — `MultiClient.AllTools()` calls `ListTools()` on every client each time (no cache). | Every tool listing hits the MCP hub API; adds latency to `/api/tools` endpoint. | Cache tool lists after initialization; invalidate via `ResetTools()`. | ✅ Done |

## Reliability

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 8 | **MCP hub Bad Gateway** — Cluster MCP hub at `:30080/mcp/{web,media}` intermittently returns 502. | Vision chain, web research, and all MCP tools unavailable when hub is down. | Cluster-side fix (Traefik routing to MCP servers). | 🔄 Cluster ops |
| 9 | **Cluster LLM vision not detected** — `HasVision()` checks `/props` endpoint (llama-server specific). Cluster LLM at `/llm` doesn't expose this. | When cluster engine is active, `HasVision()` returns false even if model supports vision. | Add `/v1/models` fallback + model name heuristics (`vision`, `mmproj`, `multimodal`). | ✅ Done |
| 10 | **Cluster models toggle off** — Qwen3.6 LLM, TTS models (Kokoro, VibeVoice, IndexTTS, Qwen3), and Whisper ASR all show `loaded: false` intermittently. | Engine falls through to cloud providers; TTS/ASR unavailable when models unloaded. | Cluster-side model lifecycle management. | 🔄 Cluster ops |
| 11 | **Neural TTS models not loaded** — Kokoro, VibeVoice, Faster Qwen3, IndexTTS all return `loaded: false`. Only eSpeak works. | Limited to robotic eSpeak voice; no neural TTS available. | Cluster-side: load neural TTS models. | 🔄 Cluster ops |

## Configuration

| # | Gap | Impact | Fix | Status |
|---|-----|--------|-----|--------|
| 12 | **No default S3 credentials** — `S3_ACCESS_KEY` and `S3_SECRET_KEY` not set in environment. | S3 storage routes return "not configured". | Set env vars or integrate with Infisical for auto-provisioning. | 🔄 Manual setup |
| 13 | **No default Langfuse keys** — `LANGFUSE_PUBLIC_KEY` / `LANGFUSE_SECRET_KEY` not set. | Langfuse tracing disabled (graceful no-op). | Set env vars or pull from Infisical. | 🔄 Manual setup |

## Legend

- ❌ Not started — code change needed
- 🔄 Cluster ops — needs cluster-side fix (model loading, MCP routing)
- ✅ Done — already fixed in recent commits
- 📝 Documented — gap identified and documented, no action yet
