/**
 * Render E2E Tests
 *
 * Tests that key components render without crashing.
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

    // Root container with the sidebar provider is visible
    await expect(page.locator('body')).toBeVisible();
  });

  test('should render chat thread content', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Thread shows either the welcome message or textarea
    const textarea = page.getByLabel('Message input');
    const welcomeText = page.getByText('Pux').first();
    const textareaVisible = await textarea.isVisible().catch(() => false);
    const welcomeVisible = await welcomeText.isVisible().catch(() => false);
    expect(textareaVisible || welcomeVisible).toBe(true);
  });

  test('should render workbench tabs', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Workbench tabs in the right panel
    await expect(page.getByRole('tab', { name: 'Sandbox' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Editor' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Scheduler' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Agents' })).toBeVisible();
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
