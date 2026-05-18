/**
 * Desktop / Sandbox Tab E2E Tests
 *
 * Tests the Sandbox tab (VNCViewer) and workbench tab switching.
 * Uses Playwright route mocking -- no real backend needed.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, SSE_SIMPLE_REPLY, SSE_WITH_TOOL_CALL, SSE_WITH_THINKING_AND_TOOLS } from './fixtures';

test.describe('Sandbox Tab - Chat Thread', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);
  });

  test('chat textarea is always visible regardless of active tab', async ({ page }) => {
    // Chat is the left panel, always visible
    const textarea = page.getByLabel('Message input');
    await expect(textarea).toBeVisible({ timeout: 5000 });
  });

  test('sends a message and sees the user text rendered', async ({ page }) => {
    await mockApiRoutes(page, { sseEvents: SSE_SIMPLE_REPLY });

    const textarea = page.getByLabel('Message input');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    // Type and send
    await textarea.fill('Hello from sandbox');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // User message must be visible
    await expect(page.getByText('Hello from sandbox')).toBeVisible({ timeout: 5000 });
  });

  test('sees assistant response text after sending', async ({ page }) => {
    await mockApiRoutes(page, { sseEvents: SSE_SIMPLE_REPLY });

    const textarea = page.getByLabel('Message input');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    await textarea.fill('Help me');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // Assistant text from SSE_SIMPLE_REPLY: "I will help you with that."
    await expect(page.getByText(/help you with that/)).toBeVisible({ timeout: 5000 });
  });

  test('sees thinking block rendered', async ({ page }) => {
    await mockApiRoutes(page, { sseEvents: SSE_WITH_THINKING_AND_TOOLS });

    const textarea = page.getByLabel('Message input');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    await textarea.fill('Analyze the code');
    await textarea.press('Enter');
    await page.waitForTimeout(3000);

    // Reasoning trigger shows "Reasoning" text
    await expect(page.getByText('Reasoning')).toBeVisible({ timeout: 5000 });
    // Click to expand and see thinking content
    await page.getByText('Reasoning').first().click();
    await expect(page.getByText(/analyze the codebase/)).toBeVisible({ timeout: 5000 });
  });

  test('sees tool calls rendered', async ({ page }) => {
    await mockApiRoutes(page, { sseEvents: SSE_WITH_TOOL_CALL });

    const textarea = page.getByLabel('Message input');
    await expect(textarea).toBeVisible({ timeout: 5000 });

    await textarea.fill('List files');
    await textarea.press('Enter');
    await page.waitForTimeout(2000);

    // Tool call name "bash" from SSE_WITH_TOOL_CALL
    await expect(page.getByText('bash')).toBeVisible({ timeout: 5000 });
    // Assistant text after tool
    await expect(page.getByText(/files in your project/)).toBeVisible({ timeout: 5000 });
  });

  test('no crash errors on any tab', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', err => errors.push(err.message));

    // Switch through all tabs
    const tabs = ['Editor', 'Scheduler', 'Agents', 'Sandbox'];
    for (const tab of tabs) {
      await page.getByRole('tab', { name: tab }).click();
      await page.waitForTimeout(500);
    }

    const criticalErrors = errors.filter(e =>
      !e.includes('Download the React DevTools') &&
      !e.includes('font') &&
      !e.includes('favicon') &&
      !e.includes('net::ERR') &&
      !e.includes('ResizeObserver')
    );
    expect(criticalErrors.length, `Tab switching errors: ${criticalErrors.join('; ')}`).toBe(0);
  });

  test('rapid tab switching does not crash', async ({ page }) => {
    for (let i = 0; i < 3; i++) {
      await page.getByRole('tab', { name: 'Sandbox' }).click();
      await page.waitForTimeout(200);
      await page.getByRole('tab', { name: 'Agents' }).click();
      await page.waitForTimeout(200);
      await page.getByRole('tab', { name: 'Scheduler' }).click();
      await page.waitForTimeout(200);
    }

    // End on Sandbox -- textarea should still work
    await page.getByRole('tab', { name: 'Sandbox' }).click();
    await page.waitForTimeout(500);
    await expect(page.getByLabel('Message input')).toBeVisible({ timeout: 5000 });
  });
});

test.describe('Sandbox Tab - VNC Viewer', () => {
  test('shows sandbox state in Sandbox tab', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Sandbox tab is default -- VNC viewer shows something
    const monitorIcon = page.locator('.lucide-monitor');
    const noSandbox = page.getByText('No sandbox for this project');
    const detecting = page.getByText('Detecting sandbox');

    const hasContent = await Promise.all([
      monitorIcon.first().isVisible().catch(() => false),
      noSandbox.isVisible().catch(() => false),
      detecting.isVisible().catch(() => false),
    ]);

    expect(hasContent.some(Boolean)).toBe(true);
  });
});

// ── Sandbox / VNC Connection Flow ──

test.describe('Sandbox Tab - VNC Connection Flow', () => {
  test('sandbox list empty shows no sandbox message', async ({ page }) => {
    await mockApiRoutes(page);

    // Sandbox list returns empty
    await page.route('**/api/sandbox/', async route => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        });
      } else {
        await route.continue();
      }
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Should show "No sandbox for this project"
    await expect(page.getByText('No sandbox for this project')).toBeVisible({ timeout: 10000 });
  });

  test('sandbox with active desktop session shows iframe', async ({ page }) => {
    await mockApiRoutes(page);

    // Mock sandbox list with active desktop session
    await page.route('**/api/sandbox/', async route => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([{
            id: 'sandbox-test-project',
            project_path: '/home/user/test-project',
            status: 'running',
            mode: 'desktop',
            desktop_session: { is_active: true, mode: 'desktop', novnc_port: 6080 },
          }]),
        });
      } else {
        await route.continue();
      }
    });

    // Mock sandbox detail endpoint
    await page.route('**/api/sandbox/sandbox-test-project', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'sandbox-test-project',
          project_path: '/home/user/test-project',
          status: 'running',
          mode: 'desktop',
          desktop_session: { is_active: true, mode: 'desktop', novnc_port: 6080 },
        }),
      });
    });

    // Mock VNC health endpoint
    await page.route('**/api/sandbox/**/vnc-health', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ healthy: true }),
      });
    });

    // Mock VNC HTML endpoint
    await page.route('**/api/sandbox/vnc/**', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<!DOCTYPE html><html><body>noVNC Mock</body></html>',
      });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);

    // iframe should appear with VNC viewer
    const iframe = page.locator('iframe[title="Sandbox VNC"]');
    await expect(iframe).toBeVisible({ timeout: 15000 });
  });

  test('re-switching tabs does not show error', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Switch away and back
    await page.getByRole('tab', { name: 'Scheduler' }).click();
    await page.waitForTimeout(500);
    await page.getByRole('tab', { name: 'Sandbox' }).click();
    await page.waitForTimeout(2000);

    // Should NOT show error
    const errorText = page.getByText(/failed|error|not available/i);
    const errorVisible = await errorText.first().isVisible().catch(() => false);
    expect(errorVisible).toBe(false);
  });
});
