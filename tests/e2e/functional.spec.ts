/**
 * Functional E2E Tests
 *
 * Tests app loading, main UI sections, tab navigation,
 * logs display, and viewport responsiveness.
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

  test('should load the application', async ({ page }) => {
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('should display main UI sections', async ({ page }) => {
    // Top bar with tabs
    await expect(page.getByRole('button', { name: 'Agent' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Tasks' })).toBeVisible();

    // Project selector
    await expect(page.locator('select').first()).toBeVisible();

    // PI branding
    await expect(page.getByText('PI', { exact: true }).first()).toBeVisible();
  });

  test('should have working tab navigation', async ({ page }) => {
    // Switch to Tasks tab
    await page.getByRole('button', { name: 'Tasks' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('button', { name: 'Tasks' })).toHaveClass(/bg-primary/);

    // Switch to Desktop tab
    await page.getByRole('button', { name: 'Desktop' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('button', { name: 'Desktop' })).toHaveClass(/bg-primary/);

    // Switch to Scheduler tab
    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('button', { name: 'Scheduler' })).toHaveClass(/bg-primary/);

    // Back to Agent
    await page.getByRole('button', { name: 'Agent' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('button', { name: 'Agent' })).toHaveClass(/bg-primary/);
  });

  test('should display agent chat on Agent tab', async ({ page }) => {
    // Agent tab is default — should show Pi Agent Ready or textarea
    const textarea = page.locator('textarea');
    const emptyState = page.getByText('Pi Agent Ready');
    const textareaVisible = await textarea.isVisible().catch(() => false);
    const emptyVisible = await emptyState.isVisible().catch(() => false);
    expect(textareaVisible || emptyVisible).toBe(true);
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
      const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
      await expect(rootDiv).toBeVisible();
    }
  });
});
