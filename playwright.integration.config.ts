import { defineConfig, devices } from '@playwright/test';

/**
 * Integration Test Configuration
 *
 * Runs against the REAL backend — no mocking.
 * Requires: Go backend running, LiteLLM accessible, at least one project.
 *
 * Run:    npx playwright test --config=playwright.integration.config.ts
 * Single: npx playwright test --config=playwright.integration.config.ts tests/integration/sse-streaming.spec.ts
 */
export default defineConfig({
  testDir: './tests/integration',
  testMatch: '*.spec.ts',
  fullyParallel: false,
  retries: 0,          // No retries — we want to see real failures
  timeout: 60_000,     // 60s per test (SSE streaming can be slow)
  workers: 1,          // Sequential — avoid backend contention
  reporter: [
    ['list'],
    ['html', { outputFolder: 'test-results/integration-report' }],
  ],

  use: {
    baseURL: 'http://localhost:5174',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 15_000,
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: {
    command: 'npm run dev',
    url: 'http://localhost:5174',
    reuseExistingServer: true,  // Don't start if already running
    timeout: 30_000,
  },
});
