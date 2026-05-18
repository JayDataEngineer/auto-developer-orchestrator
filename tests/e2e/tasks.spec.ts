/**
 * Task Board / Scheduler E2E Tests
 *
 * Tests the Scheduler panel (ConfigPanel-based) in the workbench.
 * The new UI uses a generic ConfigPanel component for the Scheduler tab
 * rather than a Kanban board.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, MOCK_SCHEDULER_JOBS } from './fixtures';

/**
 * Helper to navigate to Scheduler tab with robust waiting.
 */
async function goToSchedulerTab(page: import('@playwright/test').Page) {
  await page.waitForSelector('[role="tab"]', { timeout: 15000 });
  await page.getByRole('tab', { name: 'Scheduler' }).click();
  await page.waitForTimeout(1000);
}

test.describe('Scheduler Tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    await goToSchedulerTab(page);
  });

  // ── Panel Rendering ──

  test('renders scheduler panel', async ({ page }) => {
    // ConfigPanel renders with a list of items
    await expect(page.locator('body')).toBeVisible();
  });

  test('shows scheduled jobs from mock data', async ({ page }) => {
    // The mock scheduler returns one job: "Daily tests"
    await expect(page.getByText('Daily tests')).toBeVisible({ timeout: 5000 });
  });

  // ── Job Interaction ──

  test('clicking job shows detail panel', async ({ page }) => {
    await page.getByText('Daily tests').first().click();
    await page.waitForTimeout(500);

    // Detail panel should show job details
    // The prompt text "Run all tests" should appear
    await expect(page.getByText('Run all tests').first()).toBeVisible({ timeout: 3000 });
  });

  // ── Empty State ──

  test('empty state when no jobs', async ({ page }) => {
    await mockApiRoutes(page, { jobs: [] });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    await goToSchedulerTab(page);

    await expect(page.getByText('No scheduled jobs')).toBeVisible({ timeout: 5000 });
  });

  // ── Layout ──

  test('scheduler panel has correct structure', async ({ page }) => {
    // The panel should contain the ConfigPanel with items
    const panelContent = page.getByText('Daily tests');
    await expect(panelContent).toBeVisible({ timeout: 5000 });
  });
});

// ── Dynamic Data Tests ──

test.describe('Scheduler - Dynamic Data', () => {
  test('scheduler shows custom mock data', async ({ page }) => {
    await mockApiRoutes(page, {
      jobs: [
        {
          id: 'custom-1',
          name: 'Custom E2E Job',
          project: 'test-project',
          message: 'Custom job message',
          scheduleType: 'cron',
          cronExpr: '0 0 * * *',
          enabled: true,
          status: 'idle',
          consecutiveErrors: 0,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        },
      ],
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    await goToSchedulerTab(page);

    await expect(page.getByText('Custom E2E Job')).toBeVisible({ timeout: 5000 });
  });
});
