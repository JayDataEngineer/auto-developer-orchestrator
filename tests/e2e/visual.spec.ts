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

    // Verify body is visible
    await expect(page.locator('body')).toBeVisible();
  });

  test('should show header with workbench tabs and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Header bar should be visible
    const header = page.locator('header');
    await expect(header).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '02-top-bar.png'),
      fullPage: true,
    });
  });

  test('should show chat thread and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);

    // Chat thread shows welcome text
    await expect(page.getByText('Pux').first()).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '03-agent-tab.png'),
      fullPage: true,
    });
  });

  test('should show Scheduler tab and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.getByRole('tab', { name: 'Scheduler' }).click();
    await page.waitForTimeout(2000);

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '04-scheduler-tab.png'),
      fullPage: true,
    });

    // Verify page didn't crash
    await expect(page.locator('body')).toBeVisible();
  });

  test('should show model selector and take screenshot', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Model selector is in the composer bar
    const modelSelector = page.getByLabel('Select model');
    await expect(modelSelector).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, 'screenshots', '06-model-selector.png'),
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
    await expect(page.locator('body')).toBeVisible();
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

    await expect(page.locator('body')).toBeVisible();
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

    await expect(page.locator('body')).toBeVisible();
  });
});
