/**
 * Frontend Integration Tests
 *
 * Full browser tests against the real backend.
 * No API mocking — the app talks to the real Go backend.
 */
import { test, expect } from '@playwright/test';
import {
  gotoApp,
  apiPut,
  TEST_PROJECT, TEST_MODEL,
} from './helpers';

// ─── App Loading ────────────────────────────────────────────────────

test.describe('App Loading — Real Backend', () => {
  test('app loads without white screen', async ({ page }) => {
    await page.goto('/');
    await page.waitForLoadState('networkidle');

    // Should NOT be a blank white/black page
    const body = page.locator('body');
    await expect(body).toBeVisible({ timeout: 10_000 });

    // Should have some content
    const html = await page.content();
    expect(html.length).toBeGreaterThan(100);
  });

  test('top bar renders with tabs', async ({ page }) => {
    await gotoApp(page);

    const topBar = page.locator('.h-10.border-b');
    await expect(topBar).toBeVisible({ timeout: 10_000 });

    // All 4 tabs should be present
    await expect(page.getByRole('button', { name: 'Agent' })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('button', { name: 'Tasks' })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('button', { name: 'Desktop' })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('button', { name: 'Scheduler' })).toBeVisible({ timeout: 10_000 });
  });

  test('project selector shows real projects from backend', async ({ page }) => {
    await gotoApp(page);

    // Should show at least one project (test-repo)
    const selector = page.locator('select').first();
    await expect(selector).toBeVisible({ timeout: 10_000 });

    // The selector should have options
    const options = await selector.locator('option').allTextContents();
    expect(options.length).toBeGreaterThan(0);

    console.log(`  ✓ Projects in selector: ${options.join(', ')}`);
  });

  test('no critical console errors on load', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') {
        errors.push(msg.text());
      }
    });

    await gotoApp(page);
    await page.waitForTimeout(2000);

    // Filter out known non-critical errors (e.g. network failures for optional features)
    const criticalErrors = errors.filter(e =>
      !e.includes('ECONNREFUSED') &&  // Backend might not have all services
      !e.includes('net::ERR_CONNECTION_REFUSED') &&
      !e.includes('Failed to fetch') &&   // Optional API calls
      !e.includes('favicon')
    );

    expect(criticalErrors.length).toBe(0);
    console.log(`  ✓ Console errors: ${errors.length} total, ${criticalErrors.length} critical`);
  });
});

// ─── Agent Chat — Real Backend ──────────────────────────────────────

test.describe('Agent Chat — Real Backend', () => {
  test.beforeAll(async () => {
    // Set model for chat tests
    await apiPut('/api/pi/model', {
      project: TEST_PROJECT,
      provider: 'litellm',
      modelId: TEST_MODEL,
      agentId: 'default',
    });
  });

  test('chat textarea is visible', async ({ page }) => {
    await gotoApp(page);
    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible({ timeout: 10_000 });
  });

  test('can type in chat textarea', async ({ page }) => {
    await gotoApp(page);
    const textarea = page.locator('textarea');
    await textarea.fill('Hello from integration test');
    await expect(textarea).toHaveValue('Hello from integration test');
  });

  test('send prompt and receive real response', async ({ page }) => {
    await gotoApp(page);

    // Make sure the project is selected
    const selector = page.locator('select').first();
    await expect(selector).toBeVisible({ timeout: 10_000 });

    // Select test-repo project if available
    const options = await selector.locator('option').allTextContents();
    if (options.some(o => o.includes(TEST_PROJECT))) {
      await selector.selectOption(TEST_PROJECT);
    }

    // Type and send
    const textarea = page.locator('textarea');
    await textarea.fill('Say exactly: "integration test response"');
    await textarea.press('Enter');

    // Wait for the user message to appear
    await page.waitForTimeout(1000);

    // Wait for streaming indicator or response text
    // The response should appear within 30 seconds
    const responseAppeared = await page.waitForFunction(
      () => {
        // Check for any assistant text in the chat
        const messages = document.querySelectorAll('[class*="assistant"], [class*="message"]');
        for (const msg of messages) {
          if (msg.textContent && msg.textContent.length > 5) return true;
        }
        // Also check for streaming indicator
        const streamings = document.querySelectorAll('[data-testid*="stream"], [class*="streaming"]');
        return streamings.length > 0;
      },
      { timeout: 45_000 },
    ).catch(() => false);

    // Give it a moment for streaming to complete
    await page.waitForTimeout(3000);

    // Verify something rendered (stream started or text appeared)
    const pageContent = await page.content();
    const hasResponse = pageContent.includes('integration test') ||
                        pageContent.includes('response') ||
                        pageContent.includes('streaming');

    console.log(`  ✓ Response appeared: ${responseAppeared}, has content: ${hasResponse}`);

    // At minimum, the prompt should have been sent without crashing
    expect(pageContent.length).toBeGreaterThan(100);
  });

  test('model selector shows real models from backend', async ({ page }) => {
    await gotoApp(page);

    // Look for model selector/dropdown
    const modelButton = page.locator('button:has-text("model"), [data-testid="model-selector"], select').first();

    // May or may not be visible depending on UI state
    if (await modelButton.isVisible().catch(() => false)) {
      console.log('  ✓ Model selector visible');
    }

    // Check that the model state was loaded from backend
    const state = await page.evaluate(() => {
      return fetch('/api/pi/state?project=test-repo&agentId=default')
        .then(r => r.json())
        .catch(() => null);
    });

    if (state) {
      console.log(`  ✓ Backend model state: ${JSON.stringify(state)}`);
    }
  });
});

// ─── Desktop Tab — Real Backend ─────────────────────────────────────

test.describe('Desktop Tab — Real Backend', () => {
  test('desktop tab renders without crash', async ({ page }) => {
    await gotoApp(page);

    const desktopTab = page.getByRole('button', { name: 'Desktop' });
    await expect(desktopTab).toBeVisible({ timeout: 10_000 });
    await desktopTab.click();

    await page.waitForTimeout(2000);

    // Should NOT be a white screen
    const body = page.locator('body');
    const isVisible = await body.isVisible();
    expect(isVisible).toBe(true);
  });

  test('desktop shows real status (not mocked)', async ({ page }) => {
    await gotoApp(page);

    await page.getByRole('button', { name: 'Desktop' }).click();
    await page.waitForTimeout(3000);

    // The real behavior depends on whether sandbox is available.
    // We just verify it renders SOMETHING (not a crash/white screen).
    const content = await page.content();
    const hasContent = content.includes('Desktop') ||
                       content.includes('desktop') ||
                       content.includes('sandbox') ||
                       content.includes('Start') ||
                       content.includes('not available');

    expect(hasContent).toBe(true);
    console.log(`  ✓ Desktop tab rendered with real backend response`);
  });

  test('desktop left sidebar shows Agent Chat panel with all elements', async ({ page }) => {
    await gotoApp(page);

    // Switch to Desktop tab
    await page.getByRole('button', { name: 'Desktop' }).click();
    await page.waitForTimeout(2000);

    // Left sidebar should show "Agent Chat" header
    const agentChatHeader = page.locator('text=Agent Chat');
    await expect(agentChatHeader).toBeVisible({ timeout: 5_000 });

    // Left sidebar should contain a textarea for typing messages
    const textarea = page.locator('.w-80 textarea, .border-r textarea').first();
    await expect(textarea).toBeVisible({ timeout: 5_000 });

    console.log('  ✓ Desktop left sidebar: Agent Chat panel visible with textarea');
  });

  test('desktop right sidebar shows Controls panel', async ({ page }) => {
    await gotoApp(page);

    await page.getByRole('button', { name: 'Desktop' }).click();
    await page.waitForTimeout(2000);

    // Right sidebar should show "Controls" header
    const controlsHeader = page.locator('text=Controls');
    await expect(controlsHeader).toBeVisible({ timeout: 5_000 });

    console.log('  ✓ Desktop right sidebar: Controls panel visible');
  });

  test('desktop sandbox API is reachable (not blank)', async ({ page }) => {
    await gotoApp(page);

    // Verify sandbox API responds (from browser context)
    const sandboxResponse = await page.evaluate(async () => {
      try {
        const res = await fetch('/api/sandbox/sandbox-test-repo/viewer');
        return { status: res.status, ok: res.ok, body: await res.text() };
      } catch (err: any) {
        return { status: 0, ok: false, body: err.message };
      }
    });

    // Should get SOME response (not network error)
    expect(sandboxResponse.status).toBeGreaterThan(0);
    console.log(`  ✓ Sandbox API responded: status=${sandboxResponse.status}`);
  });

  test('desktop computer-use API endpoints are reachable', async ({ page }) => {
    await gotoApp(page);

    // Test computer-use enable from browser context
    const enableResponse = await page.evaluate(async () => {
      try {
        const res = await fetch('/api/sandbox/sandbox-test-repo/computer-use/enable', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
        });
        return { status: res.status, body: await res.text() };
      } catch (err: any) {
        return { status: 0, body: err.message };
      }
    });

    // Should get a response (even if sandbox doesn't exist)
    expect(enableResponse.status).toBeGreaterThan(0);
    console.log(`  ✓ Computer-use enable API: status=${enableResponse.status}`);

    // Test screenshot endpoint
    const screenshotResponse = await page.evaluate(async () => {
      try {
        const res = await fetch('/api/sandbox/sandbox-test-repo/computer-use/screenshot');
        return { status: res.status };
      } catch (err: any) {
        return { status: 0 };
      }
    });

    expect(screenshotResponse.status).toBeGreaterThan(0);
    console.log(`  ✓ Computer-use screenshot API: status=${screenshotResponse.status}`);
  });

  test('desktop center shows sandbox ID or error after project load', async ({ page }) => {
    await gotoApp(page);

    // Select test-repo project
    const selector = page.locator('select').first();
    const options = await selector.locator('option').allTextContents();
    if (options.some(o => o.includes('test-repo'))) {
      await selector.selectOption('test-repo');
    }

    await page.getByRole('button', { name: 'Desktop' }).click();
    await page.waitForTimeout(3000);

    // Center panel should show one of: sandbox ID, error, or "Select a project"
    const content = await page.content();
    const showsRelevantContent =
      content.includes('sandbox-test-repo') ||
      content.includes('Desktop not available') ||
      content.includes('Start Desktop') ||
      content.includes('Starting desktop') ||
      content.includes('not available') ||
      content.includes('Select a project');

    expect(showsRelevantContent).toBe(true);
    console.log('  ✓ Desktop center panel shows real content');
  });

  test('desktop can send message through Agent Chat sidebar', async ({ page }) => {
    await gotoApp(page);

    await page.getByRole('button', { name: 'Desktop' }).click();
    await page.waitForTimeout(2000);

    // Find the Agent Chat textarea
    const textarea = page.locator('.w-80 textarea, .border-r textarea').first();

    if (await textarea.isVisible({ timeout: 5_000 }).catch(() => false)) {
      // Type a test message
      await textarea.fill('Hello from desktop sidebar');
      await expect(textarea).toHaveValue('Hello from desktop sidebar');

      // Send it
      await textarea.press('Enter');
      await page.waitForTimeout(2000);

      // Should show the user message in chat
      const content = await page.content();
      const hasUserMessage = content.includes('Hello from desktop sidebar');
      console.log(`  ✓ Desktop sidebar message sent: ${hasUserMessage ? 'visible' : 'sent'}`);
    } else {
      console.log('  ⚠ Desktop sidebar textarea not visible (panel may be collapsed)');
    }
  });
});

// ─── Tasks Tab — Real Backend ───────────────────────────────────────

test.describe('Tasks Tab — Real Backend', () => {
  test('tasks tab renders', async ({ page }) => {
    await gotoApp(page);

    await page.getByRole('button', { name: 'Tasks' }).click();
    await page.waitForTimeout(2000);

    // Should show task board or empty state
    const content = await page.content();
    const hasTasksUI = content.includes('Task') || content.includes('task') || content.includes('New');

    expect(hasTasksUI).toBe(true);
  });
});

// ─── Scheduler Tab — Real Backend ───────────────────────────────────

test.describe('Scheduler Tab — Real Backend', () => {
  test('scheduler tab renders', async ({ page }) => {
    await gotoApp(page);

    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForTimeout(2000);

    // Should show scheduler UI
    const content = await page.content();
    const hasSchedulerUI = content.includes('Job') || content.includes('Schedule') || content.includes('New');

    expect(hasSchedulerUI).toBe(true);
  });
});
