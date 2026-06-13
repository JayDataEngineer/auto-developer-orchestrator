/**
 * Smoke Test - Full Application Health Check
 */
import { test, expect } from '@playwright/test';
import {
  mockApiRoutes,
  SSE_WITH_TOOL_CALL,
  SSE_SIMPLE_REPLY,
} from './fixtures';

test.describe('Smoke Test - Full App Health', () => {
  test('complete app health check', async ({ page }) => {
    const errors: string[] = [];

    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', err => errors.push(err.message));

    // ── Step 1: Load app ──
    await mockApiRoutes(page, { sseEvents: SSE_WITH_TOOL_CALL });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // No crash
    const crashErrors = errors.filter(e => e.includes('Uncaught') || e.includes('TypeError'));
    expect(crashErrors.length, `Crash on load: ${crashErrors.join('; ')}`).toBe(0);

    // ── Step 2: Chat thread is visible ──
    // Welcome text shows "Pux" and "Pux"
    // Note: "Pux" appears in both sidebar header and welcome text — use welcome-specific selector
    await expect(page.getByText('Pux').first()).toBeVisible();
    await expect(page.getByLabel('Message input')).toBeVisible();

    // ── Step 3: Send message with tool call ──
    await page.getByLabel('Message input').fill('Test message');
    await page.getByLabel('Message input').press('Enter');

    await expect(page.getByText('Test message')).toBeVisible({ timeout: 5000 });

    // Tool call must render with name
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 5000 });

    // Text from stream
    await expect(page.getByText(/files in your project/)).toBeVisible({ timeout: 5000 });

    // Wait for stream to finish
    await page.waitForTimeout(5000);

    // ── Step 4: Workbench tabs ──
    // The right panel has workbench tabs: Sandbox, Editor, Scheduler, Agents
    await expect(page.getByRole('tab', { name: 'Sandbox' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Agents' })).toBeVisible();

    // ── Step 5: Scheduler tab ──
    await page.getByRole('tab', { name: 'Scheduler' }).click();
    await page.waitForTimeout(1000);

    // Scheduler should render (ConfigPanel)
    const schedulerPanel = page.locator('[data-slot="config-panel"], .flex.h-full');
    await expect(schedulerPanel.first()).toBeVisible();

    // ── Step 6: Back to default tab ──
    await page.getByRole('tab', { name: 'Sandbox' }).click();
    await page.waitForTimeout(500);

    // Chat should still work (textarea visible)
    await expect(page.getByLabel('Message input')).toBeVisible();

    // ── Final: No critical errors ──
    const criticalErrors = errors.filter(e =>
      !e.includes('Download the React DevTools') &&
      !e.includes('font') &&
      !e.includes('favicon') &&
      !e.includes('net::ERR') &&
      !e.includes('ResizeObserver')
    );
    expect(criticalErrors.length, `Errors: ${criticalErrors.join('; ')}`).toBe(0);
  });

  test('app handles API failure gracefully', async ({ page }) => {
    await page.route('**/api/**', async route => {
      if (route.request().url().includes('/api/projects')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ projects: [] }),
        });
      } else {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'Internal server error' }),
        });
      }
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // App renders with sidebar and main content
    await expect(page.locator('body')).toBeVisible();
    // The workbench tabs exist in the right panel
    await expect(page.getByRole('tab', { name: 'Sandbox' }).first()).toBeVisible({ timeout: 10000 });
  });
});

test.describe('Smoke Test - SSE Event Processing', () => {
  test('complete agent conversation with 3 tool calls', async ({ page }) => {
    await mockApiRoutes(page, {
      sseEvents: [
        { type: 'agent_start', data: {} },
        { type: 'thinking_delta', data: { text: 'Analyzing the request...' } },
        { type: 'tool_execution_start', data: { toolName: 'read', toolId: 'tool-1', args: { filePath: '/src/main.ts' } } },
        { type: 'tool_execution_end', data: { toolId: 'tool-1', toolName: 'read', result: 'console.log("hello")', error: '' } },
        { type: 'tool_execution_start', data: { toolName: 'edit', toolId: 'tool-2', args: { filePath: '/src/main.ts' } } },
        { type: 'tool_execution_end', data: { toolId: 'tool-2', toolName: 'edit', result: 'File updated', error: '' } },
        { type: 'tool_execution_start', data: { toolName: 'bash', toolId: 'tool-3', args: { command: 'npm test' } } },
        { type: 'tool_execution_end', data: { toolId: 'tool-3', toolName: 'bash', result: 'All tests passed', error: '' } },
        { type: 'text_delta', data: { text: 'I have completed the changes. All tests pass.' } },
        { type: 'agent_end', data: { input: 1000, output: 500, cache: 100 } },
      ],
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.getByLabel('Message input').fill('Fix the bug and run tests');
    await page.getByLabel('Message input').press('Enter');

    // All 3 tool names must render
    await expect(page.locator('text=read').first()).toBeVisible({ timeout: 10000 });
    await expect(page.locator('text=edit').first()).toBeVisible({ timeout: 10000 });
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 10000 });

    // Wait for full completion
    await page.waitForTimeout(8000);

    // No spinning loaders after completion
    const toolSpinners = page.locator('[data-slot="tool-fallback-trigger-icon"].animate-spin');
    const count = await toolSpinners.count();
    expect(count, 'Tool calls still spinning after completion').toBe(0);

    // Final text
    await expect(page.getByText(/completed the changes/)).toBeVisible({ timeout: 3000 });
  });
});
