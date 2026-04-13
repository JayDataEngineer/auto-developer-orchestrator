/**
 * @vitest-environment jsdom
 */

/**
 * DESKTOP SIDEBAR INTEGRATION TESTS — ZERO MOCKS
 *
 * Tests the FULL rendering pipeline:
 *   AppShell → Desktop tab → PiAgentView in left sidebar
 *   + SSE streaming → message rendering
 *
 * The ONLY thing replaced is globalThis.fetch.
 * All components, hooks, reducers, and parsers are REAL production code.
 */
import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react';
import { AppShell } from '../../src/components/AppShell';
import { PiAgentProvider } from '../../src/contexts/PiAgentContext';

// ─── SSE helpers ────────────────────────────────────────────────

function sseFrame(eventType: string, data: unknown): string {
  return `event: ${eventType}\ndata: ${JSON.stringify(data)}\n\n`;
}

function sseResponse(events: Array<{ type: string; data: unknown }>): Response {
  const body = events.map(e => sseFrame(e.type, e.data)).join('');
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

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
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

globalThis.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
  if (fetchInterceptor) {
    const result = fetchInterceptor(input, init);
    return result instanceof Promise ? result : Promise.resolve(result);
  }
  return originalFetch(input, init);
}) as any;

// ─── Test helper ────────────────────────────────────────────────

function renderWithProvider(ui: React.ReactElement) {
  return render(<PiAgentProvider>{ui}</PiAgentProvider>);
}

// ─── Tests ──────────────────────────────────────────────────────

describe('Desktop tab — full AppShell integration (zero mocks)', () => {
  beforeEach(() => {
    // Default fetch handler: return sensible defaults for all API routes
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();

      // Projects list — useOrchestrator expects { projects: string[] }
      if (url === '/api/projects' && !init?.method) {
        return jsonResponse({ projects: ['test-project'] });
      }
      // Pi prompt — SSE stream
      if (url.includes('/api/pi/prompt') && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'agent_end', data: { input: 0, output: 0, cache: 0 } },
        ]);
      }
      // Everything else: return empty JSON (works for config, github, pi state, etc.)
      return jsonResponse({});
    });
  });

  afterEach(() => {
    restoreFetch();
    vi.restoreAllMocks();
  });

  it('switches to Desktop tab and shows PiAgentView with input', async () => {
    renderWithProvider(<AppShell />);

    // Wait for projects to load and be selected
    await waitFor(() => {
      expect(screen.getByDisplayValue('test-project')).toBeInTheDocument();
    }, { timeout: 3000 });

    // Click Desktop tab
    await act(async () => {
      fireEvent.click(screen.getByText('Desktop'));
    });

    // Desktop tab should show the agent chat input
    await waitFor(() => {
      const textarea = screen.getByPlaceholderText('Describe a coding task...');
      expect(textarea).toBeInTheDocument();
    }, { timeout: 3000 });
  });

  it('sends a message on Desktop tab and sees assistant response', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();

      if (url === '/api/projects' && !init?.method) return jsonResponse({ projects: ['test-project'] });
      if (url.includes('/api/config/ai')) return jsonResponse({});
      if (url.includes('/api/config/github')) return jsonResponse({ connected: false });
      if (url.includes('/api/pi/state')) return jsonResponse({});
      if (url.includes('/api/pi/messages')) return jsonResponse([]);
      if (url.includes('/api/pi/models')) return jsonResponse([]);
      if (url.includes('/api/pi/history')) return jsonResponse([]);
      if (url.includes('/api/sandbox')) return jsonResponse({ sandboxes: [] });

      // Prompt returns assistant reply about navigating to google.com
      if (url.includes('/api/pi/prompt') && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'text_delta', data: { text: 'I navigated to google.com successfully.' } },
          { type: 'agent_end', data: { input: 50, output: 20, cache: 0 } },
        ]);
      }
      return jsonResponse({});
    });

    renderWithProvider(<AppShell />);

    // Wait for project auto-select
    await waitFor(() => {
      expect(screen.getByDisplayValue('test-project')).toBeInTheDocument();
    }, { timeout: 3000 });

    // Switch to Desktop
    await act(async () => {
      fireEvent.click(screen.getByText('Desktop'));
    });

    const textarea = await screen.findByPlaceholderText('Describe a coding task...', undefined, { timeout: 3000 });

    // Type and send
    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Go to google.com' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 300));
    });

    // THE BUG: assistant response MUST be visible
    await waitFor(() => {
      expect(screen.getByText(/I navigated to google\.com successfully/)).toBeInTheDocument();
    }, { timeout: 3000 });
  });

  it('shows streaming text delta on Desktop tab', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();

      if (url === '/api/projects' && !init?.method) return jsonResponse({ projects: ['test-project'] });
      if (url.includes('/api/config/ai')) return jsonResponse({});
      if (url.includes('/api/config/github')) return jsonResponse({ connected: false });
      if (url.includes('/api/pi/state')) return jsonResponse({});
      if (url.includes('/api/pi/messages')) return jsonResponse([]);
      if (url.includes('/api/pi/models')) return jsonResponse([]);
      if (url.includes('/api/pi/history')) return jsonResponse([]);
      if (url.includes('/api/sandbox')) return jsonResponse({ sandboxes: [] });

      if (url.includes('/api/pi/prompt') && init?.method === 'POST') {
        return sseStreamingResponse([
          [{ type: 'agent_start', data: {} }],
          [{ type: 'text_delta', data: { text: 'Opening' } }],
          [{ type: 'text_delta', data: { text: ' browser...' } }],
          [{ type: 'agent_end', data: { input: 30, output: 10, cache: 0 } }],
        ]);
      }
      return jsonResponse({});
    });

    renderWithProvider(<AppShell />);

    await waitFor(() => {
      expect(screen.getByDisplayValue('test-project')).toBeInTheDocument();
    }, { timeout: 3000 });

    await act(async () => {
      fireEvent.click(screen.getByText('Desktop'));
    });

    const textarea = await screen.findByPlaceholderText('Describe a coding task...', undefined, { timeout: 3000 });

    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Open browser' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 400));
    });

    await waitFor(() => {
      expect(screen.getByText(/Opening browser\.\.\./)).toBeInTheDocument();
    }, { timeout: 3000 });
  });

  it('shows thinking then text on Desktop tab', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();

      if (url === '/api/projects' && !init?.method) return jsonResponse({ projects: ['test-project'] });
      if (url.includes('/api/pi/prompt') && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'thinking_delta', data: { text: 'Planning...' } },
          { type: 'text_delta', data: { text: 'I thought about it.' } },
          { type: 'agent_end', data: { input: 30, output: 10, cache: 0 } },
        ]);
      }
      return jsonResponse({});
    });

    renderWithProvider(<AppShell />);

    await waitFor(() => {
      expect(screen.getByDisplayValue('test-project')).toBeInTheDocument();
    }, { timeout: 3000 });

    await act(async () => {
      fireEvent.click(screen.getByText('Desktop'));
    });

    const textarea = await screen.findByPlaceholderText('Describe a coding task...', undefined, { timeout: 3000 });

    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Think hard' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 500));
    });

    // Text after thinking must be visible
    await waitFor(() => {
      expect(screen.getByText(/I thought about it/)).toBeInTheDocument();
    }, { timeout: 3000 });
  });

  it('shows tool calls and text on Desktop tab', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();

      if (url === '/api/projects' && !init?.method) return jsonResponse({ projects: ['test-project'] });
      if (url.includes('/api/pi/prompt') && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'tool_call', data: { id: 'tc1', name: 'bash', input: { command: 'echo hello' } } },
          { type: 'tool_result', data: { id: 'tc1', output: 'hello' } },
          { type: 'text_delta', data: { text: 'Command ran successfully.' } },
          { type: 'agent_end', data: { input: 50, output: 20, cache: 0 } },
        ]);
      }
      return jsonResponse({});
    });

    renderWithProvider(<AppShell />);

    await waitFor(() => {
      expect(screen.getByDisplayValue('test-project')).toBeInTheDocument();
    }, { timeout: 3000 });

    await act(async () => {
      fireEvent.click(screen.getByText('Desktop'));
    });

    const textarea = await screen.findByPlaceholderText('Describe a coding task...', undefined, { timeout: 3000 });

    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Run echo' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 500));
    });

    // Tool call and text should appear
    await waitFor(() => {
      expect(screen.getByText(/Command ran successfully/)).toBeInTheDocument();
    }, { timeout: 5000 });
  });

  it('BUG: user message is visible after sending on Desktop tab', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : (input as URL).toString();

      if (url === '/api/projects' && !init?.method) return jsonResponse({ projects: ['test-project'] });
      if (url.includes('/api/config/ai')) return jsonResponse({});
      if (url.includes('/api/config/github')) return jsonResponse({ connected: false });
      if (url.includes('/api/pi/state')) return jsonResponse({});
      if (url.includes('/api/pi/messages')) return jsonResponse([]);
      if (url.includes('/api/pi/models')) return jsonResponse([]);
      if (url.includes('/api/pi/history')) return jsonResponse([]);
      if (url.includes('/api/sandbox')) return jsonResponse({ sandboxes: [] });

      if (url.includes('/api/pi/prompt') && init?.method === 'POST') {
        return sseResponse([
          { type: 'agent_start', data: {} },
          { type: 'text_delta', data: { text: 'Sure, I will do that.' } },
          { type: 'agent_end', data: { input: 50, output: 20, cache: 0 } },
        ]);
      }
      return jsonResponse({});
    });

    renderWithProvider(<AppShell />);

    await waitFor(() => {
      expect(screen.getByDisplayValue('test-project')).toBeInTheDocument();
    }, { timeout: 3000 });

    await act(async () => {
      fireEvent.click(screen.getByText('Desktop'));
    });

    const textarea = await screen.findByPlaceholderText('Describe a coding task...', undefined, { timeout: 3000 });

    await act(async () => {
      fireEvent.change(textarea, { target: { value: 'Go to google.com' } });
    });
    await act(async () => {
      fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: false });
      await new Promise(r => setTimeout(r, 300));
    });

    await waitFor(() => {
      expect(screen.getByText('Go to google.com')).toBeInTheDocument();
    }, { timeout: 3000 });

    await waitFor(() => {
      expect(screen.getByText(/Sure, I will do that/)).toBeInTheDocument();
    }, { timeout: 3000 });
  });
});
