import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AppShell } from '../../src/components/AppShell';

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

vi.mock('../../src/contexts/PuxAgentContext', () => ({
  PuxAgentProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  usePuxAgentContext: () => ({
    state: {
      messages: [],
      isStreaming: false,
      text: '',
      thinking: '',
      toolCalls: [],
      model: 'test-model',
      tokenUsage: { input: 0, output: 0, cache: 0 },
      error: null,
      branchName: null,
      lastPrompt: '',
      agentId: 'default',
      prUrl: null,
      prNumber: null,
      subAgents: [],
    },
    sendPrompt: vi.fn(),
    abort: vi.fn(),
    compact: vi.fn(),
    switchModel: vi.fn(),
    reset: vi.fn(),
    hydrateState: vi.fn(),
    getModels: vi.fn(async () => []),
    loadHistory: vi.fn(),
    respondToApproval: vi.fn(),
  }),
}));

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

vi.mock('../../src/components/GitHubConnectModal', () => ({
  GitHubConnectModal: ({ isOpen, onClose }: any) =>
    isOpen ? <div data-testid="github-modal">GitHub Modal</div> : null,
}));

describe('AppShell', () => {
  it('renders 3 tabs with labels', () => {
    render(<AppShell />);

    expect(screen.getByText('Code')).toBeInTheDocument();
    expect(screen.getByText('Automate')).toBeInTheDocument();
    expect(screen.getByText('Pilot')).toBeInTheDocument();
  });

  it('defaults to Agent tab', () => {
    render(<AppShell />);

    expect(screen.getByTestId('agent-tab')).toBeInTheDocument();
  });

  it('switches to Tasks tab on click', () => {
    render(<AppShell />);

    fireEvent.click(screen.getByText('Automate'));
    expect(screen.getByTestId('tasks-tab')).toBeInTheDocument();
    expect(screen.queryByTestId('agent-tab')).not.toBeInTheDocument();
  });

  it('switches to Desktop tab on click', () => {
    render(<AppShell />);

    fireEvent.click(screen.getByText('Pilot'));
    expect(screen.getByTestId('desktop-tab')).toBeInTheDocument();
  });

  it('renders project selector with projects', () => {
    render(<AppShell />);

    expect(screen.getByText('test-project')).toBeInTheDocument();
  });

  it('renders Pux branding', () => {
    render(<AppShell />);

    expect(screen.getByText('Pux')).toBeInTheDocument();
  });

  it('shows Settings button', () => {
    render(<AppShell />);

    expect(screen.getByText('Settings')).toBeInTheDocument();
  });

  it('opens GitHub modal via custom event', () => {
    render(<AppShell />);

    fireEvent(window, new Event('open-github-settings'));
    expect(screen.getByTestId('github-modal')).toBeInTheDocument();
  });
});
