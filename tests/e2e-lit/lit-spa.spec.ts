/**
 * Lit SPA E2E tests — verify shared code renders in the browser.
 *
 * Layout (sidebar + main):
 *
 * ┌──────────┬───────────────────────────────────────────┐
 * │  Pux     │  Browser / Desktop Visual (top strip)     │
 * │          ├───────────────────────────────────────────┤
 * │  Chat    │                                           │
 * │  History │  Chat messages + tool calls               │
 * │          │  (scrollable, fills remaining space)      │
 * │          ├───────────────────────────────────────────┤
 * │  ⚙ Jobs  │  Input: ask me anything...                │
 * └──────────┴───────────────────────────────────────────┘
 *
 * Tests verify:
 * 1. Page loads without errors
 * 2. Custom elements render (pux-app, chat-panel, scheduler-panel, browser-panel)
 * 3. Sidebar + main layout structure
 * 4. Chat input works
 * 5. Scheduler panel loads in sidebar
 * 6. Browser panel loads in main area
 */

import { test, expect } from '@playwright/test';

test.describe('Lit SPA — page load', () => {
  test('loads without errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', err => errors.push(err.message));
    await page.goto('/');
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

test.describe('Lit SPA — sidebar + main layout', () => {
  test('sidebar exists with correct structure', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const sidebar = page.locator('.sidebar');
    await expect(sidebar).toBeAttached();
    // Pux branding
    await expect(sidebar.locator('.sidebar-brand')).toBeAttached();
    // Chat history area
    await expect(sidebar.locator('.sidebar-chat-history')).toBeAttached();
    // Bottom section (scheduler)
    await expect(sidebar.locator('.sidebar-bottom')).toBeAttached();
  });

  test('main area exists with correct structure', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const main = page.locator('.main');
    await expect(main).toBeAttached();
    // Browser strip at top
    await expect(main.locator('.browser-strip')).toBeAttached();
    // Chat area below
    await expect(main.locator('.chat-area')).toBeAttached();
  });

  test('scheduler-panel is in sidebar bottom', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const scheduler = page.locator('.sidebar-bottom scheduler-panel');
    await expect(scheduler).toBeAttached();
  });

  test('browser-panel is in main browser strip', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const browser = page.locator('.browser-strip browser-panel');
    await expect(browser).toBeAttached();
  });

  test('chat-panel is in main chat area', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const chat = page.locator('.chat-area chat-panel');
    await expect(chat).toBeAttached();
  });

  test('sidebar is left of main (horizontal layout)', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const sidebarBox = await page.locator('.sidebar').boundingBox();
    const resizeH = await page.locator('.resize-h').boundingBox();
    const mainBox = await page.locator('.main').boundingBox();
    expect(sidebarBox).toBeTruthy();
    expect(resizeH).toBeTruthy();
    expect(mainBox).toBeTruthy();
    // Sidebar → resize handle → main (handle is 5px)
    expect(Math.abs(sidebarBox!.x + sidebarBox!.width - resizeH!.x)).toBeLessThan(2);
    expect(Math.abs(resizeH!.x + resizeH!.width - mainBox!.x)).toBeLessThan(2);
  });
});

test.describe('Lit SPA — panel resizing', () => {
  test('horizontal resize handle exists between sidebar and main', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const handle = page.locator('.resize-h');
    await expect(handle).toBeAttached();
    const box = await handle.boundingBox();
    expect(box).toBeTruthy();
    expect(box!.width).toBe(5);
    expect(box!.height).toBeGreaterThan(100);
  });

  test('vertical resize handle exists between browser and chat', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const handle = page.locator('.resize-v');
    await expect(handle).toBeAttached();
    const box = await handle.boundingBox();
    expect(box).toBeTruthy();
    expect(box!.height).toBe(5);
    expect(box!.width).toBeGreaterThan(100);
  });

  test('dragging horizontal handle resizes sidebar', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const handle = page.locator('.resize-h');
    const box = await handle.boundingBox();
    expect(box).toBeTruthy();
    const beforeW = await page.locator('.sidebar').evaluate(el => el.clientWidth);
    // Drag right by 50px
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    await page.mouse.move(box!.x + box!.width / 2 + 50, box!.y + box!.height / 2);
    await page.mouse.up();
    const afterW = await page.locator('.sidebar').evaluate(el => el.clientWidth);
    expect(afterW - beforeW).toBeGreaterThanOrEqual(45); // ~50px wider
  });

  test('dragging vertical handle resizes browser strip', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const handle = page.locator('.resize-v');
    const box = await handle.boundingBox();
    expect(box).toBeTruthy();
    const beforeH = await page.locator('.browser-strip').evaluate(el => el.clientHeight);
    // Drag down by 40px
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2 + 40);
    await page.mouse.up();
    const afterH = await page.locator('.browser-strip').evaluate(el => el.clientHeight);
    expect(afterH - beforeH).toBeGreaterThanOrEqual(35);
  });

  test('sidebar cannot shrink below minimum', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const handle = page.locator('.resize-h');
    const box = await handle.boundingBox();
    expect(box).toBeTruthy();
    // Drag far left
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    await page.mouse.move(0, box!.y + box!.height / 2);
    await page.mouse.up();
    const w = await page.locator('.sidebar').evaluate(el => el.clientWidth);
    expect(w).toBe(140); // min width
  });

  test('sidebar cannot grow beyond maximum', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const handle = page.locator('.resize-h');
    const box = await handle.boundingBox();
    expect(box).toBeTruthy();
    // Drag far right
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await page.mouse.down();
    await page.mouse.move(2000, box!.y + box!.height / 2);
    await page.mouse.up();
    const w = await page.locator('.sidebar').evaluate(el => el.clientWidth);
    expect(w).toBe(500); // max width
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

test.describe('Lit SPA — scheduler expand/collapse', () => {
  test('clicking scheduler summary expands job list', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const summary = page.locator('.scheduler-summary');
    await expect(summary).toBeAttached();
    // Click to expand
    await summary.click();
    // Job list should now be visible
    await expect(page.locator('.job-list')).toBeAttached();
  });

  test('clicking scheduler summary twice collapses job list', async ({ page }) => {
    await page.goto('/');
    await page.waitForTimeout(800);
    const summary = page.locator('.scheduler-summary');
    await summary.click();
    await expect(page.locator('.job-list')).toBeAttached();
    await summary.click();
    // Job list removed from DOM
    await expect(page.locator('.job-list')).not.toBeAttached();
  });
});
