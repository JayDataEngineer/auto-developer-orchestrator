/**
 * E2E tests for the Lit web SPA (src/web/).
 *
 * Tests slash commands, chat input, message rendering, and SSE streaming.
 * Uses Playwright with mocked backend routes.
 */
import { test, expect, type Page, type Route } from '@playwright/test';

const WEB_URL = 'http://localhost:5175';
const BACKEND_URL = 'http://localhost:3847';

// ── SSE helpers ──

function sseEvent(type: string, data: Record<string, unknown>): string {
	return `event: ${type}\ndata: ${JSON.stringify(data)}\n\n`;
}

const SSE_SIMPLE = [
	sseEvent('agent_spawned', { agentId: 'agent-test' }),
	sseEvent('agent_start', {}),
	sseEvent('text_delta', { text: 'Hello! ' }),
	sseEvent('text_delta', { text: 'I can help.' }),
	sseEvent('agent_end', { input: 50, output: 30, cache: 0 }),
].join('');

// ── Mock routes for the Lit SPA ──

async function mockPuxRoutes(page: Page, opts: { sseBody?: string } = {}) {
	// Conversations list (sidebar)
	await page.route('**/api/pux/conversations**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([]),
		});
	});

	// SSE prompt endpoint
	await page.route('**/api/pux/prompt', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'text/event-stream',
			headers: { 'Cache-Control': 'no-cache' },
			body: opts.sseBody ?? SSE_SIMPLE,
		});
	});

	// Scheduler
	await page.route('**/api/scheduler**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ jobs: [] }),
		});
	});
}

// ── Test suite ──

test.describe('Lit Web SPA', () => {
	test.beforeEach(async ({ page }) => {
		await mockPuxRoutes(page);
		await page.goto(WEB_URL, { waitUntil: 'networkidle', timeout: 10000 });
		await page.waitForTimeout(1000);
	});

	test('renders the chat input and empty state', async ({ page }) => {
		const textarea = page.locator('textarea');
		await expect(textarea).toBeVisible();
		await expect(textarea).toHaveAttribute('placeholder', 'Message Pux...');
		await expect(page.locator('.send-btn')).toBeVisible();
		await expect(page.locator('.empty-state')).toBeVisible();
	});

	test('sends a message and receives SSE response', async ({ page }) => {
		const textarea = page.locator('textarea');
		await textarea.fill('Hello Pux');
		await textarea.press('Enter');

		// User message appears
		await expect(page.locator('.msg.user')).toBeVisible({ timeout: 3000 });
		// Assistant response appears after SSE
		await expect(page.locator('.msg.assistant .text')).toContainText('Hello! I can help.', { timeout: 3000 });
	});

	test('does NOT double-send on Enter', async ({ page }) => {
		let promptCallCount = 0;
		await page.route('**/api/pux/prompt', async route => {
			promptCallCount++;
			await route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				body: SSE_SIMPLE,
			});
		});

		const textarea = page.locator('textarea');
		await textarea.fill('test double send');
		await textarea.press('Enter');

		await page.waitForTimeout(2000);
		expect(promptCallCount).toBeLessThanOrEqual(1);
	});

	test('slash /help shows local help, does NOT hit backend', async ({ page }) => {
		let promptCalled = false;
		await page.route('**/api/pux/prompt', async route => {
			promptCalled = true;
			await route.fulfill({ status: 200, contentType: 'text/event-stream', body: SSE_SIMPLE });
		});

		const textarea = page.locator('textarea');
		await textarea.fill('/help');
		await textarea.press('Enter');

		await page.waitForTimeout(1000);

		// Help text should appear as assistant message
		await expect(page.locator('.msg.assistant .text')).toContainText('/help');
		// Should NOT have called the prompt endpoint
		expect(promptCalled).toBe(false);
	});

	test('slash /model shows local info, does NOT hit backend', async ({ page }) => {
		let promptCalled = false;
		await page.route('**/api/pux/prompt', async route => {
			promptCalled = true;
			await route.fulfill({ status: 200, contentType: 'text/event-stream', body: SSE_SIMPLE });
		});

		const textarea = page.locator('textarea');
		await textarea.fill('/model');
		await textarea.press('Enter');

		await page.waitForTimeout(1000);

		await expect(page.locator('.msg.assistant .text')).toContainText('Current model');
		expect(promptCalled).toBe(false);
	});

	test('slash /session shows local info, does NOT hit backend', async ({ page }) => {
		let promptCalled = false;
		await page.route('**/api/pux/prompt', async route => {
			promptCalled = true;
			await route.fulfill({ status: 200, contentType: 'text/event-stream', body: SSE_SIMPLE });
		});

		const textarea = page.locator('textarea');
		await textarea.fill('/session');
		await textarea.press('Enter');

		await page.waitForTimeout(1000);

		await expect(page.locator('.msg.assistant .text')).toContainText('Session');
		expect(promptCalled).toBe(false);
	});

	test('slash autocomplete via Tab executes command', async ({ page }) => {
		let promptCalled = false;
		await page.route('**/api/pux/prompt', async route => {
			promptCalled = true;
			await route.fulfill({ status: 200, contentType: 'text/event-stream', body: SSE_SIMPLE });
		});

		const textarea = page.locator('textarea');
		await textarea.fill('/mod');

		// Wait for autocomplete popup
		await expect(page.locator('.slash-item')).toBeVisible({ timeout: 2000 });

		// Press Tab to autocomplete
		await textarea.press('Tab');
		await page.waitForTimeout(500);

		// Should show model info (local), not hit backend
		await expect(page.locator('.msg.assistant .text')).toContainText('Current model');
		expect(promptCalled).toBe(false);
	});

	test('slash /new resets chat', async ({ page }) => {
		// First send a message
		const textarea = page.locator('textarea');
		await textarea.fill('hello');
		await textarea.press('Enter');
		await expect(page.locator('.msg.user')).toBeVisible({ timeout: 3000 });

		// Now reset
		await textarea.fill('/new');
		await textarea.press('Enter');
		await page.waitForTimeout(500);

		// Messages should be gone
		await expect(page.locator('.msg')).toHaveCount(0);
	});

	test('textarea auto-grows with multi-line input', async ({ page }) => {
		const textarea = page.locator('textarea');
		const initialHeight = await textarea.evaluate((el: HTMLTextAreaElement) => el.offsetHeight);

		// Type multiple lines
		await textarea.fill('line1\nline2\nline3\nline4\nline5');
		// Trigger input event
		await textarea.dispatchEvent('input');

		await page.waitForTimeout(200);
		const newHeight = await textarea.evaluate((el: HTMLTextAreaElement) => el.offsetHeight);
		expect(newHeight).toBeGreaterThan(initialHeight);
	});
});
