import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AppShell } from '../../src/components/AppShell';

// Mock useOrchestrator
vi.mock('../../src/hooks/useOrchestrator', () => ({
  useOrchestrator: () => ({
    state: {
      projects: ['test-project', 'other-project'],
      githubUser: { connected: false },
    },
    actions: {
      refreshProjectData: vi.fn(),
    },
  }),
}));

// Mock child components that make API calls
vi.mock('../../src/components/AgentTab', () => ({
  AgentTab: ({ selectedProject }: any) => (
    <div data-testid="agent-tab">Agent Tab - {selectedProject}</div>
  ),
}));

vi.mock('../../src/components/ComputerUseTab', () => ({
  ComputerUseTab: ({ selectedProject }: any) => (
    <div data-testid="desktop-tab">Desktop Tab - {selectedProject}</div>
  ),
}));

vi.mock('../../src/components/TaskBoardTab', () => ({
  TaskBoardTab: () => (
    <div data-testid="tasks-tab">Tasks Tab</div>
  ),
}));

vi.mock('../../src/components/SchedulerView', () => ({
  SchedulerView: ({ projects }: any) => (
    <div data-testid="scheduler-tab">Scheduler - {projects?.join(',')}</div>
  ),
}));

vi.mock('../../src/components/GitHubConnectModal', () => ({
  GitHubConnectModal: ({ isOpen, onClose }: any) =>
    isOpen ? <div data-testid="github-modal">GitHub Modal</div> : null,
}));

describe('AppShell', () => {
  it('renders 4 tabs with labels', () => {
    render(<AppShell />);

    expect(screen.getByText('Agent')).toBeInTheDocument();
    expect(screen.getByText('Tasks')).toBeInTheDocument();
    expect(screen.getByText('Desktop')).toBeInTheDocument();
    expect(screen.getByText('Scheduler')).toBeInTheDocument();
  });

  it('defaults to Agent tab', () => {
    render(<AppShell />);

    expect(screen.getByTestId('agent-tab')).toBeInTheDocument();
  });

  it('switches to Tasks tab on click', () => {
    render(<AppShell />);

    fireEvent.click(screen.getByText('Tasks'));
    expect(screen.getByTestId('tasks-tab')).toBeInTheDocument();
    expect(screen.queryByTestId('agent-tab')).not.toBeInTheDocument();
  });

  it('switches to Desktop tab on click', () => {
    render(<AppShell />);

    fireEvent.click(screen.getByText('Desktop'));
    expect(screen.getByTestId('desktop-tab')).toBeInTheDocument();
  });

  it('switches to Scheduler tab on click', () => {
    render(<AppShell />);

    fireEvent.click(screen.getByText('Scheduler'));
    expect(screen.getByTestId('scheduler-tab')).toBeInTheDocument();
  });

  it('renders project selector with projects', () => {
    render(<AppShell />);

    const select = screen.getByDisplayValue('test-project');
    expect(select).toBeInTheDocument();
  });

  it('renders PI branding', () => {
    render(<AppShell />);

    expect(screen.getByText('PI')).toBeInTheDocument();
  });

  it('shows GitHub settings button', () => {
    render(<AppShell />);

    // The settings/gear button
    expect(screen.getByTitle('GitHub Settings')).toBeInTheDocument();
  });

  it('opens GitHub modal on settings click', () => {
    render(<AppShell />);

    fireEvent.click(screen.getByTitle('GitHub Settings'));
    expect(screen.getByTestId('github-modal')).toBeInTheDocument();
  });
});
