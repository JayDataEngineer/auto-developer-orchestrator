/**
 * Layout visual tests — screenshots + DOM structure checks.
 *
 * Design (from plan):
 *
 * ┌──────────────────────────────────────────────────────┐
 * │  Pux                                          ⚙ ⚡   │
 * ├──────────┬───────────────────────────────────────────┤
 * │          │  ┌─────────────────────────────────────┐  │
 * │  Chat    │  │  Browser / Desktop Visual           │  │
 * │  History │  │  (screenshots from CDP)             │  │
 * │          │  └─────────────────────────────────────┘  │
 * │          │  ┌─────────────────────────────────────┐  │
 * │          │  │  Chat messages + tool calls          │  │
 * │          │  │  (scrollable, fills remaining space) │  │
 * │          │  └─────────────────────────────────────┘  │
 * │          │  ┌─────────────────────────────────────┐  │
 * │  ⚙ Jobs  │  │  Input: ask me anything...           │  │
 * │  3 total │  └─────────────────────────────────────┘  │
 * └──────────┴───────────────────────────────────────────┘
 *
 * Left sidebar: session list + scheduler summary
 * Right main:   browser visual (top) → chat (middle) → input (bottom)
 */

import { test, expect } from '@playwright/test';

const SCREENSHOT_DIR = 'tests/e2e-lit/screenshots';

test.describe('Layout — sidebar + main structure', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    // Wait for Lit to render
    await page.waitForTimeout(800);
  });

  test('screenshot — initial state', async ({ page }) => {
    await page.screenshot({ path: `${SCREENSHOT_DIR}/initial.png`, fullPage: true });
  });

  test('has left sidebar', async ({ page }) => {
    const sidebar = page.locator('.sidebar');
    await expect(sidebar).toBeAttached();
  });

  test('sidebar contains Pux branding', async ({ page }) => {
    const sidebar = page.locator('.sidebar');
    await expect(sidebar.locator('text=Pux')).toBeVisible();
  });

  test('sidebar has scheduler summary section', async ({ page }) => {
    const schedulerSummary = page.locator('.sidebar .scheduler-summary');
    await expect(schedulerSummary).toBeAttached();
  });

  test('has main area to the right of sidebar', async ({ page }) => {
    const main = page.locator('.main');
    await expect(main).toBeAttached();
  });

  test('main area has browser panel at top', async ({ page }) => {
    const browserPanel = page.locator('.main browser-panel');
    await expect(browserPanel).toBeAttached();
  });

  test('main area has chat messages in middle', async ({ page }) => {
    const chatPanel = page.locator('.main chat-panel');
    await expect(chatPanel).toBeAttached();
  });

  test('main area has input at bottom', async ({ page }) => {
    const input = page.locator('.main chat-panel input');
    await expect(input).toBeVisible();
  });

  test('sidebar is narrower than main area', async ({ page }) => {
    const sidebar = page.locator('.sidebar');
    const main = page.locator('.main');
    const sidebarBox = await sidebar.boundingBox();
    const mainBox = await main.boundingBox();
    expect(sidebarBox).toBeTruthy();
    expect(mainBox).toBeTruthy();
    expect(sidebarBox!.width).toBeLessThan(mainBox!.width);
  });

  test('sidebar and main are side by side (same top)', async ({ page }) => {
    const sidebar = page.locator('.sidebar');
    const main = page.locator('.main');
    const sidebarBox = await sidebar.boundingBox();
    const mainBox = await main.boundingBox();
    expect(sidebarBox).toBeTruthy();
    expect(mainBox).toBeTruthy();
    // Sidebar right edge → resize handle (5px) → main left edge
    expect(Math.abs(sidebarBox!.x + sidebarBox!.width - mainBox!.x)).toBeLessThanOrEqual(7);
  });
});

test.describe('Layout — browser visual', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
  });

  test('browser panel is in the main area, above chat', async ({ page }) => {
    const browserPanel = page.locator('.main browser-panel');
    const chatPanel = page.locator('.main chat-panel');
    const browserBox = await browserPanel.boundingBox();
    const chatBox = await chatPanel.boundingBox();
    expect(browserBox).toBeTruthy();
    expect(chatBox).toBeTruthy();
    // Browser is above chat (lower y value)
    expect(browserBox!.y).toBeLessThan(chatBox!.y);
  });

  test('screenshot — with browser panel visible', async ({ page }) => {
    await page.screenshot({ path: `${SCREENSHOT_DIR}/browser-visible.png`, fullPage: true });
  });
});

test.describe('Layout — chat interaction', () => {
  test('screenshot — after typing', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const input = page.locator('.main chat-panel input');
    await input.fill('Go to youtube.com and play the first video');
    await page.screenshot({ path: `${SCREENSHOT_DIR}/typed-message.png`, fullPage: true });
  });

  test('send button exists in chat panel', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const btn = page.locator('.main chat-panel button');
    await expect(btn).toBeVisible();
    await expect(btn).toHaveText('Send');
  });

  test('empty state message visible', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    await expect(page.locator('text=Send a message to start')).toBeVisible();
  });
});

test.describe('Layout — scheduler sidebar', () => {
  test('screenshot — scheduler in sidebar', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/scheduler-sidebar.png`, fullPage: true });
  });

  test('scheduler summary shows job count', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const summary = page.locator('.scheduler-summary');
    await expect(summary).toBeAttached();
  });
});
