/**
 * Navigation E2E Tests
 *
 * Tests tab switching, keyboard shortcuts, project selector,
 * branding, and top-level layout.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, MOCK_PROJECTS } from './fixtures';

test.describe('Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    // Wait for all API calls to resolve and React to render
    await page.waitForTimeout(3000);
    // Verify the top bar is rendered before running tests
    await page.waitForSelector('button:has-text("Agent")', { timeout: 10000 });
  });

  test('renders all 4 tab buttons', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Agent' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Tasks' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Desktop' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Scheduler' })).toBeVisible();
  });

  test('defaults to Agent tab on load', async ({ page }) => {
    const agentBtn = page.getByRole('button', { name: 'Agent' });
    await expect(agentBtn).toHaveClass(/bg-primary/);
  });

  test('switches to Tasks tab on click', async ({ page }) => {
    await page.getByRole('button', { name: 'Tasks' }).click();
    await expect(page.getByRole('button', { name: 'Tasks' })).toHaveClass(/bg-primary/);
    await expect(page.getByRole('button', { name: 'Agent' })).not.toHaveClass(/bg-primary/);
  });

  test('switches to Desktop tab on click', async ({ page }) => {
    await page.getByRole('button', { name: 'Desktop' }).click();
    await expect(page.getByRole('button', { name: 'Desktop' })).toHaveClass(/bg-primary/);
  });

  test('switches to Scheduler tab on click', async ({ page }) => {
    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByRole('button', { name: 'Scheduler' })).toHaveClass(/bg-primary/);
  });

  test('renders project selector with projects', async ({ page }) => {
    const select = page.locator('select').first();
    await expect(select).toBeVisible();
    await expect(select).toHaveValue(MOCK_PROJECTS[0]);
  });

  test('renders PI branding text', async ({ page }) => {
    // "PI" text appears in the top bar — check it exists
    const piText = page.getByText('PI', { exact: true }).first();
    await expect(piText).toBeVisible({ timeout: 5000 });
  });

  test('shows GitHub settings button', async ({ page }) => {
    // The GitHub button has title "GitHub Settings" or text "GitHub"
    const githubBtn = page.getByTitle('GitHub Settings');
    const githubText = page.getByText('GitHub').first();
    const btnVisible = await githubBtn.isVisible().catch(() => false);
    const textVisible = await githubText.isVisible().catch(() => false);
    expect(btnVisible || textVisible).toBe(true);
  });

  test('keyboard shortcut Ctrl+1 switches to Agent tab', async ({ page }) => {
    await page.getByRole('button', { name: 'Tasks' }).click();
    await page.waitForTimeout(300);
    await page.evaluate(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '1', ctrlKey: true, bubbles: true }));
    });
    await page.waitForTimeout(300);
    await expect(page.getByRole('button', { name: 'Agent' })).toHaveClass(/bg-primary/);
  });

  test('keyboard shortcut Ctrl+2 switches to Tasks tab', async ({ page }) => {
    await page.evaluate(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '2', ctrlKey: true, bubbles: true }));
    });
    await page.waitForTimeout(300);
    await expect(page.getByRole('button', { name: 'Tasks' })).toHaveClass(/bg-primary/);
  });

  test('keyboard shortcut Ctrl+3 switches to Desktop tab', async ({ page }) => {
    await page.evaluate(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '3', ctrlKey: true, bubbles: true }));
    });
    await page.waitForTimeout(300);
    await expect(page.getByRole('button', { name: 'Desktop' })).toHaveClass(/bg-primary/);
  });

  test('keyboard shortcut Ctrl+4 switches to Scheduler tab', async ({ page }) => {
    await page.evaluate(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '4', ctrlKey: true, bubbles: true }));
    });
    await page.waitForTimeout(300);
    await expect(page.getByRole('button', { name: 'Scheduler' })).toHaveClass(/bg-primary/);
  });

  test('no white screen on any tab', async ({ page }) => {
    const tabs = ['Tasks', 'Desktop', 'Scheduler'];
    for (const tab of tabs) {
      await page.getByRole('button', { name: tab }).click();
      await page.waitForTimeout(1000);
      const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
      await expect(rootDiv).toBeVisible();
    }
  });

  test('changing project in selector updates state', async ({ page }) => {
    const select = page.locator('select').first();
    await select.selectOption(MOCK_PROJECTS[1]);
    await expect(select).toHaveValue(MOCK_PROJECTS[1]);
  });

  test('tab buttons have SVG icons', async ({ page }) => {
    const tabs = ['Agent', 'Tasks', 'Desktop', 'Scheduler'];
    for (const tab of tabs) {
      const button = page.getByRole('button', { name: tab });
      const svg = button.locator('svg');
      await expect(svg).toBeAttached();
    }
  });

  // ── Dynamic Sidebar Layout ──

  test('Agent tab shows both left history sidebar and right artifacts panel', async ({ page }) => {
    // Agent tab is default - check left sidebar (History) and right panel (Artifacts)
    await expect(page.getByText('Artifacts')).toBeVisible({ timeout: 5000 });
    // History sidebar should have the collapse toggle
    const sidebarToggle = page.locator('.absolute.z-20 button, button:has(.lucide-chevrons-left), .lucide-chevron-left').first();
    await expect(sidebarToggle).toBeVisible({ timeout: 5000 });
  });

  test('Tasks tab shows no sidebars, full-width kanban', async ({ page }) => {
    await page.getByRole('button', { name: 'Tasks' }).click();
    await page.waitForTimeout(1000);

    // Artifacts panel should NOT be visible
    const artifactsPanel = page.getByText('Artifacts');
    const visible = await artifactsPanel.isVisible().catch(() => false);
    expect(visible).toBe(false);

    // Kanban columns should be visible
    await expect(page.getByText('Pending').first()).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('Completed').first()).toBeVisible({ timeout: 5000 });
  });

  test('Desktop tab shows its own 3-panel layout', async ({ page }) => {
    await page.getByRole('button', { name: 'Desktop' }).click();
    await page.waitForTimeout(1000);

    // Should show "Agent Chat" header in narrow left panel
    await expect(page.getByText('Agent Chat')).toBeVisible({ timeout: 5000 });
  });

  test('Scheduler tab shows full-width scheduler', async ({ page }) => {
    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForTimeout(1500);

    // Should show scheduler content without sidebars
    await expect(page.getByText('Scheduled Jobs')).toBeVisible({ timeout: 10000 });

    // Artifacts panel should NOT be visible
    const artifactsPanel = page.getByText('Artifacts');
    const visible = await artifactsPanel.isVisible().catch(() => false);
    expect(visible).toBe(false);
  });
});
