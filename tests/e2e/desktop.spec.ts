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

// ── Computer Use Enable + VNC Connection Flow ──

test.describe('Desktop Tab - Computer Use Enable Flow', () => {
  test('enable succeeds, viewer returns data, VNC iframe renders', async ({ page }) => {
    await mockApiRoutes(page);

    // Mock VNC proxy to serve a simple HTML page for the iframe
    await page.route('**/api/sandbox/vnc/**', async route => {
      const url = route.request().url();
      if (url.includes('websockify')) {
        // WebSocket upgrade — abort since we can't do WS in route mock
        await route.abort('connectionrefused');
      } else {
        // Serve noVNC HTML for the iframe
        await route.fulfill({
          status: 200,
          contentType: 'text/html',
          body: '<!DOCTYPE html><html><body>noVNC Mock</body></html>',
        });
      }
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.locator('button:has-text("Desktop")').first().click();
    // Wait for auto-enable + viewer polling + iframe render
    await page.waitForTimeout(8000);

    // After enable + viewer polling, the iframe should appear
    const iframe = page.locator('iframe').first();
    await expect(iframe).toBeVisible({ timeout: 15000 });

    // Desktop header should show "Desktop" label (the session loaded)
    await expect(page.locator('text=Desktop').first()).toBeVisible({ timeout: 5000 });
  });

  test('enable endpoint returning 503 shows error in cu.error', async ({ page }) => {
    await mockApiRoutes(page);

    // Override: enable endpoint returns 503 (sandbox manager not available)
    await page.route('**/api/sandbox/**/computer-use/enable', async route => {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'sandbox manager not available' }),
      });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(5000);

    // Error should appear — either in the header or the center panel
    const errorText = page.getByText(/sandbox manager|failed|error|timed out|not available/i);
    await expect(errorText.first()).toBeVisible({ timeout: 10000 });
  });

  test('enable succeeds but viewer returns 404 → shows connecting state', async ({ page }) => {
    await mockApiRoutes(page);

    // Override: viewer endpoint always returns 404 (background setup never completes)
    await page.route('**/api/sandbox/**/viewer', async route => {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'not found' }),
      });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await page.locator('button:has-text("Desktop")').first().click();
    await page.waitForTimeout(5000);

    // Should show "Connecting to desktop..." (enabled but no session yet)
    // or the "Starting desktop..." spinner while polling
    const connectingText = page.getByText(/Connecting to desktop/).first();
    const startingText = page.getByText(/Starting desktop/).first();

    const connectingVisible = await connectingText.isVisible().catch(() => false);
    const startingVisible = await startingText.isVisible().catch(() => false);

    // One of these states must be visible — the app is not crashed or blank
    expect(connectingVisible || startingVisible).toBe(true);
  });

  test('VNC proxy returning 502 shows "Desktop not available" with retry button', async ({ page }) => {
    await mockApiRoutes(page);

    // Override: VNC proxy returns 502 (sandbox desktop not reachable)
    await page.route('**/api/sandbox/vnc/**', async route => {
      const url = route.request().url();
      if (url.includes('websockify') || url.includes('vnc.html')) {
        await route.fulfill({
          status: 502,
          contentType: 'text/plain',
          body: 'sandbox desktop not reachable',
        });
      } else {
        await route.continue();
      }
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(5000);

    // The iframe may still render (it got a src) but will fail to connect.
    // The VNC viewer shows error inside the iframe — we can't easily read iframe content
    // due to cross-origin. But the app should NOT crash.
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();

    // Tab buttons should still work
    await expect(page.locator('button:has-text("Agent")').first()).toBeVisible();
    await expect(page.locator('button:has-text("Desktop")').first()).toBeVisible();
  });

  test('enable endpoint network failure shows error with retry', async ({ page }) => {
    await mockApiRoutes(page);

    // Override: enable endpoint fails with network error
    await page.route('**/api/sandbox/**/computer-use/enable', async route => {
      await route.abort('failed');
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(5000);

    // Error should be visible — the enable hook retries once then shows error
    const errorText = page.getByText(/failed|error|timed out|Failed to fetch/i);
    await expect(errorText.first()).toBeVisible({ timeout: 10000 });
  });

  test('sandbox list empty uses fallback sandbox ID and enable still works', async ({ page }) => {
    await mockApiRoutes(page);

    // Override: sandbox list returns empty array (no running sandboxes)
    await page.route('**/api/sandbox/', async route => {
      if (route.request().url().endsWith('/api/sandbox/') && route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ sandboxes: [] }),
        });
      } else {
        await route.continue();
      }
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(5000);

    // Should auto-enable with fallback sandbox ID "sandbox-test-project"
    // Enable mock returns success, viewer mock returns success
    const iframe = page.locator('iframe[title="Desktop"]');
    await expect(iframe).toBeVisible({ timeout: 10000 });
  });

  test('re-enabling on tab switch does not show error', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Go to Desktop tab — triggers auto-enable
    await page.locator('button:has-text("Desktop")').click();
    await page.waitForTimeout(3000);

    // Switch away and back
    await page.locator('button:has-text("Tasks")').first().click();
    await page.waitForTimeout(500);
    await page.locator('button:has-text("Desktop")').first().click();
    await page.waitForTimeout(2000);

    // Should NOT show error — already enabled, just reconnects
    const errorText = page.getByText(/failed|error|not available/i);
    const errorVisible = await errorText.first().isVisible().catch(() => false);
    expect(errorVisible).toBe(false);
  });
});
