import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright E2E Test Configuration
 *
 * Two projects:
 *   - mocked: Route mocking, no backend needed (default)
 *   - real: Real backend + frontend required
 *
 * Run mocked (default):  npx playwright test
 * Run real-backend only: npx playwright test --project=real
 * Run specific:          npx playwright test tests/e2e/agent.spec.ts
 * Run with UI:           npx playwright test --ui
 */
export default defineConfig({
  testDir: './e2e',
  testMatch: '*.spec.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 1,
  workers: 1,
  reporter: [
    ['html', { outputFolder: 'playwright-report' }],
    ['json', { outputFile: 'test-results/results.json' }],
    ['list']
  ],

  use: {
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 10000,
  },

  projects: [
    {
      name: 'mocked',
      testMatch: /(?<!^real-).*\.spec\.ts$/,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: 'http://localhost:5175',
      },
      webServer: {
        command: 'npx vite --config frontend/web/vite.config.ts --port 5175',
        url: 'http://localhost:5175',
        reuseExistingServer: !process.env.CI,
        timeout: 30_000,
      },
    },
    {
      name: 'real',
      testMatch: /real-.*\.spec\.ts$/,
      retries: 0,
      timeout: 60_000,
      use: {
        ...devices['Desktop Chrome'],
        baseURL: 'http://localhost:5174',
        actionTimeout: 15_000,
      },
      webServer: {
        command: 'npm run dev',
        url: 'http://localhost:5174',
        reuseExistingServer: true,
        timeout: 30_000,
      },
    },
  ],
});
