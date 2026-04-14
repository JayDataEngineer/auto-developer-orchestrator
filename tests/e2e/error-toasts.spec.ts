/**
 * Error Toast E2E Tests
 *
 * Verifies that error toast notifications appear when API calls fail.
 * Uses mockApiRoutes for happy-path defaults, then overrides specific
 * routes to return errors and checks for toast visibility.
 *
 * These tests exist because bugs slipped through when all tests mocked
 * everything to succeed. Error paths MUST be tested.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, SSE_SIMPLE_REPLY } from './fixtures';

// Helper: wait for a toast to appear with given text (partial match)
async function expectToast(page: import('@playwright/test').Page, text: string) {
  // Toasts render inside the fixed bottom-right container with class "text-sm font-mono"
  const toast = page.locator('.fixed.bottom-4.right-4').locator(`text=${text}`).first();
  await expect(toast).toBeVisible({ timeout: 5000 });
  return toast;
}

// Helper: assert NO toast with given text
async function expectNoToast(page: import('@playwright/test').Page, text: string) {
  const toast = page.locator('.fixed.bottom-4.right-4').locator(`text=${text}`).first();
  await expect(toast).not.toBeVisible({ timeout: 1000 });
}

test.describe('Error Toasts - Computer Use', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);
  });

  test('enable failure shows error toast', async ({ page }) => {
    // Override enable to return 500
    await page.route('**/api/sandbox/**/computer-use/enable', async route => {
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Sandbox not found' }),
      });
    });

    // Navigate to Desktop tab to trigger auto-enable
    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(2000);

    // Error toast should appear
    await expectToast(page, 'Sandbox not found');
  });

  test('screenshot failure shows error toast', async ({ page }) => {
    // Mock sandbox list to return a sandbox so Desktop tab can render
    await page.route('**/api/sandbox/**', async route => {
      const url = route.request().url();
      if (url.includes('/computer-use/screenshot')) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'CDP not connected' }),
        });
      } else if (url.includes('/computer-use/enable')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ enabled: true, sandboxId: 'sandbox-test', cdpPort: 9222 }),
        });
      } else if (route.request().method() === 'GET' && !url.includes('/computer-use') && !url.includes('/desktop') && !url.includes('/viewer')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ sandboxes: [{ id: 'sandbox-test', project: 'test-project', status: 'running' }] }),
        });
      }
    });

    // Navigate to Desktop tab
    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(3000);

    // Try to take a screenshot (the ComputerUseTab may auto-screenshot)
    // The error should produce a toast
    await expectToast(page, 'CDP not connected');
  });
});

test.describe('Error Toasts - Pi Agent', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);
  });

  test('prompt HTTP 500 shows error toast', async ({ page }) => {
    // Override prompt to return 500
    await page.route('**/api/pi/prompt', async route => {
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Model unavailable' }),
      });
    });

    // Navigate to Agent tab
    await page.locator('button:has-text("Agent")').click();
    await page.waitForTimeout(1000);

    // Send a message
    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });
    await textarea.fill('test prompt');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // Error toast should appear
    await expectToast(page, 'Model unavailable');
  });

  test('prompt network failure shows error toast', async ({ page }) => {
    // Override prompt to abort (simulates network failure)
    await page.route('**/api/pi/prompt', async route => {
      await route.abort('failed');
    });

    await page.locator('button:has-text("Agent")').click();
    await page.waitForTimeout(1000);

    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });
    await textarea.fill('network test');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // Error toast should appear with some error message
    await expectToast(page, 'failed');
  });

  test('model switch failure shows error toast', async ({ page }) => {
    // Override set model to return 500
    await page.route('**/api/pi/model', async route => {
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Model not found' }),
      });
    });

    await page.locator('button:has-text("Agent")').click();
    await page.waitForTimeout(1000);

    // Find the model selector and try to switch models
    const modelButton = page.locator('[data-testid="model-selector"], button:has-text("or-free"), select').first();
    if (await modelButton.isVisible()) {
      await modelButton.click();
      await page.waitForTimeout(500);

      // Click any model option
      const option = page.locator('li, [role="option"], option').first();
      if (await option.isVisible()) {
        await option.click();
        await page.waitForTimeout(2000);

        // Error toast should appear
        await expectToast(page, 'Failed to switch model');
      }
    }
  });
});

test.describe('Error Toasts - Behavior', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);
  });

  test('error toast has close button', async ({ page }) => {
    // Force a toast by making prompt fail
    await page.route('**/api/pi/prompt', async route => {
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Test error for close button' }),
      });
    });

    await page.locator('button:has-text("Agent")').click();
    await page.waitForTimeout(1000);

    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });
    await textarea.fill('trigger error');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // Toast should appear with close button (X icon)
    const toast = page.locator('.fixed.bottom-4.right-4 > div').first();
    await expect(toast).toBeVisible({ timeout: 5000 });

    // Close button should be visible
    const closeBtn = toast.locator('button').last();
    await expect(closeBtn).toBeVisible();

    // Click close and toast should disappear
    await closeBtn.click();
    await expect(toast).not.toBeVisible({ timeout: 1000 });
  });

  test('multiple errors queue separate toasts', async ({ page }) => {
    let callCount = 0;
    await page.route('**/api/pi/prompt', async route => {
      callCount++;
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: `Error #${callCount}` }),
      });
    });

    await page.locator('button:has-text("Agent")').click();
    await page.waitForTimeout(1000);

    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    // Trigger first error
    await textarea.fill('first');
    await textarea.press('Enter');
    await page.waitForTimeout(1500);

    // Trigger second error
    await textarea.fill('second');
    await textarea.press('Enter');
    await page.waitForTimeout(1500);

    // Should see two toasts
    const toasts = page.locator('.fixed.bottom-4.right-4 > div');
    await expect(toasts).toHaveCount(2, { timeout: 3000 });
  });
});
