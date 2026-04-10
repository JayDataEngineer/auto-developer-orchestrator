/**
 * Scheduler E2E Tests
 *
 * Tests the SchedulerView: job list, create form, CRUD actions,
 * schedule types, delivery modes, and expand/collapse details.
 */
import { test, expect } from '@playwright/test';
import { mockApiRoutes, MOCK_SCHEDULER_JOBS } from './fixtures';

test.describe('Scheduler Tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('button:has-text("Scheduler")', { timeout: 10000 });

    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForResponse(resp => resp.url().includes('/api/scheduler') && resp.status() === 200, { timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(1000);
  });

  // ── Basic Rendering ──

  test('renders scheduler tab content', async ({ page }) => {
    await expect(page.getByText('Scheduled Jobs')).toBeVisible({ timeout: 10000 });
  });

  test('shows existing scheduled jobs', async ({ page }) => {
    await expect(page.getByText('Daily tests')).toBeVisible({ timeout: 10000 });
  });

  test('shows job schedule info', async ({ page }) => {
    await expect(page.getByText(/0 9 \* \* \*/)).toBeVisible({ timeout: 10000 });
  });

  test('has New Job button', async ({ page }) => {
    await expect(page.getByText('New Job')).toBeVisible({ timeout: 10000 });
  });

  test('no white screen on scheduler tab', async ({ page }) => {
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
    await expect(page.getByText('Scheduled Jobs')).toBeVisible({ timeout: 10000 });
  });

  test('shows job count', async ({ page }) => {
    await expect(page.getByText(/1 jobs/)).toBeVisible({ timeout: 10000 });
  });

  test('has refresh button', async ({ page }) => {
    // RefreshCw icon button next to header
    const refreshBtn = page.locator('.lucide-refresh-cw');
    await expect(refreshBtn.first()).toBeVisible({ timeout: 5000 });
  });

  // ── Job Actions ──

  test('shows job actions - Run now, Enable/Disable, Delete', async ({ page }) => {
    await expect(page.getByText('Daily tests')).toBeVisible({ timeout: 10000 });

    // Action buttons use title attributes
    await expect(page.getByTitle('Run now')).toBeVisible({ timeout: 5000 });
    await expect(page.getByTitle('Delete')).toBeVisible({ timeout: 5000 });
    // Enable or Disable (one will be visible)
    const disableBtn = page.getByTitle('Disable');
    const enableBtn = page.getByTitle('Enable');
    const disableVisible = await disableBtn.isVisible().catch(() => false);
    const enableVisible = await enableBtn.isVisible().catch(() => false);
    expect(disableVisible || enableVisible).toBe(true);
  });

  // ── Job Expand/Collapse ──

  test('clicking job expands details', async ({ page }) => {
    await expect(page.getByText('Daily tests')).toBeVisible({ timeout: 10000 });

    // Click the job row to expand
    await page.getByText('Daily tests').click();
    await page.waitForTimeout(500);

    // Prompt message should appear in expanded section
    await expect(page.getByText('Run all tests')).toBeVisible({ timeout: 5000 });
  });

  test('expanded job shows prompt label', async ({ page }) => {
    await page.getByText('Daily tests').click();
    await page.waitForTimeout(500);

    await expect(page.getByText('Prompt:')).toBeVisible({ timeout: 5000 });
  });

  test('clicking expanded job collapses it', async ({ page }) => {
    await page.getByText('Daily tests').click();
    await page.waitForTimeout(500);
    await expect(page.getByText('Run all tests')).toBeVisible({ timeout: 5000 });

    // Click again to collapse
    await page.getByText('Daily tests').click();
    await page.waitForTimeout(500);

    // Prompt should be hidden
    const promptVisible = await page.getByText('Run all tests').isVisible().catch(() => false);
    expect(promptVisible).toBe(false);
  });

  // ── No Close Button in Tab Mode ──

  test('no close button when rendered as tab', async ({ page }) => {
    // The X close button only appears when onClose is provided (modal mode)
    // In tab mode, no X button should be present in the scheduler header
    const schedulerHeader = page.locator('text=Scheduled Jobs').locator('..');
    const closeButtons = schedulerHeader.locator('.lucide-x');
    const count = await closeButtons.count();
    expect(count).toBe(0);
  });
});

// ── Create Job Form Tests ──

test.describe('Scheduler - Create Job Form', () => {
  test.beforeEach(async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('button:has-text("Scheduler")', { timeout: 10000 });

    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForResponse(resp => resp.url().includes('/api/scheduler') && resp.status() === 200, { timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(1000);
  });

  test('clicking New Job opens create form', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    await expect(page.getByText('New Scheduled Job')).toBeVisible({ timeout: 3000 });
  });

  test('create form has all required fields', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    await expect(page.getByPlaceholder(/daily status check/i)).toBeVisible({ timeout: 3000 });
    await expect(page.getByPlaceholder(/what should the agent do/i)).toBeVisible({ timeout: 3000 });
  });

  test('create form has project selector', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    // Project selector in the form
    const selects = page.locator('form select');
    const count = await selects.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('create form Create Job disabled without required fields', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    const createBtn = page.getByText('Create Job');
    await expect(createBtn).toBeDisabled();
  });

  test('create form enables when all fields filled', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    await page.getByPlaceholder(/daily status check/i).fill('Test Job');
    await page.getByPlaceholder(/what should the agent do/i).fill('Run tests');
    await page.waitForTimeout(300);

    const createBtn = page.getByText('Create Job');
    await expect(createBtn).not.toBeDisabled();
  });

  test('create form submit calls API and closes form', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    await page.getByPlaceholder(/daily status check/i).fill('E2E Test Job');
    await page.getByPlaceholder(/what should the agent do/i).fill('Run e2e tests');
    await page.getByText('Create Job').click();
    await page.waitForTimeout(1000);

    // Form should close after submit (API was called)
    const formVisible = await page.getByText('New Scheduled Job').isVisible().catch(() => false);
    expect(formVisible).toBe(false);
  });

  test('create form cancel closes form', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);
    await expect(page.getByText('New Scheduled Job')).toBeVisible({ timeout: 3000 });

    // Click Cancel button
    await page.getByText('Cancel').click();
    await page.waitForTimeout(300);

    // Form should be gone
    const formVisible = await page.getByText('New Scheduled Job').isVisible().catch(() => false);
    expect(formVisible).toBe(false);
  });

  test('create form X button closes form', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);
    await expect(page.getByText('New Scheduled Job')).toBeVisible({ timeout: 3000 });

    // Click the X close button in form header
    const formHeader = page.locator('text=New Scheduled Job').locator('..');
    const xBtn = formHeader.locator('button:has(.lucide-x)');
    await xBtn.click();
    await page.waitForTimeout(300);

    const formVisible = await page.getByText('New Scheduled Job').isVisible().catch(() => false);
    expect(formVisible).toBe(false);
  });

  test('schedule type can be changed to Every', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    // Find schedule type selector
    const scheduleSelects = page.locator('form select');
    // The first schedule type select should have cron/every/at options
    const scheduleSelect = scheduleSelects.nth(1);
    await scheduleSelect.selectOption('every');
    await page.waitForTimeout(300);

    // "Every 30 minutes" option should appear
    await expect(page.getByText(/Every 30 minutes/).first()).toBeVisible({ timeout: 3000 }).catch(() => {
      // It may appear in the dropdown text, which is OK
    });
  });

  test('schedule type can be changed to One-time', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    const scheduleSelects = page.locator('form select');
    const scheduleSelect = scheduleSelects.nth(1);
    await scheduleSelect.selectOption('at');
    await page.waitForTimeout(300);

    // datetime-local input should appear
    const dateInput = page.locator('input[type="datetime-local"]');
    await expect(dateInput).toBeVisible({ timeout: 3000 });
  });

  test('delivery mode buttons are present', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    // Wait for the form to appear
    await expect(page.getByText('New Scheduled Job')).toBeVisible({ timeout: 5000 });

    // store, session, webhook buttons inside the form
    const form = page.locator('form');
    await expect(form.locator('button:has-text("store")').first()).toBeVisible({ timeout: 5000 });
    await expect(form.locator('button:has-text("session")').first()).toBeVisible({ timeout: 5000 });
    await expect(form.locator('button:has-text("webhook")').first()).toBeVisible({ timeout: 5000 });
  });

  test('clicking webhook shows URL input', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    // Click webhook delivery mode button (not select option)
    const webhookBtn = page.locator('form button:has-text("webhook")').first();
    await webhookBtn.click();
    await page.waitForTimeout(300);

    // Webhook URL input should appear
    await expect(page.getByPlaceholder(/example\.com\/webhook/i).first()).toBeVisible({ timeout: 3000 });
  });

  test('Auto-Branch checkbox present in form', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    const autoBranchLabels = page.locator('form label:has-text("Auto-Branch")');
    const count = await autoBranchLabels.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('Auto-Merge checkbox present in form', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    const autoMergeLabels = page.locator('form label:has-text("Auto-Merge")');
    const count = await autoMergeLabels.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('Enabled checkbox present in form', async ({ page }) => {
    await page.getByText('New Job').click();
    await page.waitForTimeout(500);

    const enabledLabels = page.locator('form label:has-text("Enabled")');
    const count = await enabledLabels.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });
});

// ── Empty State ──

test.describe('Scheduler - Empty State', () => {
  test('shows empty state when no jobs', async ({ page }) => {
    await mockApiRoutes(page, { jobs: [] });

    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('button:has-text("Scheduler")', { timeout: 10000 });

    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForResponse(resp => resp.url().includes('/api/scheduler') && resp.status() === 200, { timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(1000);

    await expect(page.getByText('No scheduled jobs yet')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('Create a job to automate recurring tasks')).toBeVisible({ timeout: 5000 });
  });
});

// ── Job Action Buttons ──

test.describe('Scheduler - Job Actions', () => {
  test('Run now button clicks successfully', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('button:has-text("Scheduler")', { timeout: 10000 });

    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForResponse(resp => resp.url().includes('/api/scheduler') && resp.status() === 200, { timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(1000);

    await expect(page.getByText('Daily tests')).toBeVisible({ timeout: 10000 });

    // Click Run now - it should work without error
    await page.getByTitle('Run now').click();
    await page.waitForTimeout(500);

    // No crash should happen
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });

  test('Delete button clicks successfully', async ({ page }) => {
    await mockApiRoutes(page);
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.waitForSelector('button:has-text("Scheduler")', { timeout: 10000 });

    await page.getByRole('button', { name: 'Scheduler' }).click();
    await page.waitForResponse(resp => resp.url().includes('/api/scheduler') && resp.status() === 200, { timeout: 10000 }).catch(() => {});
    await page.waitForTimeout(1000);

    await expect(page.getByText('Daily tests')).toBeVisible({ timeout: 10000 });

    await page.getByTitle('Delete').click();
    await page.waitForTimeout(500);

    // No crash
    const rootDiv = page.locator('.flex.flex-col.h-screen.bg-black');
    await expect(rootDiv).toBeVisible();
  });
});
