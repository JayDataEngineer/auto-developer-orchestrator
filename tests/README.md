# Tests

## Structure

```
tests/
├── e2e/                    # Playwright tests for web frontend
│   ├── fixtures.ts         # Mock data + SSE builders + route mocking
│   ├── real-helpers.ts     # Helpers for real-backend tests
│   ├── *.spec.ts           # Mocked tests (no backend)
│   └── real-*.spec.ts      # Real-backend integration tests
├── python/                 # Python pytest suite
│   ├── api/                # REST API tests (no LLM)
│   ├── sse/                # SSE streaming contract tests
│   ├── agent/              # Agent loop tests (requires LLM)
│   ├── browser/            # Browser automation tests
│   ├── desktop/            # Desktop/xdotool automation
│   ├── frontend/           # WebUI chat tests (Playwright)
│   └── tui/                # TUI visual tests (requires task tui-visual)
└── playwright.config.ts    # Playwright config (mocked + real projects)
```

## Commands

| Command | What it runs |
|---------|-------------|
| `task test` | All unit tests (Go + JS + TUI) in parallel, then E2E |
| `task test-go` | Go backend tests |
| `task test-js` | Frontend shared tests (vitest) |
| `task test-tui` | TUI tests (bun test) |
| `task test-e2e` | Playwright E2E (mocked backend) |
| `task test-integration` | Playwright E2E (real backend) |
| `task test-python` | Python pytest suite (all domains) |
| `task test-tui-e2e` | TUI visual E2E (requires task tui-visual) |
| `task test-webui-e2e` | WebUI chat E2E (Playwright + route mocking) |

## Frameworks

- **Go**: `go test` — co-located with source in `backend/internal/`
- **JS/TS**: vitest (shared) + bun:test (TUI)
- **Python**: pytest with markers (`api`, `sse`, `agent`, `browser`, `desktop`, `tui`)
- **Playwright**: web frontend tests (mocked + real backend)
