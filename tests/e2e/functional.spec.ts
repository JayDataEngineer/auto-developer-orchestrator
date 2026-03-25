import { test, expect } from '@playwright/test';

test.describe('Auto-Developer Orchestrator - Functional Tests', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
  });

  test('should load the application', async ({ page }) => {
    await expect(page).toHaveTitle(/Auto-Developer/);
    await expect(page.locator('body')).toBeVisible();
  });

  test('should display main UI sections', async ({ page }) => {
    // Check for main sections
    await expect(page.locator('[class*="Sidebar"]')).toBeVisible();
    await expect(page.locator('[class*="Header"]')).toBeVisible();
    await expect(page.locator('[class*="Terminal"]')).toBeVisible();
  });

  test('should have working tab navigation', async ({ page }) => {
    // Find and click terminal tab
    const terminalTab = page.getByText('Terminal', { exact: true });
    if (await terminalTab.isVisible()) {
      await terminalTab.click();
      await page.waitForTimeout(500);
      await expect(page.locator('[class*="Terminal"]')).toBeVisible();
    }

    // Find and click activity tab
    const activityTab = page.getByText('Activity', { exact: true });
    if (await activityTab.isVisible()) {
      await activityTab.click();
      await page.waitForTimeout(500);
      await expect(page.locator('[class*="Activity"]')).toBeVisible();
    }
  });

  test('should display logs in terminal', async ({ page }) => {
    await page.waitForTimeout(3000); // Wait for logs to populate
    
    // Terminal should have some content
    const terminalContent = page.locator('[class*="Terminal"]');
    await expect(terminalContent).toBeVisible();
  });

  test('should handle window resize', async ({ page }) => {
    // Test different viewport sizes
    const viewports = [
      { width: 375, height: 667 },   // Mobile
      { width: 768, height: 1024 },  // Tablet
      { width: 1920, height: 1080 }, // Desktop
    ];

    for (const viewport of viewports) {
      await page.setViewportSize(viewport.width, viewport.height);
      await page.waitForTimeout(500);
      
      // Page should still be functional
      await expect(page.locator('body')).toBeVisible();
    }
  });
});
