/**
 * One-shot: capture current layout screenshot for analysis.
 */
import { test } from '@playwright/test';

test('capture current layout screenshot', async ({ page }) => {
  await page.goto('/');
  await page.waitForTimeout(1000);
  await page.screenshot({ path: 'tests/e2e-lit/screenshots/current-layout.png', fullPage: true });
});
