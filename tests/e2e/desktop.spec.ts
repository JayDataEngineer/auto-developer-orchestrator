/**
 * Desktop Tab E2E Tests — Real Browser Rendering
 *
 * Tests the full Desktop tab pipeline in Chromium:
 *   - PiAgentView renders in left sidebar
 *   - User can type and send messages
 *   - SSE response renders assistant text, thinking, tool calls
 *   - Center panel shows ComputerUseTab
 *
 * Uses Playwright route mocking — no real backend needed.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, buildSSEStream, SSE_SIMPLE_REPLY, SSE_WITH_TOOL_CALL, SSE_WITH_THINKING_AND_TOOLS } from './fixtures';

test.describe('Desktop Tab - Chat Sidebar', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    // Wait for projects to load and auto-select
    await page.waitForTimeout(1500);
  });

  test('switches to Desktop tab and renders PiAgentView', async ({ page }) => {
    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(1000);

    // PiAgentView should render the textarea
    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });
  });

  test('sends a message and sees the user text rendered', async ({ page }) => {
    // Route the prompt to return a simple reply
    await mockApiRoutes(page, { sseEvents: SSE_SIMPLE_REPLY });

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(1000);

    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    // Type and send
    await textarea.fill('Hello from desktop');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // User message must be visible in the sidebar
    await expect(page.getByText('Hello from desktop')).toBeVisible({ timeout: 5000 });
  });

  test('sees assistant response text after sending', async ({ page }) => {
    await mockApiRoutes(page, { sseEvents: SSE_SIMPLE_REPLY });

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(1000);

    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    await textarea.fill('Help me');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // Assistant text from SSE_SIMPLE_REPLY: "I will help you with that."
    await expect(page.getByText(/help you with that/)).toBeVisible({ timeout: 5000 });
  });

  test('sees thinking block rendered', async ({ page }) => {
    await mockApiRoutes(page, { sseEvents: SSE_WITH_THINKING_AND_TOOLS });

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(1000);

    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    await textarea.fill('Analyze the code');
    await textarea.press('Enter');
    await page.waitForTimeout(3000);

    // ReasoningBlock renders "Reasoning" header always visible
    await expect(page.getByText('Reasoning')).toBeVisible({ timeout: 5000 });
    // Character count shows
    await expect(page.getByText(/chars/)).toBeVisible({ timeout: 5000 });
    // Click to expand and see thinking content
    await page.getByText('Reasoning').click();
    await expect(page.getByText(/analyze the codebase/)).toBeVisible({ timeout: 5000 });
  });

  test('sees tool calls rendered', async ({ page }) => {
    await mockApiRoutes(page, { sseEvents: SSE_WITH_TOOL_CALL });

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(1000);

    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    await textarea.fill('List files');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // Tool call name "bash" from SSE_WITH_TOOL_CALL
    await expect(page.getByText('bash')).toBeVisible({ timeout: 5000 });
    // Assistant text after tool
    await expect(page.getByText(/files in your project/)).toBeVisible({ timeout: 5000 });
  });

  test('no crash errors on Desktop tab', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', err => errors.push(err.message));

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(2000);

    const criticalErrors = errors.filter(e =>
      !e.includes('Download the React DevTools') &&
      !e.includes('font') &&
      !e.includes('favicon') &&
      !e.includes('net::ERR') &&
      !e.includes('ResizeObserver')
    );
    expect(criticalErrors.length, `Desktop errors: ${criticalErrors.join('; ')}`).toBe(0);
  });

  test('rapid tab switching does not crash', async ({ page }) => {
    for (let i = 0; i < 3; i++) {
      await page.locator('button:has-text("Agent")').click();
      await page.waitForTimeout(200);
      await page.locator('button:has-text("Desktop")').click();
      await page.waitForTimeout(200);
      await page.locator('button:has-text("Tasks")').click();
      await page.waitForTimeout(200);
    }

    // End on Desktop — textarea should still work
    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(500);
    await expect(page.locator('textarea')).toBeVisible({ timeout: 5000 });
  });
});

test.describe('Desktop Tab - Center Panel', () => {
  test('shows ComputerUseTab in center when on Desktop tab', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(2000);

    // The center panel should contain desktop/VNC-related content
    // Look for the Monitor icon or "Start Desktop" or "Connecting" text
    const monitorIcon = page.locator('.lucide-monitor');
    const startDesktop = page.getByText('Start Desktop');
    const connecting = page.getByText('Connecting');

    const hasContent = await Promise.all([
      monitorIcon.first().isVisible().catch(() => false),
      startDesktop.isVisible().catch(() => false),
      connecting.isVisible().catch(() => false),
    ]);

    expect(hasContent.some(Boolean)).toBe(true);
  });
});
