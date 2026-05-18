/**
 * Edge Cases E2E Tests
 *
 * Tests console errors, empty data states, API failures,
 * rapid interactions, and layout integrity.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, MOCK_PROJECTS } from './fixtures';

// ── Console Errors Across All Tabs ──

test.describe('No Console Errors', () => {
  const allowedErrors = [
    'Download the React DevTools',
    'font',
    'favicon',
    'net::ERR',
    'ResizeObserver',
  ];

  function isCriticalError(msg: string): boolean {
    return !allowedErrors.some(allowed => msg.includes(allowed));
  }

  test('Chat view produces no critical console errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', err => errors.push(err.message));

    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);

    const critical = errors.filter(isCriticalError);
    expect(critical.length, `Chat errors: ${critical.join('; ')}`).toBe(0);
  });

  test('Scheduler tab produces no critical console errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', err => errors.push(err.message));

    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.getByRole('tab', { name: 'Scheduler' }).click();
    await page.waitForTimeout(2000);

    const critical = errors.filter(isCriticalError);
    expect(critical.length, `Scheduler errors: ${critical.join('; ')}`).toBe(0);
  });

  test('Agents tab produces no critical console errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });
    page.on('pageerror', err => errors.push(err.message));

    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.getByRole('tab', { name: 'Agents' }).click();
    await page.waitForTimeout(2000);

    const critical = errors.filter(isCriticalError);
    expect(critical.length, `Agents errors: ${critical.join('; ')}`).toBe(0);
  });
});

// ── Empty Data States ──

test.describe('Empty Data States', () => {
  test('no projects shows empty sidebar state', async ({ page }) => {
    // Mock all routes manually
    await page.route('**/api/projects', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ projects: [] }),
      });
    });
    await page.route('**/api/config/ai', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
    });
    await page.route('**/api/github/user', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ connected: false }) });
    });
    await page.route('**/api/pi/**', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
    });
    await page.route('**/api/pux/**', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
    });
    await page.route('**/api/scheduler**', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ jobs: [] }) });
    });
    await page.route('**/api/workers/**', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/sandbox/**', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
    });
    await page.route('**/api/cli/**', async route => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({}) });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // "No projects yet" should appear in the sidebar
    await expect(page.getByText('No projects yet')).toBeVisible({ timeout: 5000 });
  });
});

// ── API Failure Handling ──

test.describe('API Failure Handling', () => {
  test('all API failures show app without crash', async ({ page }) => {
    await page.route('**/api/**', async route => {
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Internal server error' }),
      });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);

    // App should still render something
    await expect(page.locator('body')).toBeVisible();
  });

  test('tasks API failure does not crash app', async ({ page }) => {
    await mockApiRoutes(page);
    // Override tasks API to fail
    await page.route('**/api/pi/tasks/list**', async route => {
      await route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Tasks service unavailable' }),
      });
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Should not crash
    await expect(page.locator('body')).toBeVisible();
  });
});

// ── Rapid Interaction Stress Tests ──

test.describe('Rapid Interactions', () => {
  test('rapid tab switching 5 times without crash', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Switch between tabs rapidly
    for (let round = 0; round < 5; round++) {
      await page.getByRole('tab', { name: 'Editor' }).click();
      await page.waitForTimeout(150);
      await page.getByRole('tab', { name: 'Agents' }).click();
      await page.waitForTimeout(150);
      await page.getByRole('tab', { name: 'Sandbox' }).click();
      await page.waitForTimeout(150);
    }

    // Final state should be valid
    await expect(page.locator('body')).toBeVisible();
  });

  test('rapid sidebar toggle without crash', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Toggle the sidebar rapidly using the sidebar trigger
    for (let i = 0; i < 3; i++) {
      const sidebarTrigger = page.locator('[data-sidebar="trigger"]').first();
      await sidebarTrigger.click();
      await page.waitForTimeout(300);
    }

    // App should still work
    await expect(page.locator('body')).toBeVisible();
  });
});

// ── Layout Integrity ──

test.describe('Layout Integrity', () => {
  test('app root renders without crash', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await expect(page.locator('body')).toBeVisible();
  });

  test('header renders with all elements', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Workbench tabs
    await expect(page.getByRole('tab', { name: 'Sandbox' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Editor' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Scheduler' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Agents' })).toBeVisible();

    // Pux branding in sidebar
    await expect(page.getByText('Pux', { exact: true }).first()).toBeVisible({ timeout: 5000 });

    // Textarea in chat
    await expect(page.getByLabel('Message input')).toBeVisible();
  });

  test('tab content area is visible', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    // Body is visible and contains tab content
    await expect(page.locator('body')).toBeVisible();

    // Tab buttons should be visible (proving layout is rendered)
    await expect(page.getByRole('tab', { name: 'Sandbox' })).toBeVisible();
  });
});
