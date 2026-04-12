import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { TaskBoardTab } from '../../src/components/TaskBoardTab';
import { ToastProvider } from '../../src/components/ui/Toast';

// Mock scheduler API
const mockJobs = [
  {
    id: 'task-1',
    name: 'Fix login bug',
    description: 'Login fails on mobile',
    project: '',
    message: 'Fix the login bug on mobile',
    status: 'idle' as const,
    scheduleType: 'manual' as const,
    enabled: false,
    consecutiveErrors: 0,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
  {
    id: 'task-2',
    name: 'Daily sync',
    project: '',
    message: 'Sync data',
    status: 'idle' as const,
    scheduleType: 'cron' as const,
    cronExpr: '0 0 9 * * *',
    enabled: true,
    consecutiveErrors: 0,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  },
];

vi.mock('../../src/lib/api', () => ({
  api: {
    scheduler: {
      list: vi.fn(async () => ({ jobs: mockJobs })),
      create: vi.fn(async () => ({ success: true, job: mockJobs[0] })),
      update: vi.fn(async () => ({ success: true, job: mockJobs[0] })),
      delete: vi.fn(async () => ({ success: true })),
      trigger: vi.fn(async () => ({ success: true, message: 'Triggered' })),
      executions: vi.fn(async () => ({ executions: [] })),
      runs: vi.fn(async () => ({ runs: [] })),
    },
  },
}));

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

describe('TaskBoardTab', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders tasks from scheduler API', async () => {
    renderWithToast(<TaskBoardTab />);

    expect(await screen.findByText('Fix login bug')).toBeInTheDocument();
    expect(screen.getByText('Daily sync')).toBeInTheDocument();
  });

  it('shows Manual Tasks and Scheduled Jobs sections', async () => {
    renderWithToast(<TaskBoardTab />);

    expect(await screen.findByText('Manual Tasks')).toBeInTheDocument();
    expect(screen.getByText('Scheduled Jobs')).toBeInTheDocument();
  });

  it('opens create form on New Task button click', async () => {
    renderWithToast(<TaskBoardTab />);

    await screen.findByText('Fix login bug');

    const newTaskBtns = screen.getAllByText('New Task');
    fireEvent.click(newTaskBtns[0]);

    expect(screen.getByPlaceholderText('Task name')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Prompt — what should the agent do?')).toBeInTheDocument();
  });

  it('opens edit modal on manual task click', async () => {
    renderWithToast(<TaskBoardTab />);

    fireEvent.click(await screen.findByText('Fix login bug'));

    expect(screen.getByText('Edit Task')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Fix login bug')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Fix the login bug on mobile')).toBeInTheDocument();
  });

  it('opens edit modal on scheduled job click', async () => {
    renderWithToast(<TaskBoardTab />);

    fireEvent.click(await screen.findByText('Daily sync'));

    expect(screen.getByText('Edit Task')).toBeInTheDocument();
    expect(screen.getByDisplayValue('Daily sync')).toBeInTheDocument();
  });
});
