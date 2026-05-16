# Dead Code Audit — 2026-05-16

## Category 1: Dead Go Packages (conditionally registered, never triggered)

| Package | File | Why it's dead |
|---------|------|---------------|
| `tools/face` | `internal/tools/face/` | Registered only if `cfg.DBProvider.FaceConfig()` returns true — no provider ever does. `face.yaml` tool package has zero references. |
| `tools/nlp` | `internal/tools/nlp/` | Registered only if `cfg.LLMProvider != nil` (it is), but NLP tools (`extract_entities`, `cluster_content`) are never in any worker's tool list. `nlp.yaml` tool package has zero references. |

`tools/graph` is NOT dead — registered when `cfg.DBProvider != nil` and AGE/Postgres graph queries are actively used.

## Category 2: Duplicate PlanTool

Two `create_plan` implementations exist:

| Implementation | File | Status |
|---|---|---|
| `plan.PlanTool` | `internal/tools/plan/plan.go` | **Active** — registered on CTO (orchestrator.go:119) |
| `orchestration.PlanTool` | `internal/tools/orchestration/orchestration.go:382` | **Dead** — registered on sub-agents (orchestrator.go:334) but sub-agents never call `create_plan` |

`plan.InjectActivePlan()` IS active (used in `pux_prompt.go`). Only the orchestration duplicate is dead.

## Category 3: Dead Tool Package YAMLs

No worker references these:

- `config/tool_packages/face.yaml` — face_recognize, face_batch_recognize
- `config/tool_packages/graph.yaml` — graph tools used via Go code directly, not YAML
- `config/tool_packages/nlp.yaml` — extract_entities, cluster_content

## Category 4: Legacy Roles (duplicated by workers)

All 7 `config/roles/` directories are fallback-only. New `config/workers/` defines equivalents:

| Legacy Role | New Worker |
|---|---|
| `alex/` | `shell_ops.yaml` |
| `elena/` | `vision_ops.yaml` |
| `generalist/` | `general.yaml` |
| `jake/` | `browser_ops.yaml` |
| `marcus/` | `code_ops.yaml` |
| `ryan/` | `desktop_ops.yaml` |
| `sarah/` | `researcher.yaml` |

Loader falls back to these but they're semantically duplicated.

## Category 5: Dead TUI Code

| Item | File | Why it's dead |
|---|---|---|
| `ActionBar` component | `ts-tui-ink/src/components/action-bar.tsx` | Never imported |
| `ink-scroll-view` dependency | `ts-tui-ink/package.json:17` | Never imported in source |
| `markdansi` dependency | `ts-tui-ink/package.json:20` | Never imported in source |

## Category 6: Root Orphan Files

| File | What it is | Action |
|---|---|---|
| `desktop-screenshot.png` | Temp screenshot | Delete |
| `desktop_screenshot.png` | Duplicate screenshot | Delete |
| `robot-painter-story.md` | Creative writing test | Delete |
| `test-composer.tsx` | Temp test component | Delete |
| `src/web/sb-test.mjs` | Temp Playwright script | Delete |
| `GAPS-CODE.md` | Planning doc | Move to memory/ |
| `SITE.md` | Planning doc | Move to memory/ |
| `dist-web/` | Build artifact | Delete + gitignore |
| `playwright-report-lit/` | Test report artifact | Delete + gitignore |
| `go-backend/context.test` | Unknown test file | Delete |
