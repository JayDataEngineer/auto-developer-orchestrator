/**
 * agentReducer Unit Tests
 *
 * Tests the pure state reducer — no React, no hooks, no side effects.
 * Every event type transition is covered.
 */
import { describe, it, expect } from 'vitest';
import {
  agentReducer,
  initialAgentState,
  updateLastAssistant,
  PiAgentState,
} from '../../src/hooks/agentReducer';
import { PiSSEEvent } from '../../src/lib/pi-events';

// Deterministic ID generators for repeatable tests
let msgCounter = 0;
let toolCounter = 0;
const genMsgId = () => `msg-${++msgCounter}`;
const genToolId = () => `tool-${++toolCounter}`;

function reduce(state: PiAgentState, event: PiSSEEvent): PiAgentState {
  return agentReducer(state, event, genMsgId, genToolId);
}

function reduceMany(state: PiAgentState, events: PiSSEEvent[]): PiAgentState {
  return events.reduce((s, e) => reduce(s, e), state);
}

describe('agentReducer', () => {
  beforeEach(() => {
    msgCounter = 0;
    toolCounter = 0;
  });

  // ── text_delta / thinking_delta ──────────────────────────────────

  it('text_delta returns state unchanged (handled upstream)', () => {
    const result = reduce(initialAgentState, {
      type: 'text_delta',
      data: { text: 'hello' },
    });
    expect(result).toEqual(initialAgentState);
  });

  it('thinking_delta returns state unchanged (handled upstream)', () => {
    const result = reduce(initialAgentState, {
      type: 'thinking_delta',
      data: { text: 'hmm' },
    });
    expect(result).toEqual(initialAgentState);
  });

  // ── agent_start ──────────────────────────────────────────────────

  it('agent_start sets isStreaming and creates streaming assistant message', () => {
    const result = reduce(initialAgentState, { type: 'agent_start', data: {} });
    expect(result.isStreaming).toBe(true);
    expect(result.text).toBe('');
    expect(result.thinking).toBe('');
    expect(result.toolCalls).toEqual([]);
    expect(result.error).toBeNull();
    expect(result.messages).toHaveLength(1);
    expect(result.messages[0].role).toBe('assistant');
    const msg = result.messages[0] as any;
    expect(msg.streaming).toBe(true);
    expect(msg.text).toBe('');
  });

  it('agent_start does NOT create duplicate assistant if one is already streaming', () => {
    const afterFirst = reduce(initialAgentState, { type: 'agent_start', data: {} });
    const afterSecond = reduce(afterFirst, { type: 'agent_start', data: {} });
    expect(afterSecond.messages).toHaveLength(1);
  });

  // ── agent_spawned ────────────────────────────────────────────────

  it('agent_spawned sets agentId', () => {
    const result = reduce(initialAgentState, {
      type: 'agent_spawned',
      data: { agentId: 'agent-xyz' },
    });
    expect(result.agentId).toBe('agent-xyz');
  });

  // ── tool_execution_start ─────────────────────────────────────────

  it('tool_execution_start adds ToolCall to toolCalls and assistant message', () => {
    const afterStart = reduce(initialAgentState, { type: 'agent_start', data: {} });
    const result = reduce(afterStart, {
      type: 'tool_execution_start',
      data: { toolName: 'bash', args: { command: 'ls' }, toolId: 'tc-1' },
    });
    expect(result.toolCalls).toHaveLength(1);
    expect(result.toolCalls[0].name).toBe('bash');
    expect(result.toolCalls[0].args).toEqual({ command: 'ls' });
    expect(result.toolCalls[0].id).toBe('tc-1');
    expect(result.toolCalls[0].result).toBeUndefined();
    // Also added to the assistant message
    const msg = result.messages[result.messages.length - 1] as any;
    expect(msg.toolCalls).toHaveLength(1);
  });

  it('tool_execution_start generates ID when toolId is empty', () => {
    const afterStart = reduce(initialAgentState, { type: 'agent_start', data: {} });
    const result = reduce(afterStart, {
      type: 'tool_execution_start',
      data: { toolName: 'read', args: {}, toolId: '' },
    });
    expect(result.toolCalls[0].id).toMatch(/^tool-/);
  });

  // ── tool_execution_end ───────────────────────────────────────────

  it('tool_execution_end sets result and endTime', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'tool_execution_start', data: { toolName: 'bash', args: {}, toolId: 'tc-1' } },
      { type: 'tool_execution_end', data: { toolId: 'tc-1', result: 'file1.txt' } },
    ]);
    expect(state.toolCalls[0].result).toBe('file1.txt');
    expect(state.toolCalls[0].endTime).toBeDefined();
  });

  it('tool_execution_end sets error field', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'tool_execution_start', data: { toolName: 'bash', args: {}, toolId: 'tc-e' } },
      { type: 'tool_execution_end', data: { toolId: 'tc-e', result: null, error: 'Permission denied' } },
    ]);
    expect(state.toolCalls[0].error).toBe('Permission denied');
  });

  it('tool_execution_end detects sub-agent spawn from bash result', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      {
        type: 'tool_execution_start',
        data: { toolName: 'bash', args: { command: 'pi /subagent/spawn code' }, toolId: 'tc-sp' },
      },
      {
        type: 'tool_execution_end',
        data: {
          toolId: 'tc-sp',
          result: 'Spawned sub-agent\n{"subAgentId":"sub-code-123","status":"running"}',
        },
      },
    ]);
    expect(state.subAgents).toHaveLength(1);
    expect(state.subAgents[0].subAgentId).toBe('sub-code-123');
    expect(state.subAgents[0].type).toBe('code');
  });

  // ── agent_end ────────────────────────────────────────────────────

  it('agent_end sets isStreaming=false and accumulates tokenUsage', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'agent_end', data: { input: 100, output: 50, cache: 10 } },
    ]);
    expect(state.isStreaming).toBe(false);
    expect(state.tokenUsage).toEqual({ input: 100, output: 50, cache: 10 });
  });

  it('agent_end sets thinking from agent_end data', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'agent_end', data: { input: 0, output: 0, cache: 0, thinking: 'I analyzed it.' } },
    ]);
    expect(state.thinking).toBe('I analyzed it.');
  });

  it('agent_end marks assistant message as non-streaming', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'agent_end', data: { input: 0, output: 0, cache: 0 } },
    ]);
    const msg = state.messages[state.messages.length - 1] as any;
    expect(msg.streaming).toBe(false);
  });

  it('agent_end accumulates token usage across multiple turns', () => {
    const first = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'agent_end', data: { input: 100, output: 50, cache: 10 } },
    ]);
    const second = reduceMany(first, [
      { type: 'agent_start', data: {} },
      { type: 'agent_end', data: { input: 200, output: 100, cache: 20 } },
    ]);
    expect(second.tokenUsage).toEqual({ input: 300, output: 150, cache: 30 });
  });

  // ── error ────────────────────────────────────────────────────────

  it('error event sets error and stops streaming', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'error', data: { error: 'Model connection failed' } },
    ]);
    expect(state.error).toBe('Model connection failed');
    expect(state.isStreaming).toBe(false);
  });

  // ── state_update ─────────────────────────────────────────────────

  it('state_update updates model and tokenUsage', () => {
    const result = reduce(initialAgentState, {
      type: 'state_update',
      data: { model: 'gemma-4-26b', input: 500, output: 100, cache: 400 },
    });
    expect(result.model).toBe('gemma-4-26b');
    expect(result.tokenUsage).toEqual({ input: 500, output: 100, cache: 400 });
  });

  // ── branch_created ───────────────────────────────────────────────

  it('branch_created sets branchName', () => {
    const result = reduce(initialAgentState, {
      type: 'branch_created',
      data: { branch: 'feature/auth' },
    });
    expect(result.branchName).toBe('feature/auth');
  });

  // ── commit_created ───────────────────────────────────────────────

  it('commit_created sets lastCommit and branchName', () => {
    const result = reduce(initialAgentState, {
      type: 'commit_created',
      data: { message: 'feat: add auth', branch: 'feature/auth' },
    });
    expect(result.lastCommit).toEqual({ message: 'feat: add auth', branch: 'feature/auth' });
    expect(result.branchName).toBe('feature/auth');
  });

  // ── push_complete ────────────────────────────────────────────────

  it('push_complete sets lastPush', () => {
    const result = reduce(initialAgentState, {
      type: 'push_complete',
      data: { branch: 'feature/auth' },
    });
    expect(result.lastPush).toEqual({ branch: 'feature/auth' });
  });

  // ── pr_created ──────────────────────────────────────────────────

  it('pr_created sets prUrl and prNumber', () => {
    const result = reduce(initialAgentState, {
      type: 'pr_created',
      data: { url: 'https://github.com/repo/pull/42', number: 42, title: 'Auth' },
    });
    expect(result.prUrl).toBe('https://github.com/repo/pull/42');
    expect(result.prNumber).toBe(42);
  });

  // ── web_update ──────────────────────────────────────────────────

  it('web_update sets webUpdate', () => {
    const result = reduce(initialAgentState, {
      type: 'web_update',
      data: { url: 'https://example.com', title: 'Example', screenshot: 'base64', elements: [] },
    });
    expect(result.webUpdate).toEqual({
      url: 'https://example.com',
      title: 'Example',
      screenshot: 'base64',
      elements: [],
    });
  });

  // ── approval_request / question_asked ────────────────────────────

  it('approval_request sets pendingApproval', () => {
    const result = reduce(initialAgentState, {
      type: 'approval_request',
      data: { requestId: 'req-1', type: 'tool_confirm', toolName: 'bash', message: 'Run rm', risk: 'high' },
    });
    expect(result.pendingApproval).toEqual({
      requestId: 'req-1',
      type: 'tool_confirm',
      toolName: 'bash',
      message: 'Run rm',
      risk: 'high',
    });
  });

  it('question_asked sets pendingApproval', () => {
    const result = reduce(initialAgentState, {
      type: 'question_asked',
      data: { requestId: 'req-2', type: 'question', message: 'Which account?', risk: 'low' },
    });
    expect(result.pendingApproval?.requestId).toBe('req-2');
  });

  // ── unknown event ────────────────────────────────────────────────

  it('unknown event type returns state unchanged', () => {
    const result = reduce(initialAgentState, {
      type: 'something_random' as any,
      data: {},
    });
    expect(result).toEqual(initialAgentState);
  });

  // ── full pipeline ────────────────────────────────────────────────

  it('full pipeline: start → tool → text → end produces correct final state', () => {
    const state = reduceMany(initialAgentState, [
      { type: 'agent_start', data: {} },
      { type: 'thinking_delta', data: { text: 'Let me check...' } },
      { type: 'tool_execution_start', data: { toolName: 'bash', args: { command: 'ls' }, toolId: 'tc-pipe' } },
      { type: 'tool_execution_end', data: { toolId: 'tc-pipe', result: 'main.ts' } },
      { type: 'text_delta', data: { text: 'Found one file.' } },
      { type: 'state_update', data: { model: 'gemma-4-26b', input: 200, output: 50, cache: 100 } },
      { type: 'agent_end', data: { input: 200, output: 50, cache: 100, thinking: 'Let me check...' } },
    ]);

    expect(state.isStreaming).toBe(false);
    expect(state.toolCalls).toHaveLength(1);
    expect(state.toolCalls[0].result).toBe('main.ts');
    expect(state.thinking).toBe('Let me check...');
    expect(state.tokenUsage).toEqual({ input: 400, output: 100, cache: 200 });
    expect(state.model).toBe('gemma-4-26b');
    expect(state.messages).toHaveLength(1);
    const msg = state.messages[0] as any;
    expect(msg.streaming).toBe(false);
    expect(msg.toolCalls).toHaveLength(1);
  });
});

// ── updateLastAssistant helper ──────────────────────────────────

describe('updateLastAssistant', () => {
  it('updates the last assistant message', () => {
    const msgs = [
      { id: '1', role: 'user' as const, content: 'hi', timestamp: 0 },
      { id: '2', role: 'assistant' as const, text: 'hello', thinking: '', toolCalls: [], timestamp: 0 },
    ];
    const result = updateLastAssistant(msgs, msg => ({ ...msg, text: msg.text + ' world' }));
    expect((result[1] as any).text).toBe('hello world');
  });

  it('returns unchanged if last message is not assistant', () => {
    const msgs = [{ id: '1', role: 'user' as const, content: 'hi', timestamp: 0 }];
    const result = updateLastAssistant(msgs, msg => ({ ...msg, text: 'x' }));
    expect(result).toEqual(msgs);
  });

  it('returns unchanged for empty array', () => {
    const result = updateLastAssistant([], msg => ({ ...msg, text: 'x' }));
    expect(result).toEqual([]);
  });
});
