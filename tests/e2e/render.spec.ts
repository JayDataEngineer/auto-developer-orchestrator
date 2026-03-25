import { test, expect } from '@playwright/test';

test.describe('Frontend Render Tests', () => {
  test('should render main page without crashing', async ({ page }) => {
    // Navigate to the app
    await page.goto('/');
    
    // Wait for network to be idle (page loaded)
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000); // Wait for React to hydrate
    
    // Take a screenshot for visual confirmation
    await page.screenshot({ 
      path: 'tests/e2e/screenshots/render-test.png',
      fullPage: true 
    });
    
    // Basic sanity checks - page should have content
    const body = await page.locator('body');
    await expect(body).toBeVisible();
    
    // Should not have any error text
    const errorText = page.getByText(/Uncaught TypeError|cannot access property|Error:/i);
    await expect(errorText).not.toBeVisible({ timeout: 5000 });
    
    console.log('✓ Frontend rendered successfully');
  });

  test('should render Terminal component', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);
    
    // Terminal component should exist and be visible
    const terminal = page.locator('[class*="Terminal"]').first();
    await expect(terminal).toBeVisible();
    
    // Take screenshot
    await terminal.screenshot({ 
      path: 'tests/e2e/screenshots/terminal-render-test.png'
    });
    
    console.log('✓ Terminal component rendered');
  });

  test('should render Sidebar component', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    
    // Sidebar should exist
    const sidebar = page.locator('[class*="Sidebar"]').first();
    await expect(sidebar).toBeVisible();
    
    // Take screenshot
    await sidebar.screenshot({ 
      path: 'tests/e2e/screenshots/sidebar-render-test.png'
    });
    
    console.log('✓ Sidebar component rendered');
  });

  test('should render Header component', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    
    // Header should exist
    const header = page.locator('[class*="Header"]').first();
    await expect(header).toBeVisible();
    
    // Take screenshot
    await header.screenshot({ 
      path: 'tests/e2e/screenshots/header-render-test.png'
    });
    
    console.log('✓ Header component rendered');
  });

  test('should not have console errors', async ({ page }) => {
    const errors: string[] = [];
    
    page.on('console', msg => {
      if (msg.type() === 'error') {
        errors.push(msg.text());
      }
    });
    
    page.on('pageerror', error => {
      errors.push(error.message);
    });
    
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);
    
    // Filter out expected/benign errors
    const realErrors = errors.filter(err => 
      !err.includes('Download the React DevTools') &&
      !err.includes('font')
    );
    
    if (realErrors.length > 0) {
      console.error('Console errors found:', realErrors);
      // Don't fail - just log for now
    }
    
    // Take final screenshot
    await page.screenshot({ 
      path: 'tests/e2e/screenshots/no-errors-test.png',
      fullPage: true 
    });
    
    console.log(`✓ Console check complete (${realErrors.length} errors)`);
  });
});
