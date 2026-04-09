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

    // ── Step 2: Agent tab ──
    await expect(page.getByText('Pi Agent Ready')).toBeVisible();
    await expect(page.locator('textarea')).toBeVisible();

    // ── Step 3: Send message with tool call ──
    await page.locator('textarea').fill('Test message');
    await page.locator('textarea').press('Enter');

    await expect(page.getByText('Test message')).toBeVisible({ timeout: 5000 });

    // Tool call must render with name
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 5000 });

    // Text from stream
    await expect(page.getByText(/files in your project/)).toBeVisible({ timeout: 5000 });

    // Wait for stream to finish
    await page.waitForTimeout(5000);

    // ── Step 4: Tasks tab ──
    await page.locator('.h-10.border-b button:has-text("Tasks")').click();
    await page.waitForTimeout(1000);

    await expect(page.getByText('Pending').first()).toBeVisible();
    await expect(page.getByText('Fix login bug')).toBeVisible();
    await expect(page.getByText('Add dark mode')).toBeVisible();

    // ── Step 5: Desktop tab ──
    await page.locator('.h-10.border-b button:has-text("Desktop")').click();
    await page.waitForTimeout(2000);

    // Desktop should render something
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();

    // ── Step 6: Scheduler tab ──
    await page.locator('.h-10.border-b button:has-text("Scheduler")').click();
    await page.waitForTimeout(1000);

    await expect(page.getByText('Daily tests')).toBeVisible({ timeout: 5000 });

    // ── Step 7: Back to Agent ──
    await page.locator('.h-10.border-b button:has-text("Agent")').click();
    await page.waitForTimeout(500);

    // Agent tab should still work (textarea visible)
    await expect(page.locator('textarea')).toBeVisible();

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

    await expect(page.locator('body')).toBeVisible();
    await expect(page.locator('.h-10.border-b button:has-text("Agent")')).toBeVisible();
    await expect(page.locator('.h-10.border-b button:has-text("Tasks")')).toBeVisible();
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

    await page.locator('textarea').fill('Fix the bug and run tests');
    await page.locator('textarea').press('Enter');

    // All 3 tool names must render
    await expect(page.locator('text=read').first()).toBeVisible({ timeout: 10000 });
    await expect(page.locator('text=edit').first()).toBeVisible({ timeout: 10000 });
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 10000 });

    // Reasoning block
    await expect(page.getByText('Reasoning')).toBeVisible({ timeout: 10000 });

    // Wait for full completion
    await page.waitForTimeout(8000);

    // No spinning loaders after completion
    const toolSpinners = page.locator('.border.border-white\\/5.bg-zinc-950 .animate-spin');
    const count = await toolSpinners.count();
    expect(count, 'Tool calls still spinning after completion').toBe(0);

    // Final text
    await expect(page.getByText(/completed the changes/)).toBeVisible({ timeout: 3000 });

    // Token usage
    await expect(page.getByText(/Tokens:/).first()).toBeVisible({ timeout: 3000 });
  });
});
