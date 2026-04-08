import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TaskBoardTab } from '../../src/components/TaskBoardTab';
import { ToastProvider } from '../../src/components/ui/Toast';
import type { PiTask } from '../../src/lib/api';

// Mock the useTasks hook
const mockTasks: PiTask[] = [
  {
    id: 'task-1',
    title: 'Fix login bug',
    description: 'Login fails on mobile',
    status: 'pending',
    projectDir: 'test-project',
    parentAgent: 'default',
    createdAt: Date.now(),
    updatedAt: Date.now(),
  },
  {
    id: 'task-2',
    title: 'Add dark mode',
    status: 'in_progress',
    projectDir: 'test-project',
    parentAgent: 'default',
    createdAt: Date.now(),
    updatedAt: Date.now(),
    durationMs: 120000,
    inputTokens: 5000,
    outputTokens: 2000,
  },
  {
    id: 'task-3',
    title: 'Write tests',
    status: 'completed',
    projectDir: 'test-project',
    parentAgent: 'default',
    createdAt: Date.now(),
    updatedAt: Date.now(),
  },
  {
    id: 'task-4',
    title: 'Deploy to prod',
    status: 'failed',
    projectDir: 'test-project',
    parentAgent: 'default',
    error: 'Build failed',
    createdAt: Date.now(),
    updatedAt: Date.now(),
  },
];

vi.mock('../../src/hooks/useTasks', () => ({
  useTasks: () => ({
    tasks: mockTasks,
    loading: false,
    error: null,
    fetchTasks: vi.fn(),
    createTask: vi.fn(async (data) => ({ ...data, id: 'new-task', status: 'pending', projectDir: 'test-project', parentAgent: 'default', createdAt: Date.now(), updatedAt: Date.now() })),
    updateTask: vi.fn(),
    stopTask: vi.fn(),
    deleteTask: vi.fn(),
    setDependencies: vi.fn(),
    groupedByStatus: {
      pending: mockTasks.filter(t => t.status === 'pending'),
      in_progress: mockTasks.filter(t => t.status === 'in_progress'),
      completed: mockTasks.filter(t => t.status === 'completed'),
      failed: mockTasks.filter(t => t.status === 'failed'),
    },
  }),
}));

function renderWithToast(ui: React.ReactElement) {
  return render(<ToastProvider>{ui}</ToastProvider>);
}

describe('TaskBoardTab', () => {
  it('shows empty state when no project selected', () => {
    renderWithToast(<TaskBoardTab selectedProject={null} />);
    expect(screen.getByText('Select a project to view tasks')).toBeInTheDocument();
  });

  it('renders all 4 kanban columns', () => {
    renderWithToast(<TaskBoardTab selectedProject="test-project" />);

    expect(screen.getByText('Pending')).toBeInTheDocument();
    expect(screen.getByText('In Progress')).toBeInTheDocument();
    expect(screen.getByText('Completed')).toBeInTheDocument();
    expect(screen.getByText('Failed')).toBeInTheDocument();
  });

  it('renders task titles in the board', () => {
    renderWithToast(<TaskBoardTab selectedProject="test-project" />);

    expect(screen.getByText('Fix login bug')).toBeInTheDocument();
    expect(screen.getByText('Add dark mode')).toBeInTheDocument();
    expect(screen.getByText('Write tests')).toBeInTheDocument();
    expect(screen.getByText('Deploy to prod')).toBeInTheDocument();
  });

  it('shows task counts per column', () => {
    renderWithToast(<TaskBoardTab selectedProject="test-project" />);

    // Each column shows a count
    const counts = screen.getAllByText('1');
    expect(counts.length).toBeGreaterThanOrEqual(4); // 4 columns, each with 1 task
  });

  it('opens create form on New Task button click', () => {
    renderWithToast(<TaskBoardTab selectedProject="test-project" />);

    const newTaskBtn = screen.getByText('New Task');
    fireEvent.click(newTaskBtn);

    expect(screen.getByText('New Task')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Task title')).toBeInTheDocument();
  });

  it('selects task on card click and shows detail panel', () => {
    renderWithToast(<TaskBoardTab selectedProject="test-project" />);

    // Click on a task card
    fireEvent.click(screen.getByText('Fix login bug'));

    // Detail panel should appear with description (appears in both card and detail)
    const descriptions = screen.getAllByText('Login fails on mobile');
    expect(descriptions.length).toBeGreaterThanOrEqual(2); // card + detail panel
  });

  it('shows error in failed task detail', () => {
    renderWithToast(<TaskBoardTab selectedProject="test-project" />);

    // Click on the failed task
    fireEvent.click(screen.getByText('Deploy to prod'));

    expect(screen.getByText('Build failed')).toBeInTheDocument();
  });
});
