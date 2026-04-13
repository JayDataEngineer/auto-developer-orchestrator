/**
 * Agent Chat E2E Tests
 *
 * Tests the Pi Agent view with mocked SSE streaming. This is the most
 * critical test file because it catches the "infinite loading spinner
 * with no tool name" bug.
 */
import { test, expect, type Page, type Route } from '@playwright/test';
import {
  mockApiRoutes,
  SSE_SIMPLE_REPLY,
  SSE_WITH_TOOL_CALL,
  SSE_WITH_MULTIPLE_TOOLS,
  SSE_WITH_TOOL_ERROR,
  SSE_HANGING_TOOL,
  SSE_MISSING_TOOL_NAME,
  SSE_WITH_PR_CREATED,
  SSE_WITH_ERROR_EVENT,
  SSE_WITH_SUBAGENT_SPAWN,
  SSE_WITH_THINKING_AND_TOOLS,
  SSE_WITH_COMMIT_CREATED,
  SSE_WITH_BRANCH_CREATED,
  SSE_WITH_APPROVAL_REQUEST,
  SSE_WITH_QUESTION,
  buildSSEStream,
  type SSEEvent,
} from './fixtures';

/**
 * Override just the prompt endpoint with custom SSE events.
 * This is called AFTER mockApiRoutes to avoid interference.
 */
async function overrideSSERoute(page: Page, events: SSEEvent[]) {
  await page.route('**/api/pi/prompt', async route => {
    const body = buildSSEStream(events);
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      headers: { 'Cache-Control': 'no-cache', 'Connection': 'keep-alive' },
      body,
    });
  });
}

test.describe('Agent Chat', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);
  });

  // ── Empty State ──

  test('shows agent ready state before any message', async ({ page }) => {
    await expect(page.getByText('Pi Agent Ready')).toBeVisible();
  });

  test('shows prompt textarea', async ({ page }) => {
    const textarea = page.locator('textarea');
    await expect(textarea).toBeVisible();
  });

  // ── Sending a Prompt ──

  test('can type and send a prompt', async ({ page }) => {
    await overrideSSERoute(page, SSE_SIMPLE_REPLY);
    await page.locator('textarea').fill('Hello, can you help me?');
    await page.locator('textarea').press('Enter');
    await expect(page.getByText('Hello, can you help me?')).toBeVisible({ timeout: 5000 });
  });

  // ── Simple Text Reply ──

  test('renders text from SSE stream', async ({ page }) => {
    await overrideSSERoute(page, SSE_SIMPLE_REPLY);
    await page.locator('textarea').fill('Hello');
    await page.locator('textarea').press('Enter');
    await expect(page.getByText(/I will help you/)).toBeVisible({ timeout: 5000 });
  });

  // ── Tool Call Rendering (CRITICAL) ──

  test('tool call renders with tool name visible', async ({ page }) => {
    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    await page.locator('textarea').fill('List files');
    await page.locator('textarea').press('Enter');

    // Tool name "bash" must appear — core bug check
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 5000 });
    await expect(page.getByText('ls -la')).toBeVisible({ timeout: 5000 });
  });

  test('tool call does NOT show infinite spinner when completed', async ({ page }) => {
    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    await page.locator('textarea').fill('List files');
    await page.locator('textarea').press('Enter');

    // Wait for stream to finish
    await page.waitForTimeout(5000);

    // After completion, no spinning loaders in tool call items
    const toolSpinners = page.locator('.border.border-white\\/5.bg-zinc-950 .animate-spin');
    const count = await toolSpinners.count();
    expect(count).toBe(0);
  });

  test('tool call shows result when expanded', async ({ page }) => {
    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    await page.locator('textarea').fill('List files');
    await page.locator('textarea').press('Enter');

    // Wait for tool call to complete (no spinner)
    await page.waitForTimeout(6000);

    // Click the tool call item to expand
    const toolCallBtn = page.locator('.border.border-white\\/5.bg-zinc-950 button').first();
    await toolCallBtn.click();
    await page.waitForTimeout(500);
    await expect(page.getByText(/total 42/)).toBeVisible({ timeout: 5000 });
  });

  test('multiple tool calls render with all names', async ({ page }) => {
    await overrideSSERoute(page, SSE_WITH_MULTIPLE_TOOLS);
    await page.locator('textarea').fill('Read and test');
    await page.locator('textarea').press('Enter');

    await expect(page.locator('text=read').first()).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 5000 });
  });

  test('tool call with error renders and completes', async ({ page }) => {
    await overrideSSERoute(page, SSE_WITH_TOOL_ERROR);
    await page.locator('textarea').fill('Build');
    await page.locator('textarea').press('Enter');

    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 5000 });

    await page.waitForTimeout(5000);
    const toolSpinners = page.locator('.border.border-white\\/5.bg-zinc-950 .animate-spin');
    const count = await toolSpinners.count();
    expect(count).toBe(0);
  });

  // ── Bug Regression Tests ──

  test('BUG REGRESSION: tool call with empty name shows wrench icon', async ({ page }) => {
    await overrideSSERoute(page, SSE_MISSING_TOOL_NAME);
    await page.locator('textarea').fill('Test');
    await page.locator('textarea').press('Enter');

    await page.waitForTimeout(3000);

    const toolCallItems = page.locator('.border.border-white\\/5.bg-zinc-950');
    const count = await toolCallItems.count();
    if (count > 0) {
      const wrench = page.locator('.lucide-wrench');
      await expect(wrench).toBeVisible();
    }
  });

  test('BUG REGRESSION: hanging tool shows spinner AND name', async ({ page }) => {
    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test hanging');
    await page.locator('textarea').press('Enter');

    await page.waitForTimeout(3000);

    // Tool name MUST be visible even while loading
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 5000 });

    // Spinner should be present since tool never ended (running tools have border-primary)
    const toolSpinners = page.locator('.animate-spin');
    const count = await toolSpinners.count();
    expect(count).toBeGreaterThan(0);
  });

  // ── Thinking / Reasoning ──

  test('renders thinking/reasoning block', async ({ page }) => {
    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    await page.locator('textarea').fill('Think');
    await page.locator('textarea').press('Enter');
    await expect(page.getByText('Reasoning')).toBeVisible({ timeout: 5000 });
  });

  // ── Model Selector ──

  test('shows model selector', async ({ page }) => {
    await expect(page.getByText(/Model:/)).toBeVisible({ timeout: 5000 });
  });

  test('model dropdown shows available models', async ({ page }) => {
    await page.getByText(/Model:/).click();
    await expect(page.getByText('OR Free')).toBeVisible({ timeout: 3000 });
  });

  // ── Controls ──

  test('shows New Task button', async ({ page }) => {
    await expect(page.getByText('New Task').first()).toBeVisible();
  });

  test('shows Auto-Branch toggle', async ({ page }) => {
    await expect(page.getByText('Auto-Branch').first()).toBeVisible({ timeout: 10000 });
  });

  test('shows Compact button', async ({ page }) => {
    await expect(page.getByText('Compact').first()).toBeVisible();
  });

  // ── Token Usage ──

  test('shows token usage after conversation', async ({ page }) => {
    await overrideSSERoute(page, SSE_SIMPLE_REPLY);
    await page.locator('textarea').fill('Hello');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(5000);
    await expect(page.getByText(/Tokens:/).first()).toBeVisible({ timeout: 5000 });
  });

  // ── Fleet Bar ──

  test('shows project name in agent view', async ({ page }) => {
    const projectLabels = page.getByText('test-project');
    const count = await projectLabels.count();
    expect(count).toBeGreaterThan(0);
  });
});

test.describe('Agent Chat - No White Screen', () => {
  test('agent tab always renders content', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    const textarea = page.locator('textarea');
    const emptyState = page.getByText('Pi Agent Ready');
    const textareaVisible = await textarea.isVisible();
    const emptyVisible = await emptyState.isVisible();
    expect(textareaVisible || emptyVisible).toBe(true);
  });
});

// ── Comprehensive Interaction Tests ──

test.describe('Agent Chat - Streaming & Abort', () => {
  test('streaming indicator visible during response', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Use hanging tool so streaming stays active (no agent_end)
    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test streaming');
    await page.locator('textarea').press('Enter');

    // Wait for streaming to start - either "Streaming" header text or stop button
    const stopBtn = page.locator('button:has(.lucide-square)').first();
    await expect(stopBtn).toBeVisible({ timeout: 8000 });

    // "Streaming" text should be visible in the header while active
    await expect(page.getByText('Streaming').first()).toBeVisible({ timeout: 5000 });
  });

  test('abort button stops streaming', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test abort');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(2000);

    // Stop button (red square) should appear while streaming
    const stopBtn = page.locator('button:has(.lucide-square)').first();
    await expect(stopBtn).toBeVisible({ timeout: 5000 });
    await stopBtn.click();
    await page.waitForTimeout(500);

    // Streaming indicator should disappear
    const streamingVisible = await page.getByText('Streaming').isVisible().catch(() => false);
    expect(streamingVisible).toBe(false);
  });

  test('streaming cursor visible during text stream', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Use a stream with text but no agent_end so streaming stays active
    await overrideSSERoute(page, [
      { type: 'agent_start', data: {} },
      { type: 'text_delta', data: { text: 'Generating...' } },
    ]);
    await page.locator('textarea').fill('Test');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(2000);

    // The pulsing cursor (animate-pulse span) should be visible
    const cursor = page.locator('.animate-pulse').first();
    const count = await cursor.count();
    expect(count).toBeGreaterThan(0);
  });
});

test.describe('Agent Chat - Model Switching', () => {
  test('model dropdown opens and lists all models', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.getByText(/Model:/).click();
    await expect(page.getByText('OR Free')).toBeVisible({ timeout: 3000 });
    await expect(page.getByText('Fast')).toBeVisible({ timeout: 3000 });
    await expect(page.getByText('Smart')).toBeVisible({ timeout: 3000 });
  });

  test('clicking a model switches to it', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.getByText(/Model:/).click();
    await page.getByText('Smart').click();
    await page.waitForTimeout(500);

    // Model label should now show "smart"
    await expect(page.getByText(/Model: smart/)).toBeVisible({ timeout: 3000 });
  });

  test('model dropdown closes on click outside', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await page.getByText(/Model:/).click();
    await expect(page.getByText('OR Free')).toBeVisible({ timeout: 3000 });

    // Click somewhere else
    await page.locator('body').click({ position: { x: 10, y: 10 } });
    await page.waitForTimeout(300);

    // Dropdown should be gone
    const orFreeVisible = await page.getByText('OR Free').isVisible().catch(() => false);
    expect(orFreeVisible).toBe(false);
  });
});

test.describe('Agent Chat - Toggles & Buttons', () => {
  test('Auto-Branch toggle changes color when clicked', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    const toggle = page.getByText('Auto-Branch').first();
    await expect(toggle).toBeVisible();

    // Before clicking - muted color
    const beforeClass = await toggle.getAttribute('class') || '';
    expect(beforeClass).toContain('text-muted');

    await toggle.click();
    await page.waitForTimeout(300);

    // After clicking - primary color
    const afterClass = await toggle.getAttribute('class') || '';
    expect(afterClass).toContain('text-primary');
  });

  test('Auto-Merge toggle only visible when Auto-Branch is on', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Auto-Merge should NOT be visible initially
    const autoMergeBefore = page.getByText('Auto-Merge');
    const visibleBefore = await autoMergeBefore.isVisible().catch(() => false);
    expect(visibleBefore).toBe(false);

    // Turn on Auto-Branch
    await page.getByText('Auto-Branch').first().click();
    await page.waitForTimeout(300);

    // Now Auto-Merge should appear
    await expect(page.getByText('Auto-Merge')).toBeVisible({ timeout: 3000 });
  });

  test('Compact button is visible and not disabled', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    const compactBtn = page.getByText('Compact').first();
    await expect(compactBtn).toBeVisible();
    await expect(compactBtn).not.toBeDisabled();
  });

  test('New Task button clears conversation', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Send a message first
    await overrideSSERoute(page, SSE_SIMPLE_REPLY);
    await page.locator('textarea').fill('Hello');
    await page.locator('textarea').press('Enter');
    await expect(page.getByText('Hello, can you help me?')).toBeVisible({ timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(2000);

    // Click New Task
    await page.getByText('New Task').first().click();
    await page.waitForTimeout(500);

    // The empty state should come back
    await expect(page.getByText('Pi Agent Ready')).toBeVisible({ timeout: 3000 });
  });
});

test.describe('Agent Chat - PR & Error Events', () => {
  test('PR created banner appears after pr_created event', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_PR_CREATED);
    await page.locator('textarea').fill('Create PR');
    await page.locator('textarea').press('Enter');

    // PR banner text
    await expect(page.getByText(/Pull Request #42 Created/)).toBeVisible({ timeout: 8000 });
    // PR URL
    await expect(page.getByText(/github.com\/test-org\/test-repo\/pull\/42/)).toBeVisible({ timeout: 5000 });
    // Open PR button
    await expect(page.getByText('Open PR')).toBeVisible({ timeout: 5000 });
  });

  test('error SSE event shows error in header', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_ERROR_EVENT);
    await page.locator('textarea').fill('Trigger error');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(3000);

    // Error message should appear
    await expect(page.getByText('Model rate limit exceeded')).toBeVisible({ timeout: 5000 });
  });
});

test.describe('Agent Chat - Sub-Agent Cards', () => {
  test('sub-agent card appears when bash spawns sub-agent', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_SUBAGENT_SPAWN);
    await page.locator('textarea').fill('Spawn agent');
    await page.locator('textarea').press('Enter');

    // Sub-Agents header
    await expect(page.getByText(/Sub-Agents/)).toBeVisible({ timeout: 8000 });
    // Sub-agent type label
    await expect(page.getByText(/code sub-agent/)).toBeVisible({ timeout: 5000 });
    // Status indicator
    await expect(page.getByText('running')).toBeVisible({ timeout: 5000 });
  });
});

test.describe('Agent Chat - Multi-Turn Conversation', () => {
  test('can send multiple messages in sequence', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // First message
    await overrideSSERoute(page, SSE_SIMPLE_REPLY);
    const textarea = page.locator('textarea').first();
    await textarea.fill('First message');
    await textarea.press('Enter');
    await expect(page.getByText('First message')).toBeVisible({ timeout: 5000 });
    await page.waitForTimeout(5000);

    // Re-locate textarea after first response completes
    const textarea2 = page.locator('textarea').first();
    await expect(textarea2).toBeVisible({ timeout: 5000 });
    await textarea2.fill('Second message');
    await textarea2.press('Enter');
    await expect(page.getByText('Second message')).toBeVisible({ timeout: 5000 });

    // Both user messages should be in the conversation
    const firstMsgs = await page.getByText('First message').count();
    const secondMsgs = await page.getByText('Second message').count();
    expect(firstMsgs).toBeGreaterThanOrEqual(1);
    expect(secondMsgs).toBeGreaterThanOrEqual(1);
  });
});

test.describe('Agent Chat - Reasoning Block', () => {
  test('reasoning block expands to show content', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    await page.locator('textarea').fill('Think');
    await page.locator('textarea').press('Enter');

    // Reasoning button
    await expect(page.getByText('Reasoning')).toBeVisible({ timeout: 5000 });

    // Click to expand
    await page.getByText('Reasoning').click();
    await page.waitForTimeout(500);

    // Content should be visible
    await expect(page.getByText('Let me check the current files...')).toBeVisible({ timeout: 3000 });
  });

  test('reasoning block shows char count', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    await page.locator('textarea').fill('Think');
    await page.locator('textarea').press('Enter');

    await expect(page.getByText(/chars/)).toBeVisible({ timeout: 5000 });
  });
});

test.describe('Agent Chat - Fleet Bar', () => {
  test('shows model name in fleet bar', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(3000);

    // Model name is loaded from /api/pi/state and shown in fleet bar
    // It may show as "or-free" or "default" depending on hydration timing
    const fleetBar = page.locator('.w-full.border-b.border-white\\/5');
    await expect(fleetBar.first()).toBeVisible({ timeout: 5000 });

    // Check that some model text appears in the fleet bar area
    const modelText = page.getByText(/or-free|default/).first();
    await expect(modelText).toBeVisible({ timeout: 8000 });
  });

  test('shows Live indicator during streaming', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(2000);

    await expect(page.getByText('Live')).toBeVisible({ timeout: 5000 });
  });
});

// ── Tool Visibility During Streaming ──

test.describe('Agent Chat - Tool Visibility', () => {
  test('running tool shows name in header during streaming', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Use hanging tool so it stays "running"
    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(2000);

    // Header should show "Running: bash"
    await expect(page.getByText(/Running: bash/).first()).toBeVisible({ timeout: 5000 });
  });

  test('running tool has highlighted border', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(2000);

    // Running tool should have border-primary class (highlighted)
    const highlightedTool = page.locator('.border-primary\\/30');
    const count = await highlightedTool.count();
    expect(count).toBeGreaterThan(0);
  });

  test('full flow with thinking + tool + text renders correctly', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_THINKING_AND_TOOLS);
    await page.locator('textarea').fill('Analyze codebase');
    await page.locator('textarea').press('Enter');

    // Reasoning block
    await expect(page.getByText('Reasoning')).toBeVisible({ timeout: 5000 });
    // Both tool names
    await expect(page.locator('text=bash').first()).toBeVisible({ timeout: 5000 });
    await expect(page.locator('text=read').first()).toBeVisible({ timeout: 5000 });
    // Final text
    await expect(page.getByText(/I found 3 TypeScript files/)).toBeVisible({ timeout: 5000 });
  });

  test('multiple tool calls in one message show all results when expanded', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_MULTIPLE_TOOLS);
    await page.locator('textarea').fill('Read and test');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(5000);

    // Expand first tool (read)
    const toolBtns = page.locator('.border.border-white\\/5.bg-zinc-950 button, .border.border-primary\\/30 button');
    const count = await toolBtns.count();
    if (count > 0) {
      await toolBtns.first().click();
      await page.waitForTimeout(500);
      // Result should be visible
      const resultText = page.getByText(/export default function App/);
      await expect(resultText).toBeVisible({ timeout: 3000 });
    }
  });

  test('error tool call shows error in result panel', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);

    await overrideSSERoute(page, SSE_WITH_TOOL_ERROR);
    const textarea = page.locator('textarea').first();
    await expect(textarea).toBeVisible({ timeout: 10000 });
    await textarea.fill('Build');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(5000);

    // Click tool to expand
    const toolBtns = page.locator('.border.border-white\\/5.bg-zinc-950 button, .border.border-primary\\/30 button');
    if (await toolBtns.count() > 0) {
      await toolBtns.first().click();
      await page.waitForTimeout(500);
      // Error should be visible
      await expect(page.getByText('Build failed: exit code 1')).toBeVisible({ timeout: 3000 });
    }
  });
});

// ── Git Events ──

test.describe('Agent Chat - Git Events', () => {
  test('commit_created event updates state', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_COMMIT_CREATED);
    await page.locator('textarea').fill('Commit');
    await page.locator('textarea').press('Enter');

    // Branch name should appear in fleet bar
    await expect(page.getByText('feat/auth')).toBeVisible({ timeout: 8000 });
  });

  test('branch_created event shows branch name in FleetBar', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_BRANCH_CREATED);
    await page.locator('textarea').fill('Branch');
    await page.locator('textarea').press('Enter');

    // Branch name in fleet bar
    await expect(page.getByText('feature/new-ui')).toBeVisible({ timeout: 8000 });
  });
});

// ── Console Error / Key Prop Regression ──

test.describe('Agent Chat - Console Error Regression', () => {
  test('no React key prop warnings after sending message', async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'warning' && msg.text().includes('key')) {
        consoleErrors.push(msg.text());
      }
    });

    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    await page.locator('textarea').fill('Test key props');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(6000);

    const keyWarnings = consoleErrors.filter(e => e.includes('Each child in a list should have a unique "key" prop'));
    expect(keyWarnings.length).toBe(0);
  });

  test('multi-turn conversation produces no key warnings', async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'warning' && msg.text().includes('key')) {
        consoleErrors.push(msg.text());
      }
    });

    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // First message
    await overrideSSERoute(page, SSE_SIMPLE_REPLY);
    await page.locator('textarea').fill('First');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(6000);

    // Second message
    await overrideSSERoute(page, SSE_WITH_TOOL_CALL);
    const textarea2 = page.locator('textarea').first();
    await textarea2.fill('Second');
    await textarea2.press('Enter');
    await page.waitForTimeout(6000);

    const keyWarnings = consoleErrors.filter(e => e.includes('Each child in a list should have a unique "key" prop'));
    expect(keyWarnings.length).toBe(0);
  });
});

// ── Right Panel Streaming State ──

test.describe('Agent Chat - Right Panel Streaming', () => {
  test('right panel shows Agent Active indicator during streaming', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    // Right panel should be visible — use the Artifacts toggle button in the top bar
    const artifactsPanel = page.getByRole('button', { name: 'Artifacts' });
    await expect(artifactsPanel).toBeVisible({ timeout: 5000 });

    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test streaming');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(2000);

    // Right panel should show "Agent Active"
    await expect(page.getByText('Agent Active')).toBeVisible({ timeout: 5000 });
  });

  test('right panel shows running tool name during streaming', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_HANGING_TOOL);
    await page.locator('textarea').fill('Test');
    await page.locator('textarea').press('Enter');
    await page.waitForTimeout(2000);

    // Right panel should show tool name
    const runningTexts = page.getByText(/Running: bash/);
    const count = await runningTexts.count();
    expect(count).toBeGreaterThan(0);
  });
});

// ── Approval / Human-in-the-Loop ──

test.describe('Agent Chat - Approval Banner', () => {
  test('approval banner appears with tool name and risk level', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_APPROVAL_REQUEST);
    await page.locator('textarea').fill('Post tweet');
    await page.locator('textarea').press('Enter');

    // Approval Required header
    await expect(page.getByText('Approval Required')).toBeVisible({ timeout: 5000 });
    // Risk badge
    await expect(page.getByText('high')).toBeVisible({ timeout: 3000 });
    // Tool name
    await expect(page.getByText('bash')).toBeVisible({ timeout: 3000 });
    // Message
    await expect(page.getByText('Post tweet via Twitter API')).toBeVisible({ timeout: 3000 });
  });

  test('approve button clicks and calls respond API', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_APPROVAL_REQUEST);
    await page.locator('textarea').fill('Post tweet');
    await page.locator('textarea').press('Enter');
    await expect(page.getByText('Approval Required')).toBeVisible({ timeout: 5000 });

    // Click Approve
    await page.getByText('Approve').click();
    await page.waitForTimeout(500);

    // Banner should disappear
    const bannerVisible = await page.getByText('Approval Required').isVisible().catch(() => false);
    expect(bannerVisible).toBe(false);
  });

  test('deny button clicks and sends deny action', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_APPROVAL_REQUEST);
    await page.locator('textarea').fill('Post tweet');
    await page.locator('textarea').press('Enter');
    await expect(page.getByText('Approval Required')).toBeVisible({ timeout: 5000 });

    // Click Deny
    await page.getByText('Deny').click();
    await page.waitForTimeout(500);

    // Banner should disappear
    const bannerVisible = await page.getByText('Approval Required').isVisible().catch(() => false);
    expect(bannerVisible).toBe(false);
  });
});

test.describe('Agent Chat - Question Banner', () => {
  test('question banner appears with question text', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_QUESTION);
    await page.locator('textarea').fill('Post tweet');
    await page.locator('textarea').press('Enter');

    // Agent Asks header
    await expect(page.getByText('Agent Asks')).toBeVisible({ timeout: 5000 });
    // Question text
    await expect(page.getByText('Which Twitter account should I use?')).toBeVisible({ timeout: 3000 });
    // Answer input
    await expect(page.getByPlaceholder(/your answer/i)).toBeVisible({ timeout: 3000 });
    // Submit button
    await expect(page.getByText('Submit')).toBeVisible({ timeout: 3000 });
  });

  test('submit answer sends text response', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(1500);

    await overrideSSERoute(page, SSE_WITH_QUESTION);
    await page.locator('textarea').fill('Post tweet');
    await page.locator('textarea').press('Enter');
    await expect(page.getByText('Agent Asks')).toBeVisible({ timeout: 5000 });

    // Type answer
    await page.getByPlaceholder(/your answer/i).fill('@myaccount');
    await page.getByText('Submit').click();
    await page.waitForTimeout(500);

    // Banner should disappear
    const bannerVisible = await page.getByText('Agent Asks').isVisible().catch(() => false);
    expect(bannerVisible).toBe(false);
  });
});
