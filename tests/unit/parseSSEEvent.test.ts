/**
 * parseSSEEvent Unit Tests
 *
 * Tests the SSE text → PiSSEEvent parser.
 * Pure function — no side effects, no React.
 */
import { describe, it, expect } from 'vitest';
import { parseSSEEvent } from '../../src/lib/pi-events';

describe('parseSSEEvent', () => {
  it('parses a valid agent_start event', () => {
    const result = parseSSEEvent('event: agent_start\ndata: {}');
    expect(result).toEqual({ type: 'agent_start', data: {} });
  });

  it('parses a valid text_delta event with JSON data', () => {
    const result = parseSSEEvent('event: text_delta\ndata: {"text":"hello"}');
    expect(result).toEqual({ type: 'text_delta', data: { text: 'hello' } });
  });

  it('parses tool_execution_start with complex args', () => {
    const raw = 'event: tool_execution_start\ndata: {"toolName":"bash","args":{"command":"ls -la"},"toolId":"tc-1"}';
    const result = parseSSEEvent(raw);
    expect(result).not.toBeNull();
    expect(result!.type).toBe('tool_execution_start');
    expect(result!.data).toEqual({
      toolName: 'bash',
      args: { command: 'ls -la' },
      toolId: 'tc-1',
    });
  });

  it('returns null when event type is missing', () => {
    const result = parseSSEEvent('data: {"text":"hello"}');
    expect(result).toBeNull();
  });

  it('returns null when data is missing', () => {
    const result = parseSSEEvent('event: agent_start');
    expect(result).toBeNull();
  });

  it('returns null when data is invalid JSON', () => {
    const result = parseSSEEvent('event: text_delta\ndata: {not json}');
    expect(result).toBeNull();
  });

  it('returns null for empty string', () => {
    const result = parseSSEEvent('');
    expect(result).toBeNull();
  });

  it('handles extra whitespace in event type', () => {
    const result = parseSSEEvent('event: agent_end \ndata: {"input":0,"output":0,"cache":0}');
    expect(result).not.toBeNull();
    expect(result!.type).toBe('agent_end');
  });
});
