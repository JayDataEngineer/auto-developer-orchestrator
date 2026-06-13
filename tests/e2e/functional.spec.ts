/**
 * Functional E2E Tests
 *
 * Tests app loading, main UI sections, tab navigation,
 * and viewport responsiveness.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes } from './fixtures';

test.describe('Auto-Developer Orchestrator - Functional Tests', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
  });

  test('should load the application', async () => {
    // App renders with body visible
    await expect(page.locator('body')).toBeVisible();
  });

  test('should display main UI sections', async ({ page }) => {
    // Workbench tabs
    await expect(page.getByRole('tab', { name: 'Sandbox' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Scheduler' })).toBeVisible();

    // Chat textarea
    await expect(page.getByLabel('Message input')).toBeVisible();

    // Pux branding
    await expect(page.getByText('Pux', { exact: true }).first()).toBeVisible();
  });

  test('should have working tab navigation', async ({ page }) => {
    // Switch to Editor tab
    await page.getByRole('tab', { name: 'Editor' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('tab', { name: 'Editor' })).toHaveAttribute('data-state', 'active');

    // Switch to Agents tab
    await page.getByRole('tab', { name: 'Agents' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('tab', { name: 'Agents' })).toHaveAttribute('data-state', 'active');

    // Back to Sandbox
    await page.getByRole('tab', { name: 'Sandbox' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('tab', { name: 'Sandbox' })).toHaveAttribute('data-state', 'active');
  });

  test('should display agent chat', async ({ page }) => {
    // Chat is always visible with textarea or welcome text
    const textarea = page.getByLabel('Message input');
    const welcomeText = page.getByText('Pux').first();
    const textareaVisible = await textarea.isVisible().catch(() => false);
    const welcomeVisible = await welcomeText.isVisible().catch(() => false);
    expect(textareaVisible || welcomeVisible).toBe(true);
  });

  test('should handle window resize', async ({ page }) => {
    const viewports = [
      { width: 375, height: 667 },   // Mobile
      { width: 768, height: 1024 },  // Tablet
      { width: 1920, height: 1080 }, // Desktop
    ];

    for (const viewport of viewports) {
      await page.setViewportSize(viewport);
      await page.waitForTimeout(500);
      await expect(page.locator('body')).toBeVisible();
    }
  });
});
