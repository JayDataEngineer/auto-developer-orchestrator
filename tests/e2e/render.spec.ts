/**
 * Render E2E Tests
 *
 * Tests that key components render without crashing and
 * produces screenshot artifacts for visual confirmation.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes } from './fixtures';

test.describe('Frontend Render Tests', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
  });

  test('should render main page without crashing', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // No uncaught errors visible on the page
    const errorText = page.getByText(/Uncaught TypeError|cannot access property|Error:/i);
    await expect(errorText).not.toBeVisible({ timeout: 5000 });

    // Root container should be visible
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('should render Agent tab content', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Agent tab is default — should show textarea or empty state
    const textarea = page.locator('textarea');
    const emptyState = page.getByText('Pi Agent Ready');
    const textareaVisible = await textarea.isVisible().catch(() => false);
    const emptyVisible = await emptyState.isVisible().catch(() => false);
    expect(textareaVisible || emptyVisible).toBe(true);
  });

  test('should render top bar with tabs', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Top bar
    const topBar = page.locator('.h-10.border-b');
    await expect(topBar).toBeVisible();

    // All tab buttons
    await expect(page.getByRole('button', { name: 'Agent' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Tasks' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Desktop' })).toBeVisible();
  });

  test('should render project selector', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    const select = page.locator('select').first();
    await expect(select).toBeVisible();
  });

  test('should not have critical console errors', async ({ page }) => {
    const errors: string[] = [];

    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', err => errors.push(err.message));

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);

    const realErrors = errors.filter(err =>
      !err.includes('Download the React DevTools') &&
      !err.includes('font') &&
      !err.includes('favicon') &&
      !err.includes('net::ERR') &&
      !err.includes('ResizeObserver')
    );

    expect(realErrors.length, `Critical errors: ${realErrors.join('; ')}`).toBe(0);
  });
});
