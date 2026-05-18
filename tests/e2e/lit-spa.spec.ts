/**
 * E2E tests for the web SPA (src/web/).
 *
 * Tests chat input, message rendering, and SSE streaming.
 * Uses Playwright with mocked backend routes.
 */
import { test, expect, type Page } from '@playwright/test';

const WEB_URL = 'http://localhost:5175';

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

// ── Mock routes for the web SPA ──

async function mockPuxRoutes(page: Page, opts: { sseBody?: string } = {}) {
	// Projects
	await page.route('**/api/projects', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ projects: [] }),
		});
	});

	// Pux models
	await page.route('**/api/pux/models**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ models: [] }),
		});
	});

	// Pux conversations
	await page.route('**/api/pux/conversations**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([]),
		});
	});

	// Pux conversation (single)
	await page.route('**/api/pux/conversation**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ success: true }),
		});
	});

	// Pux history
	await page.route('**/api/pux/history**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ messages: [] }),
		});
	});

	// Pux model
	await page.route('**/api/pux/model**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ success: true }),
		});
	});

	// Pux defaults
	await page.route('**/api/pux/defaults', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ logic: '', worker: '' }),
		});
	});

	// Pux providers
	await page.route('**/api/pux/providers**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ providers: [] }),
		});
	});

	// Pux agent status
	await page.route('**/api/pux/agent-status**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ agents: {} }),
		});
	});

	// Pux MCP servers
	await page.route('**/api/pux/mcp-servers**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ servers: [] }),
		});
	});

	// Pux decision
	await page.route('**/api/pux/decision**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ success: true }),
		});
	});

	// SSE prompt endpoint (both new and legacy paths)
	const sseHandler = async (route: import('@playwright/test').Route) => {
		await route.fulfill({
			status: 200,
			contentType: 'text/event-stream',
			headers: { 'Cache-Control': 'no-cache' },
			body: opts.sseBody ?? SSE_SIMPLE,
		});
	};
	await page.route('**/api/pux/prompt', sseHandler);
	await page.route('**/api/pi/prompt', sseHandler);

	// Scheduler
	await page.route('**/api/scheduler**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({ jobs: [] }),
		});
	});

	// Sandbox
	await page.route('**/api/sandbox/**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([]),
		});
	});

	// Workers
	await page.route('**/api/workers/**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify([]),
		});
	});

	// Legacy pi endpoints (catch-all for any remaining pi routes)
	await page.route('**/api/pi/**', async route => {
		await route.fulfill({
			status: 200,
			contentType: 'application/json',
			body: JSON.stringify({}),
		});
	});
}

// ── Test suite ──

test.describe('Web SPA', () => {
	test.beforeEach(async ({ page }) => {
		await mockPuxRoutes(page);
		await page.goto(WEB_URL, { waitUntil: 'networkidle', timeout: 10000 });
		await page.waitForTimeout(1000);
	});

	test('renders the chat input and welcome state', async ({ page }) => {
		const textarea = page.getByLabel('Message input');
		await expect(textarea).toBeVisible();
		await expect(textarea).toHaveAttribute('placeholder', 'Send a message...');
		// Welcome text
		await expect(page.getByText('Your AI-powered development orchestrator')).toBeVisible();
	});

	test('sends a message and receives SSE response', async ({ page }) => {
		const textarea = page.getByLabel('Message input');
		await textarea.fill('Hello Pux');
		await textarea.press('Enter');

		// User message appears (in the chat thread with data-role="user")
		await expect(page.locator('[data-role="user"]')).toBeVisible({ timeout: 3000 });
		// Assistant response appears after SSE
		await expect(page.getByText(/I can help/)).toBeVisible({ timeout: 3000 });
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
		await page.route('**/api/pi/prompt', async route => {
			promptCallCount++;
			await route.fulfill({
				status: 200,
				contentType: 'text/event-stream',
				body: SSE_SIMPLE,
			});
		});

		const textarea = page.getByLabel('Message input');
		await textarea.fill('test double send');
		await textarea.press('Enter');

		await page.waitForTimeout(2000);
		expect(promptCallCount).toBeLessThanOrEqual(1);
	});

	test('textarea auto-grows with multi-line input', async ({ page }) => {
		const textarea = page.getByLabel('Message input');
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
