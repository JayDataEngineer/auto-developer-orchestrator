# DRY Audit Report — 2026-05-16

## HIGH PRIORITY

| # | Pattern | Locations | Repeats | Fix | Status |
|---|---------|-----------|---------|-----|--------|
| 1 | **SSE write logic** — `json.Marshal` + `fmt.Fprintf(event:data)` duplicated instead of using existing `writeSSE()` | `pux.go:294`, `pux_sse.go:32` | 2 | Call `writeSSE()` from `pux.go` | **DONE** |
| 2 | **Project validation** — `if project == "" { JSONError(...) }` boilerplate | 25+ handler locations | 25+ | `requireProject()` / `requireProjectBody()` helpers | **DONE** (12 locations fixed) |
| 3 | **Overlay toggle pattern** — already uses `openOverlay()`/`closeAllOverlays()` helpers | `shared/src/pux-store.ts` | 8 | Already consolidated — audit overcounted | **ALREADY GOOD** |
| 4 | **Escape key handler** — most have context-specific logic (screen nav, vim mode) | 8 TUI overlay components | 4 simple | Not worth abstracting — saves 1 line per use | **SKIP** |
| 5 | **API loader pattern** — `fetch -> resp.ok -> json -> set()` + silent catch | `shared/src/pux-store.ts` | 6 | `apiLoad()` helper in store | **DONE** |
| 6 | **Config roles/ vs workers/** — legacy `roles/` was already empty, loader only reads `workers/` | `config/roles/` (empty) + `config/workers/` | 0 pairs | Deleted empty dir, fixed stale test | **DONE** |
| 7 | **Tool package file ops** — identical tool lists in `code.yaml`, `shell.yaml`, and capability counterparts | `config/tool_packages/` + `config/capabilities/` | 4 | Base `file-ops` capability imported by both | **OPEN** |

## MEDIUM PRIORITY

| # | Pattern | Locations | Fix | Status |
|---|---------|-----------|-----|--------|
| 8 | **http.Client creation** — `&http.Client{Timeout: X}` scattered across services | 15+ files | `httputil.NewClient(timeout)` factory | **OPEN** |
| 9 | **TTSRequest struct** — handler has `Service+Text` (API DTO), service has `Text` only (cluster body) | 2 files | Different responsibilities — consolidating creates coupling | **SKIP** |
| 10 | **Env var + default** — `os.Getenv() \|\| "default"` pattern | 10+ files | `configutil.EnvWithDefault(key, fallback)` | **OPEN** |
| 11 | **Silent catch blocks** — handled by `apiLoad()` for loader functions | `pux-store.ts` | Already eliminated for loaders via `apiLoad()` | **DONE** (with #5) |
| 12 | **localStorage get/set** — try-catch wrapped access | `pux-store.ts`, 4 places | `storage.get(key)` / `storage.set(key, val)` | **OPEN** |
| 13 | **mcp_servers: []** — empty field in every tool package YAML | 6 config files | Make field optional, default to `[]` | **OPEN** |

## LOW PRIORITY

| # | Pattern | Note |
|---|---------|-------|
| 14 | `json.Marshal` + error check | ~50 locations — standard Go, not worth abstracting |
| 15 | `fmt.Errorf("...: %w", err)` wrapping | ~100 locations — idiomatic, leave as-is |
| 16 | Test `assert resp.status_code == 200` | Common pytest pattern, not harmful |
| 17 | Component import boilerplate | Standard React, not a DRY problem |

## ALREADY GOOD

- `writeJSON()` and `setSSEHeaders()` in `handlers/utils.go` — widely adopted
- `openOverlay()` / `closeAllOverlays()` in `pux-store.ts` — toggle functions already use them
- `tests/python/utils/sse.py` — centralized SSE helpers
- `tests/python/conftest.py` — shared fixtures
- `internal/agents/common/common.go` — unified worker/capability loader
