# E2E Tests

Playwright tests for the web frontend (`frontend/web/`).

## Structure

```
tests/e2e/
├── fixtures.ts              # Mock data, SSE stream builders, route mocking
├── real-helpers.ts           # Helpers for real-backend integration tests
├── agent.spec.ts             # Agent chat with mocked SSE
├── web-spa.spec.ts           # Web SPA chat flows
├── smoke.spec.ts             # Basic load/render checks
├── build.spec.ts             # Build artifact checks
├── desktop.spec.ts           # Desktop/VNC integration
├── edge-cases.spec.ts        # Error handling edge cases
├── error-toasts.spec.ts      # Error toast rendering
├── functional.spec.ts        # End-to-end user flows
├── navigation.spec.ts        # Page navigation
├── render.spec.ts            # Component rendering
├── tasks.spec.ts             # Task management UI
├── visual.spec.ts            # Visual regression
├── real-backend-api.spec.ts  # API tests against real backend
├── real-frontend.spec.ts     # Frontend tests against real backend
└── real-sse-streaming.spec.ts # SSE streaming against real backend
```

## Running

```bash
# Mocked (no backend needed) — default
task test-e2e

# Real backend (requires task dev running)
task test-integration

# Specific test
npx playwright test --config=tests/playwright.config.ts tests/e2e/agent.spec.ts

# Specific project only
npx playwright test --config=tests/playwright.config.ts --project=mocked
npx playwright test --config=tests/playwright.config.ts --project=real
```

## Test Files

| Prefix | Purpose |
|--------|---------|
| (no prefix) | Mocked backend via route mocking |
| `real-` | Requires running backend + frontend |
