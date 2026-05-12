import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright config for the Lit web SPA.
 *
 * Tests the shared-code web UI against a mocked backend.
 * Run with: npx playwright test --config=playwright-lit.config.ts
 */
export default defineConfig({
  testDir: './tests/e2e-lit',
  testMatch: '*.spec.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 1,
  workers: 1,
  reporter: [['list'], ['html', { outputFolder: 'playwright-report-lit' }]],
  use: {
    baseURL: 'http://localhost:5175',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    actionTimeout: 10000,
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: {
    command: 'npx vite --config src/web/vite.config.ts --port 5175',
    url: 'http://localhost:5175',
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
