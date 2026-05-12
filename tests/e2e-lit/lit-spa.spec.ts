/**
 * Lit SPA E2E tests — verify shared code renders in the browser.
 *
 * These test that:
 * 1. The page loads (no import/compile errors)
 * 2. Custom elements render (pux-app, chat-panel, scheduler-panel, browser-panel)
 * 3. Chat input works
 * 4. Scheduler panel loads
 * 5. Browser/Scheduler toggles work
 */

import { test, expect } from '@playwright/test';

test.describe('Lit SPA — page load', () => {
  test('loads without errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));
    await page.goto('/');
    // Wait for custom elements to define
    await page.waitForTimeout(1000);
    expect(errors).toEqual([]);
  });

  test('renders pux-app element', async ({ page }) => {
    await page.goto('/');
    const app = page.locator('pux-app');
    await expect(app).toBeAttached();
  });

  test('shows Pux branding', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('text=Pux')).toBeVisible();
  });

  test('has chat input', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('chat-panel input');
    await expect(input).toBeVisible();
    await expect(input).toHaveAttribute('placeholder', 'Ask Pux anything...');
  });

  test('has send button', async ({ page }) => {
    await page.goto('/');
    const btn = page.locator('chat-panel button');
    await expect(btn).toBeVisible();
    await expect(btn).toHaveText('Send');
  });
});

test.describe('Lit SPA — toggle buttons', () => {
  test('Browser toggle exists', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('button:has-text("Browser")')).toBeVisible();
  });

  test('Scheduler toggle exists', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('button:has-text("Scheduler")')).toBeVisible();
  });

  test('clicking Browser shows browser-panel', async ({ page }) => {
    await page.goto('/');
    // Browser area starts with .hidden class (width: 0)
    const browserArea = page.locator('.browser-area');
    await expect(browserArea).toHaveClass(/hidden/);
    // Click Browser toggle
    await page.locator('button:has-text("Browser")').click();
    // Hidden class removed
    await expect(browserArea).not.toHaveClass(/hidden/);
    // Browser panel now visible inside
    await expect(page.locator('browser-panel')).toBeAttached();
  });

  test('clicking Scheduler shows scheduler-panel', async ({ page }) => {
    await page.goto('/');
    expect(await page.locator('scheduler-panel').isVisible()).toBe(false);
    await page.locator('button:has-text("Scheduler")').click();
    await expect(page.locator('scheduler-panel')).toBeVisible();
  });

  test('clicking Scheduler twice hides it', async ({ page }) => {
    await page.goto('/');
    const btn = page.locator('button:has-text("Scheduler")');
    await btn.click();
    await expect(page.locator('scheduler-panel')).toBeVisible();
    await btn.click();
    await expect(page.locator('scheduler-panel')).not.toBeVisible();
  });
});

test.describe('Lit SPA — chat input', () => {
  test('typing in input updates value', async ({ page }) => {
    await page.goto('/');
    const input = page.locator('chat-panel input');
    await input.fill('hello world');
    await expect(input).toHaveValue('hello world');
  });

  test('empty state shows placeholder text', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('text=Send a message to start')).toBeVisible();
  });
});
