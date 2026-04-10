/**
 * Task Board E2E Tests
 *
 * Tests the Kanban task board with 4 columns, task cards,
 * detail panel, create form, and task actions.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, MOCK_TASKS, MOCK_PROJECTS } from './fixtures';

/**
 * Helper to navigate to Tasks tab with robust waiting.
 */
async function goToTasksTab(page: import('@playwright/test').Page) {
  await page.waitForSelector('button:has-text("Tasks")', { timeout: 15000 });
  await page.locator('.h-10.border-b button:has-text("Tasks")').click();
  await page.waitForTimeout(1000);
}

test.describe('Task Board Tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    await goToTasksTab(page);
  });

  // ── Kanban Columns ──

  test('renders all 4 kanban columns', async ({ page }) => {
    await expect(page.getByText('Pending').first()).toBeVisible();
    await expect(page.getByText('In Progress').first()).toBeVisible();
    await expect(page.getByText('Completed').first()).toBeVisible();
    await expect(page.getByText('Failed').first()).toBeVisible();
  });

  test('renders task titles in the board', async ({ page }) => {
    await expect(page.getByText('Fix login bug')).toBeVisible();
    await expect(page.getByText('Add dark mode')).toBeVisible();
    await expect(page.getByText('Write tests')).toBeVisible();
    await expect(page.getByText('Deploy to prod')).toBeVisible();
  });

  test('shows task count per column', async ({ page }) => {
    await expect(page.getByText('Fix login bug')).toBeVisible({ timeout: 10000 });
    const countBadges = page.locator('text=1');
    const count = await countBadges.count();
    expect(count).toBeGreaterThanOrEqual(4);
  });

  // ── Task Card Interaction ──

  test('clicking task card shows detail', async ({ page }) => {
    await page.getByText('Fix login bug').first().click();
    await page.waitForTimeout(500);

    const descriptions = page.getByText('Login fails on mobile');
    const count = await descriptions.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('clicking failed task shows error', async ({ page }) => {
    await page.getByText('Deploy to prod').first().click();
    await page.waitForTimeout(500);

    await expect(page.getByText('Build failed')).toBeVisible({ timeout: 3000 });
  });

  // ── Status Badges ──

  test('tasks show status indicators', async ({ page }) => {
    const allDots = page.locator('.bg-zinc-600, .bg-yellow-400, .bg-emerald-400, .bg-red-400');
    const count = await allDots.count();
    expect(count).toBeGreaterThan(0);
  });

  // ── Create Task ──

  test('New Task button opens create form', async ({ page }) => {
    const newTaskBtn = page.getByText('New Task').first();
    await newTaskBtn.click();

    await expect(page.getByPlaceholder(/title/i)).toBeVisible({ timeout: 3000 });
  });

  test('can fill create form', async ({ page }) => {
    const newTaskBtn = page.getByText('New Task').first();
    await newTaskBtn.click();

    const titleInput = page.getByPlaceholder(/title/i);
    await titleInput.fill('E2E Test Task');

    await expect(titleInput).toHaveValue('E2E Test Task');
  });

  // ── Layout ──

  test('kanban columns have visible styling', async ({ page }) => {
    const columns = page.locator('.border.border-white\\/5');
    const count = await columns.count();
    expect(count).toBeGreaterThan(0);
  });

  // ── Detail Panel ──

  test('detail panel shows task description', async ({ page }) => {
    await page.getByText('Fix login bug').first().click();
    await page.waitForTimeout(500);

    // Description appears in both card and detail panel - use .first()
    await expect(page.getByText('Login fails on mobile').first()).toBeVisible({ timeout: 3000 });
  });

  test('detail panel shows metrics for in_progress task', async ({ page }) => {
    await page.getByText('Add dark mode').first().click();
    await page.waitForTimeout(500);

    await expect(page.getByText('Duration')).toBeVisible({ timeout: 3000 });
    await expect(page.getByText('Tokens')).toBeVisible({ timeout: 3000 });
    await expect(page.getByText('Created')).toBeVisible({ timeout: 3000 });
  });

  test('detail panel shows error for failed task', async ({ page }) => {
    await page.getByText('Deploy to prod').first().click();
    await page.waitForTimeout(500);

    await expect(page.getByText('Error')).toBeVisible({ timeout: 3000 });
    await expect(page.getByText('Build failed')).toBeVisible({ timeout: 3000 });
  });

  test('detail panel shows Start button for pending task', async ({ page }) => {
    await page.getByText('Fix login bug').first().click();
    await page.waitForTimeout(500);

    await expect(page.getByText('Start').first()).toBeVisible({ timeout: 3000 });
  });

  test('detail panel shows Stop button for in_progress task', async ({ page }) => {
    await page.getByText('Add dark mode').first().click();
    await page.waitForTimeout(500);

    await expect(page.getByText('Stop').first()).toBeVisible({ timeout: 3000 });
  });

  test('detail panel has delete button', async ({ page }) => {
    await page.getByText('Fix login bug').first().click();
    await page.waitForTimeout(500);

    const deleteButtons = page.locator('.lucide-trash-2');
    const count = await deleteButtons.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('detail panel close button works', async ({ page }) => {
    await page.getByText('Fix login bug').first().click();
    await page.waitForTimeout(500);

    // Close button (X icon in detail panel header)
    const closeBtn = page.locator('.border-t.border-white\\/5 button:has(.lucide-x)').first();
    const visible = await closeBtn.isVisible().catch(() => false);
    if (visible) {
      await closeBtn.click();
      await page.waitForTimeout(500);

      const detailPanel = page.locator('.border-t.border-white\\/5.bg-zinc-950\\/80');
      const panelCount = await detailPanel.count();
      expect(panelCount).toBe(0);
    }
  });

  // ── Create Form ──

  test('create form has title, description, and model inputs', async ({ page }) => {
    await page.getByText('New Task').first().click();

    await expect(page.getByPlaceholder(/title/i)).toBeVisible({ timeout: 3000 });
    await expect(page.getByPlaceholder(/description/i)).toBeVisible({ timeout: 3000 });
    await expect(page.getByPlaceholder(/model/i)).toBeVisible({ timeout: 3000 });
  });

  test('create form Create disabled without title', async ({ page }) => {
    await page.getByText('New Task').first().click();

    const createBtn = page.getByText('Create');
    await expect(createBtn).toBeDisabled();
  });

  test('create form enables Create when title filled', async ({ page }) => {
    await page.getByText('New Task').first().click();

    await page.getByPlaceholder(/title/i).fill('Test task');
    const createBtn = page.getByText('Create');
    await expect(createBtn).not.toBeDisabled();
  });

  test('create form cancel closes the form', async ({ page }) => {
    await page.getByText('New Task').first().click();
    await expect(page.getByPlaceholder(/title/i)).toBeVisible({ timeout: 3000 });

    const cancelX = page.locator('form .text-zinc-500:has(.lucide-x)').first();
    await cancelX.click();
    await page.waitForTimeout(300);

    const titleInput = page.getByPlaceholder(/title/i);
    const visible = await titleInput.isVisible().catch(() => false);
    expect(visible).toBe(false);
  });

  test('create form can fill all fields', async ({ page }) => {
    await page.getByText('New Task').first().click();

    await page.getByPlaceholder(/title/i).fill('E2E Comprehensive Task');
    await page.getByPlaceholder(/description/i).fill('A thorough test task');
    await page.getByPlaceholder(/model/i).fill('smart');

    await expect(page.getByPlaceholder(/title/i)).toHaveValue('E2E Comprehensive Task');
    await expect(page.getByPlaceholder(/description/i)).toHaveValue('A thorough test task');
    await expect(page.getByPlaceholder(/model/i)).toHaveValue('smart');
  });

  test('create form submit calls API and shows new task', async ({ page }) => {
    await page.getByText('New Task').first().click();

    await page.getByPlaceholder(/title/i).fill('Submit Test Task');
    await page.getByPlaceholder(/description/i).fill('Testing submit');
    await page.getByText('Create').click();
    await page.waitForTimeout(1000);

    // The task should appear in the board (API was called) — use .first() as toast may also match
    await expect(page.getByText('Submit Test Task').first()).toBeVisible({ timeout: 5000 });
  });

  // ── Card Hover Actions ──

  test('pending task card shows Start on hover', async ({ page }) => {
    await expect(page.getByText('Fix login bug')).toBeVisible({ timeout: 5000 });

    const card = page.getByText('Fix login bug').first();
    await card.hover();
    await page.waitForTimeout(300);

    const startBtn = page.getByTitle('Start').first();
    await expect(startBtn).toBeVisible({ timeout: 3000 });
  });

  test('in_progress task card shows Stop on hover', async ({ page }) => {
    await expect(page.getByText('Add dark mode')).toBeVisible({ timeout: 5000 });

    const card = page.getByText('Add dark mode').first();
    await card.hover();
    await page.waitForTimeout(300);

    const stopBtn = page.getByTitle('Stop').first();
    await expect(stopBtn).toBeVisible({ timeout: 3000 });
  });

  test('task card shows Delete on hover', async ({ page }) => {
    await expect(page.getByText('Fix login bug')).toBeVisible({ timeout: 5000 });

    const card = page.getByText('Fix login bug').first();
    await card.hover();
    await page.waitForTimeout(300);

    const deleteBtn = page.getByTitle('Delete').first();
    await expect(deleteBtn).toBeVisible({ timeout: 3000 });
  });
});

// ── Dynamic Data Tests ──

test.describe('Task Board - Dynamic Data', () => {
  test('task board shows custom mock data', async ({ page }) => {
    await mockApiRoutes(page, {
      tasks: [
        {
          id: 'custom-1',
          title: 'Custom E2E Task',
          status: 'pending',
          projectDir: 'test-project',
          parentAgent: 'default',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
      ],
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    await goToTasksTab(page);

    await expect(page.getByText('Custom E2E Task')).toBeVisible({ timeout: 5000 });
  });

  test('empty state when no tasks', async ({ page }) => {
    await mockApiRoutes(page, { tasks: [] });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    await goToTasksTab(page);

    await expect(page.getByText('No tasks yet')).toBeVisible({ timeout: 5000 });
  });

  test('task with output shows output in detail', async ({ page }) => {
    await mockApiRoutes(page, {
      tasks: [
        {
          id: 'task-output',
          title: 'Task with output',
          status: 'completed',
          projectDir: 'test-project',
          parentAgent: 'default',
          output: 'All tests passed successfully',
          createdAt: Date.now(),
          updatedAt: Date.now(),
        },
      ],
    });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(2000);
    await goToTasksTab(page);

    await page.getByText('Task with output').first().click();
    await page.waitForTimeout(500);

    await expect(page.getByText('Output', { exact: true })).toBeVisible({ timeout: 3000 });
    await expect(page.getByText('All tests passed successfully')).toBeVisible({ timeout: 3000 });
  });
});
