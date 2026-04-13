import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PiAgentView } from '../../src/components/PiAgentView';

// Mock the PiAgentContext
vi.mock('../../src/contexts/PiAgentContext', () => {
  const context = {
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
  };
  return {
    PiAgentProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
    usePiAgentContext: () => context,
  };
});

// Mock react-syntax-highlighter
vi.mock('react-syntax-highlighter', () => ({
  Prism: ({ children }: any) => <pre>{children}</pre>,
}));

vi.mock('react-syntax-highlighter/dist/esm/styles/prism', () => ({
  oneDark: {},
}));

describe('PiAgentView', () => {
  it('renders the agent view with prompt input', () => {
    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    expect(screen.getByText('PI')).toBeInTheDocument();
    expect(screen.getByText('CODING AGENT')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Describe a coding task...')).toBeInTheDocument();
  });

  it('shows empty state when no messages', () => {
    render(<PiAgentView selectedProject="test-project" />);

    expect(screen.getByText('Pi Agent Ready')).toBeInTheDocument();
  });

  it('disables input when no project is selected', () => {
    render(<PiAgentView selectedProject={undefined} />);

    const textarea = screen.getByPlaceholderText('Select a project first...');
    expect(textarea).toBeDisabled();
  });
});
