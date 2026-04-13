/**
 * Visual E2E Tests
 *
 * Takes screenshots of the current UI at various viewports
 * and verifies key visual elements are present.
 */
import { test, expect } from '@playwright/test';
import { fileURLToPath } from 'url';
import path from 'path';
import { mockApiRoutes } from './fixtures';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

test.describe('Auto-Developer Orchestrator - Visual Tests', () => {
  test('should load main page and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '01-main-page.png'),
      fullPage: true,
    });

    // Verify root container is visible
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('should show top bar with tabs and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Top bar should be visible
    const topBar = page.locator('.h-10.border-b');
    await expect(topBar).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '02-top-bar.png'),
      fullPage: true,
    });
  });

  test('should show Agent tab and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);

    // Agent tab is default — verify content area exists
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '03-agent-tab.png'),
      fullPage: true,
    });
  });

  test('should show Tasks tab and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.getByRole('button', { name: 'Tasks' }).click();
    await page.waitForTimeout(2000);

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '04-tasks-tab.png'),
      fullPage: true,
    });

    // Verify page didn't crash
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('should show project selector and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    const select = page.locator('select').first();
    await expect(select).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '06-project-selector.png'),
      clip: { x: 0, y: 0, width: 800, height: 100 },
    });
  });

  test('should test responsive layout - mobile view', async ({ page }) => {
    await mockApiRoutes(page);
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '07-mobile-view.png'),
      fullPage: true,
    });

    // App should still be functional
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('should test responsive layout - tablet view', async ({ page }) => {
    await mockApiRoutes(page);
    await page.setViewportSize({ width: 768, height: 1024 });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '08-tablet-view.png'),
      fullPage: true,
    });

    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('should test responsive layout - desktop wide', async ({ page }) => {
    await mockApiRoutes(page);
    await page.setViewportSize({ width: 1920, height: 1080 });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '09-desktop-wide.png'),
      fullPage: true,
    });

    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('should capture dark theme with backdrop blur', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '10-ui-theme.png'),
      fullPage: true,
    });

    // Verify backdrop-blur (glass morphism effect) is used in top bar
    const blurElements = page.locator('.backdrop-blur-md');
    const count = await blurElements.count();
    expect(count).toBeGreaterThan(0);
  });
});
