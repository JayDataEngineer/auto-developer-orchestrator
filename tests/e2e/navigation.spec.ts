/**
 * Navigation E2E Tests
 *
 * Tests workbench tab switching, sidebar, branding,
 * and top-level layout.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes } from './fixtures';

test.describe('Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    // Wait for all API calls to resolve and React to render
    await page.waitForTimeout(3000);
    // Verify the app is rendered before running tests
    await page.waitForSelector('[data-slot="thread-root"], [aria-label="Message input"]', { timeout: 10000 });
  });

  test('renders workbench tab buttons', async ({ page }) => {
    await expect(page.getByRole('tab', { name: 'Sandbox' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Editor' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Scheduler' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Agents' })).toBeVisible();
  });

  test('defaults to Sandbox tab on load', async ({ page }) => {
    const sandboxTab = page.getByRole('tab', { name: 'Sandbox' });
    await expect(sandboxTab).toHaveAttribute('data-state', 'active');
  });

  test('switches to Scheduler tab on click', async ({ page }) => {
    await page.getByRole('tab', { name: 'Scheduler' }).click();
    await expect(page.getByRole('tab', { name: 'Scheduler' })).toHaveAttribute('data-state', 'active');
    await expect(page.getByRole('tab', { name: 'Sandbox' })).not.toHaveAttribute('data-state', 'active');
  });

  test('switches to Agents tab on click', async ({ page }) => {
    await page.getByRole('tab', { name: 'Agents' }).click();
    await expect(page.getByRole('tab', { name: 'Agents' })).toHaveAttribute('data-state', 'active');
  });

  test('renders Pux branding in sidebar', async ({ page }) => {
    // "Pux" text appears in the sidebar header
    const puxText = page.getByText('Pux', { exact: true }).first();
    await expect(puxText).toBeVisible({ timeout: 5000 });
  });

  test('shows New Chat button in sidebar', async ({ page }) => {
    const newChatBtn = page.getByText('New Chat');
    await expect(newChatBtn).toBeVisible({ timeout: 5000 });
  });

  test('shows Open Folder button in sidebar', async ({ page }) => {
    const openFolderBtn = page.getByText('Open Folder');
    await expect(openFolderBtn).toBeVisible({ timeout: 5000 });
  });

  test('no white screen on any tab', async ({ page }) => {
    const tabs = ['Editor', 'Scheduler', 'Agents'];
    for (const tab of tabs) {
      await page.getByRole('tab', { name: tab }).click();
      await page.waitForTimeout(1000);
      // Body should always be visible
      await expect(page.locator('body')).toBeVisible();
      // Textarea should remain visible (chat is always present)
      await expect(page.getByLabel('Message input')).toBeVisible({ timeout: 5000 });
    }
  });

  test('tab buttons have SVG icons', async ({ page }) => {
    const tabs = ['Sandbox', 'Editor', 'Scheduler', 'Agents'];
    for (const tab of tabs) {
      const tabBtn = page.getByRole('tab', { name: tab });
      const svg = tabBtn.locator('svg');
      await expect(svg).toBeAttached();
    }
  });

  // ── Sidebar Layout ──

  test('sidebar shows project groups or empty state', async ({ page }) => {
    // Either "No projects yet" or project items are shown
    const noProjects = page.getByText('No projects yet');
    const projectItems = page.locator('[data-collapsible]');
    const hasEmpty = await noProjects.isVisible().catch(() => false);
    const hasProjects = (await projectItems.count()) > 0;
    expect(hasEmpty || hasProjects).toBe(true);
  });

  test('workbench toggle button is visible', async ({ page }) => {
    // The panel toggle button with aria-label
    const toggleBtn = page.getByLabel('Open workbench').or(page.getByLabel('Close workbench'));
    await expect(toggleBtn.first()).toBeVisible({ timeout: 5000 });
  });

  test('terminal toggle button is visible', async ({ page }) => {
    const termBtn = page.getByLabel('Toggle terminal');
    await expect(termBtn).toBeVisible({ timeout: 5000 });
  });
});
