/**
 * @vitest-environment jsdom
 */

/**
 * INTEGRATION TESTS — ZERO MOCKS
 *
 * These tests exercise the REAL pipeline:
 *   fetch → SSE stream → readSSEStream → parseSSEEvent → useThrottledDeltas
 *   → agentReducer → setState → PiAgentView re-render
 *
 * The ONLY thing replaced is globalThis.fetch — we inject a fake Response
 * with a ReadableStream body that produces real SSE-formatted text.
 * Every component, hook, reducer, parser, and utility is the REAL production code.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { PiAgentView } from '../../src/components/PiAgentView';

// ─── SSE helpers ────────────────────────────────────────────────

/** Build a single SSE frame: `event: <type>\ndata: <json>\n\n` */
function sseFrame(eventType: string, data: unknown): string {
  return `event: ${eventType}\ndata: ${JSON.stringify(data)}\n\n`;
}

/** Build a full SSE stream body from an array of events */
function sseBody(events: Array<{ type: string; data: unknown }>): string {
  return events.map(e => sseFrame(e.type, e.data)).join('');
}

/** Create a real Response with a ReadableStream body containing SSE data */
function sseResponse(events: Array<{ type: string; data: unknown }>): Response {
  const body = sseBody(events);
  const encoder = new TextEncoder();
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode(body));
      controller.close();
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

/** Create a Response that delivers SSE events one chunk at a time (simulates streaming) */
function sseStreamingResponse(
  eventChunks: Array<Array<{ type: string; data: unknown }>>,
): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream({
    start(controller) {
      for (const chunk of eventChunks) {
        const text = chunk.map(e => sseFrame(e.type, e.data)).join('');
        controller.enqueue(encoder.encode(text));
      }
      controller.close();
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

// ─── Fetch interceptor ──────────────────────────────────────────

const originalFetch = globalThis.fetch;
let fetchInterceptor: ((input: RequestInfo | URL, init?: RequestInit) => Response | Promise<Response>) | null = null;

function mockFetch(
  handler: (input: RequestInfo | URL, init?: RequestInit) => Response | Promise<Response>,
) {
  fetchInterceptor = handler;
}

function restoreFetch() {
  fetchInterceptor = null;
}

// Override globalThis.fetch once — intercepts ALL network calls
globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
  if (fetchInterceptor) {
    const result = fetchInterceptor(input, init);
    return result instanceof Promise ? result : Promise.resolve(result);
  }
  return originalFetch(input, init);
}) as any;

// ─── Tests ──────────────────────────────────────────────────────

describe('PiAgentView — real integration (zero mocks)', () => {
  beforeEach(() => {
    // Default: return empty JSON for any non-prompt call
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      // POST /api/pi/prompt → SSE stream
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        // Default: empty agent cycle
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'agent_end', data: { input: 0, output: 0, cache: 0 } },
        ]);
      }
      // Everything else: return empty JSON
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });
  });

  afterEach(() => {
    restoreFetch();
    vi.restoreAllMocks();
  });

  it('renders without crashing when no project is selected', () => {
    const { container } = render(<PiAgentView />);
    // Should show the "Pi Agent Ready" empty state
    expect(screen.getByText('Pi Agent Ready')).toBeInTheDocument();
    // No crash, no error boundary
    expect(container.innerHTML).toBeTruthy();
  });

  it('renders without crashing with a project selected', () => {
    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);
    expect(screen.getByText('Pi Agent Ready')).toBeInTheDocument();
  });

  it('does NOT crash when user sends a message (the page-reset bug)', async () => {
    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    expect(textarea).toBeInTheDocument();

    // Type a message and press Enter
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Hello, write a hello world' } });
    });

    // The critical test: sending should NOT throw or cause a re-render crash
    let error: Error | null = null;
    try {
      await act(async () => {
        fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
        // Wait for the fetch to resolve and SSE stream to process
        await new Promise(r => setTimeout(r, 100));
      });
    } catch (e) {
      error = e as Error;
    }

    expect(error).toBeNull();
    // The component should still be mounted and functional
    expect(screen.getByPlaceholderText('Describe a coding task...')).toBeInTheDocument();
  });

  it('shows the user message after sending', async () => {
    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'List my files' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 100));
    });

    // User message should be rendered
    expect(screen.getByText('List my files')).toBeInTheDocument();
  });

  it('renders assistant text from SSE agent_start → text_delta → agent_end', async () => {
    // Set up fetch to return a full conversation with text
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseStreamingResponse([
          [{ type: 'agent_start', data: {} }],
          [{ type: 'text_delta', data: { text: 'Hello! ' } }],
          [{ type: 'text_delta', data: { text: 'I can help.' } }],
          [{ type: 'agent_end', data: { input: 100, output: 50, cache: 10 } }],
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Say hello' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      // Wait for SSE stream processing + RAF flush of throttled deltas
      await new Promise(r => setTimeout(r, 250));
    });

    // The assistant text should be rendered (via MarkdownBlock)
    expect(screen.getByText(/Hello!/)).toBeInTheDocument();
    expect(screen.getByText(/I can help/)).toBeInTheDocument();
  });

  it('renders thinking/reasoning block from agent_end with thinking field', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          {
            type: 'agent_end',
            data: { input: 200, output: 100, cache: 50, thinking: 'I need to analyze the code first.' },
          },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Analyze this' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 150));
    });

    // ReasoningBlock should be rendered with "Reasoning" header
    expect(screen.getByText('Reasoning')).toBeInTheDocument();
    // Character count should show
    expect(screen.getByText(/chars/)).toBeInTheDocument();
  });

  it('renders thinking from thinking_delta events', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseStreamingResponse([
          [{ type: 'agent_start', data: {} }],
          [{ type: 'thinking_delta', data: { text: 'Let me think about this...' } }],
          [{ type: 'agent_end', data: { input: 100, output: 50, cache: 0 } }],
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Think about it' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      // Wait for RAF-based throttled delta flush
      await new Promise(r => setTimeout(r, 250));
    });

    // ReasoningBlock should appear
    expect(screen.getByText('Reasoning')).toBeInTheDocument();
  });

  it('renders tool calls from tool_execution_start and tool_execution_end', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          {
            type: 'tool_execution_start',
            data: {
              toolName: 'bash',
              args: { command: 'ls -la' },
              toolId: 'tool-int-1',
            },
          },
          {
            type: 'tool_execution_end',
            data: {
              toolId: 'tool-int-1',
              toolName: 'bash',
              result: 'file1.txt\nfile2.txt',
            },
          },
          { type: 'agent_end', data: { input: 300, output: 150, cache: 50 } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'List files' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 150));
    });

    // ToolCallItem should render the tool name
    expect(screen.getByText('bash')).toBeInTheDocument();
    // And the formatted args
    expect(screen.getByText('ls -la')).toBeInTheDocument();
  });

  it('renders multiple tool calls in sequence', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          {
            type: 'tool_execution_start',
            data: { toolName: 'read', args: { filePath: '/src/main.ts' }, toolId: 'tc-1' },
          },
          {
            type: 'tool_execution_end',
            data: { toolId: 'tc-1', toolName: 'read', result: 'file contents here' },
          },
          {
            type: 'tool_execution_start',
            data: { toolName: 'edit', args: { filePath: '/src/main.ts' }, toolId: 'tc-2' },
          },
          {
            type: 'tool_execution_end',
            data: { toolId: 'tc-2', toolName: 'edit', result: 'ok' },
          },
          {
            type: 'tool_execution_start',
            data: { toolName: 'bash', args: { command: 'npm test' }, toolId: 'tc-3' },
          },
          {
            type: 'tool_execution_end',
            data: { toolId: 'tc-3', toolName: 'bash', result: 'all tests pass' },
          },
          { type: 'agent_end', data: { input: 500, output: 200, cache: 100 } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Fix the bug' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 200));
    });

    expect(screen.getByText('read')).toBeInTheDocument();
    expect(screen.getByText('edit')).toBeInTheDocument();
    expect(screen.getByText('bash')).toBeInTheDocument();
  });

  it('renders tool call result when expanded', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          {
            type: 'tool_execution_start',
            data: { toolName: 'bash', args: { command: 'echo hello' }, toolId: 'tc-expand' },
          },
          {
            type: 'tool_execution_end',
            data: { toolId: 'tc-expand', toolName: 'bash', result: 'hello world output' },
          },
          { type: 'agent_end', data: { input: 100, output: 50, cache: 0 } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Run echo' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 200));
    });

    // Tool is complete (has endTime), so it starts collapsed. Click to expand.
    const bashButton = screen.getByText('bash').closest('button')!;
    await act(async () => {
      fireEvent.click(bashButton);
    });

    // Result should now be visible
    expect(screen.getByText('hello world output')).toBeInTheDocument();
  });

  it('shows "Thinking..." spinner when streaming with no text yet', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        // Return a stream that stalls — agent_start but never agent_end
        // Use a ReadableStream that stays open
        const encoder = new TextEncoder();
        const stream = new ReadableStream({
          start(controller) {
            controller.enqueue(encoder.encode(sseFrame('agent_start', {})));
            // Don't close — simulates ongoing streaming
          },
        });
        return new Response(stream, {
          status: 200,
          headers: { 'Content-Type': 'text/event-stream' },
        });
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Think hard' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 150));
    });

    // "Thinking..." spinner should appear for the streaming assistant with no text
    expect(screen.getByText('Thinking...')).toBeInTheDocument();
    // "Streaming" indicator in header
    expect(screen.getByText('Streaming')).toBeInTheDocument();
  });

  it('shows error state from SSE error event', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'error', data: { error: 'Model connection failed' } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Do stuff' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 150));
    });

    // Error should be displayed in the header
    expect(screen.getByText(/Model connection failed/)).toBeInTheDocument();
  });

  it('handles HTTP error from prompt endpoint', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return new Response(JSON.stringify({ error: 'Project not found' }), {
          status: 404,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Try this' } });
    });

    let error: Error | null = null;
    try {
      await act(async () => {
        fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
        await new Promise(r => setTimeout(r, 100));
      });
    } catch (e) {
      error = e as Error;
    }

    expect(error).toBeNull();
    // Error state should be shown
    expect(screen.getByText(/Project not found/)).toBeInTheDocument();
  });

  it('handles tool_execution_end with error field', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          {
            type: 'tool_execution_start',
            data: { toolName: 'bash', args: { command: 'rm -rf /' }, toolId: 'tc-err' },
          },
          {
            type: 'tool_execution_end',
            data: { toolId: 'tc-err', toolName: 'bash', result: null, error: 'Permission denied' },
          },
          { type: 'agent_end', data: { input: 100, output: 50, cache: 0 } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Delete everything' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 200));
    });

    // Tool should be rendered — click to expand and see error
    const bashButton = screen.getByText('bash').closest('button')!;
    await act(async () => {
      fireEvent.click(bashButton);
    });

    expect(screen.getByText('Permission denied')).toBeInTheDocument();
  });

  it('survives rapid double-send without crashing', async () => {
    let callCount = 0;
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        callCount++;
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'agent_end', data: { input: 10, output: 5, cache: 0 } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');

    // First send
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'First message' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 100));
    });

    // Second send immediately after
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Second message' } });
    });

    let error: Error | null = null;
    try {
      await act(async () => {
        fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
        await new Promise(r => setTimeout(r, 200));
      });
    } catch (e) {
      error = e as Error;
    }

    expect(error).toBeNull();
    // Both prompts were sent
    expect(callCount).toBeGreaterThanOrEqual(2);
    // Component still alive
    expect(screen.getByPlaceholderText('Describe a coding task...')).toBeInTheDocument();
  });

  it('renders full pipeline: thinking + text + tools in one response', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseStreamingResponse([
          [{ type: 'agent_start', data: {} }],
          [{ type: 'thinking_delta', data: { text: 'I need to check the files first.' } }],
          [
            { type: 'tool_execution_start', data: { toolName: 'bash', args: { command: 'ls' }, toolId: 'ftc-1' } },
            { type: 'tool_execution_end', data: { toolId: 'ftc-1', toolName: 'bash', result: 'main.ts\nutils.ts' } },
          ],
          [{ type: 'text_delta', data: { text: 'Here are your files: main.ts and utils.ts.' } }],
          [{ type: 'agent_end', data: { input: 400, output: 200, cache: 100 } }],
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'List all files' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      // Wait for RAF-throttled deltas to flush
      await new Promise(r => setTimeout(r, 300));
    });

    // User message rendered
    expect(screen.getByText('List all files')).toBeInTheDocument();
    // Tool call rendered
    expect(screen.getByText('bash')).toBeInTheDocument();
    // Text rendered
    expect(screen.getByText(/Here are your files/)).toBeInTheDocument();
    // Token usage rendered (400in / 200out / 100cache)
    expect(screen.getByText(/Tokens:/)).toBeInTheDocument();
  });

  it('does NOT send when input is empty', async () => {
    let promptCalled = false;
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        promptCalled = true;
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 50));
    });

    expect(promptCalled).toBe(false);
  });

  it('resets state when "New Task" button is clicked', async () => {
    // First, populate with a message
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'text_delta', data: { text: 'Hello world' } },
          { type: 'agent_end', data: { input: 100, output: 50, cache: 0 } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Say hi' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 250));
    });

    // Verify content exists
    expect(screen.getByText('Say hi')).toBeInTheDocument();

    // Click "New Task"
    const resetButton = screen.getByText('New Task').closest('button')!;
    await act(async () => {
      fireEvent.click(resetButton);
    });

    // Should show empty state again
    expect(screen.getByText('Pi Agent Ready')).toBeInTheDocument();
  });

  it('BUG: loadHistory arriving after sendPrompt OVERWRITES user messages (page reset)', async () => {
    // This test reproduces the exact bug the user reports:
    // 1. Page loads → loadHistory starts fetching (async, slow)
    // 2. User sends a message → sendPrompt adds user+assistant messages
    // 3. loadHistory resolves LATE → overwrites messages array with old history
    // 4. User's message disappears — appears as "page reset"

    let resolveMessages: (() => void) | null = null;

    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();

      // /api/pi/messages — DELAYED response (simulates slow backend)
      if (url.includes('/api/pi/messages')) {
        return new Promise<Response>((resolve) => {
          resolveMessages = () => {
            resolve(new Response(JSON.stringify([
              { id: 'old-1', role: 'user', content: 'Old question from yesterday', createdAt: new Date().toISOString() },
              { id: 'old-2', role: 'assistant', text: 'Old answer from yesterday', thinking: '', toolCalls: '[]', createdAt: new Date().toISOString() },
            ]), {
              status: 200,
              headers: { 'Content-Type': 'application/json' },
            }));
          };
        });
      }

      // /api/pi/prompt → normal SSE
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'text_delta', data: { text: 'I fixed the bug.' } },
          { type: 'agent_end', data: { input: 100, output: 50, cache: 0 } },
        ]);
      }

      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    // Wait a tick for mount effects to fire (loadHistory is now pending)
    await act(async () => {
      await new Promise(r => setTimeout(r, 50));
    });

    const textarea = screen.getByPlaceholderText('Describe a coding task...');

    // User sends a message
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Fix the login bug' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 200));
    });

    // User message should be visible
    expect(screen.getByText('Fix the login bug')).toBeInTheDocument();
    // Assistant text from SSE should also be visible
    expect(screen.getByText(/I fixed the bug/)).toBeInTheDocument();

    // NOW: loadHistory resolves late, returning old messages
    await act(async () => {
      resolveMessages!();
      await new Promise(r => setTimeout(r, 100));
    });

    // BUG: The user's message and the assistant's response MUST STILL BE VISIBLE
    // If loadHistory overwrites them, this is the "page refresh" bug
    // This assertion WILL FAIL right now — proving the bug exists
    expect(screen.getByText('Fix the login bug')).toBeInTheDocument();
    expect(screen.getByText(/I fixed the bug/)).toBeInTheDocument();
  });

  it('handles state_update event from SSE', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();
      if (url === '/api/pi/prompt' && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'state_update', data: { model: 'llamacpp/gemma-4-26b', input: 0, output: 0, cache: 0 } },
          { type: 'agent_end', data: { input: 3000, output: 150, cache: 2800 } },
        ]);
      }
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    });

    render(<PiAgentView selectedProject="test-project" projects={['test-project']} />);

    const textarea = screen.getByPlaceholderText('Describe a coding task...');
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Use gemma' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 150));
    });

    // Model from state_update should appear in the model selector
    expect(screen.getByText(/Model:.*llamacpp\/gemma-4-26b/)).toBeInTheDocument();
    // Token usage from agent_end should render
    expect(screen.getByText(/Tokens:/)).toBeInTheDocument();
  });
});
