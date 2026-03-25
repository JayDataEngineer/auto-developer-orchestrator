import { test, expect } from '@playwright/test';
import path from 'path';

test.describe('Auto-Developer Orchestrator - Visual Tests', () => {
  // Take screenshot of the main page
  test('should load main page and take screenshot', async ({ page }) => {
    await page.goto('/');
    
    // Wait for the page to be fully loaded
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000); // Wait for any animations
    
    // Take full page screenshot
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '01-main-page.png'),
      fullPage: true 
    });
    
    // Verify page title
    await expect(page).toHaveTitle(/Auto-Developer/);
  });

  test('should show sidebar and take screenshot', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Sidebar should be visible
    const sidebar = page.locator('[class*="Sidebar"]');
    await expect(sidebar).toBeVisible();

    // Take screenshot with sidebar
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '02-sidebar.png'),
      fullPage: true 
    });
  });

  test('should show terminal panel and take screenshot', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000); // Wait for logs to populate

    // Terminal should be visible
    const terminal = page.locator('[class*="Terminal"]');
    await expect(terminal).toBeVisible();

    // Take screenshot of terminal
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '03-terminal.png'),
      fullPage: true 
    });
  });

  test('should show header and take screenshot', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Header should be visible
    const header = page.locator('[class*="Header"]');
    await expect(header).toBeVisible();

    // Take screenshot focused on header area
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '04-header.png'),
      fullPage: true 
    });
  });

  test('should switch to activity tab and take screenshot', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Click on activity tab
    const activityTab = page.getByText('Activity', { exact: true });
    if (await activityTab.isVisible()) {
      await activityTab.click();
      await page.waitForTimeout(1000);

      // Take screenshot
      await page.screenshot({ 
        path: path.join(__dirname, 'screenshots', '05-activity-tab.png'),
        fullPage: true 
      });
    }
  });

  test('should show project selector and take screenshot', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Look for project selector
    const projectSelector = page.locator('select, [class*="project"], [class*="Project"]').first();
    
    if (await projectSelector.isVisible()) {
      await projectSelector.screenshot({ 
        path: path.join(__dirname, 'screenshots', '06-project-selector.png')
      });
    } else {
      // Fallback - just screenshot the top area
      await page.screenshot({ 
        path: path.join(__dirname, 'screenshots', '06-project-area.png'),
        clip: { x: 0, y: 0, width: 800, height: 400 }
      });
    }
  });

  test('should test responsive layout - mobile view', async ({ page }) => {
    await page.goto('/');
    await page.setViewportSize({ width: 375, height: 667 }); // iPhone SE
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Take mobile screenshot
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '07-mobile-view.png'),
      fullPage: true 
    });
  });

  test('should test responsive layout - tablet view', async ({ page }) => {
    await page.goto('/');
    await page.setViewportSize({ width: 768, height: 1024 }); // iPad
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Take tablet screenshot
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '08-tablet-view.png'),
      fullPage: true 
    });
  });

  test('should test responsive layout - desktop wide', async ({ page }) => {
    await page.goto('/');
    await page.setViewportSize({ width: 1920, height: 1080 }); // Full HD
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Take desktop screenshot
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '09-desktop-wide.png'),
      fullPage: true 
    });
  });

  test('should capture dark theme/glass morphism UI', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Set dark mode if there's a toggle
    // For now just capture the default theme
    await page.screenshot({ 
      path: path.join(__dirname, 'screenshots', '10-ui-theme.png'),
      fullPage: true 
    });

    // Verify glass morphism classes are present
    const glassElements = page.locator('[class*="glass"]');
    expect(await glassElements.count()).toBeGreaterThan(0);
  });
});
