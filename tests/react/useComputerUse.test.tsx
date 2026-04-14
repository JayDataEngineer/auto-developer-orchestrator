/**
 * @vitest-environment jsdom
 */

/**
 * useComputerUse Hook Tests — ZERO MOCKS
 *
 * Tests the real hook with real state transitions.
 * Only globalThis.fetch is intercepted to return controlled responses.
 */
import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useComputerUse } from '../../src/hooks/useComputerUse';

// ─── Helpers ──────────────────────────────────────────────────

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// ─── Fetch interceptor ────────────────────────────────────────

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

// ─── Tests ────────────────────────────────────────────────────

describe('useComputerUse', () => {
  beforeEach(() => {
    restoreFetch();
  });

  afterEach(() => {
    restoreFetch();
    vi.restoreAllMocks();
  });

  // ── Initial state ─────────────────────────────────────────

  it('initial state is all defaults', () => {
    const { result } = renderHook(() => useComputerUse());

    expect(result.current.enabled).toBe(false);
    expect(result.current.sandboxId).toBeNull();
    expect(result.current.screenshot).toBeNull();
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
    expect(result.current.cdpPort).toBeNull();
    expect(result.current.url).toBe('');
    expect(result.current.title).toBe('');
    expect(result.current.elements).toEqual([]);
  });

  // ── enableComputerUse ─────────────────────────────────────

  it('enableComputerUse sets loading then enabled', async () => {
    mockFetch((_input, _init) => {
      return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    expect(result.current.sandboxId).toBe('sb-1');
    expect(result.current.cdpPort).toBe(19222);
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
  });

  it('enableComputerUse retries on first failure', async () => {
    let callCount = 0;
    mockFetch((_input, _init) => {
      callCount++;
      if (callCount === 1) {
        return new Response(JSON.stringify({ error: 'fail' }), { status: 500, headers: { 'Content-Type': 'application/json' } });
      }
      return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    expect(callCount).toBe(2);
  });

  it('enableComputerUse sets error on both failures', async () => {
    mockFetch((_input, _init) => {
      return new Response(JSON.stringify({ error: 'broken' }), { status: 500, headers: { 'Content-Type': 'application/json' } });
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.error).toBeTruthy();
    });

    expect(result.current.enabled).toBe(false);
    expect(result.current.loading).toBe(false);
  });

  it('enableComputerUse timeout message on AbortError', async () => {
    mockFetch((_input, _init) => {
      const abortErr = new DOMException('The operation was aborted', 'AbortError');
      return Promise.reject(abortErr);
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.error).toContain('timed out');
    });
  });

  // ── disableComputerUse ────────────────────────────────────

  it('disableComputerUse resets to initial state', async () => {
    // First enable
    mockFetch((_input, init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/disable')) {
        return jsonResponse({ disabled: true });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.disableComputerUse();
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(false);
    });
    expect(result.current.sandboxId).toBeNull();
    expect(result.current.loading).toBe(false);
  });

  it('disableComputerUse sets error on failure', async () => {
    // Enable first
    mockFetch((_input, _init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/disable')) {
        return new Response(JSON.stringify({ error: 'fail' }), { status: 500, headers: { 'Content-Type': 'application/json' } });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.disableComputerUse();
    });

    await waitFor(() => {
      expect(result.current.error).toBeTruthy();
    });
  });

  // ── takeScreenshot ────────────────────────────────────────

  it('takeScreenshot updates screenshot and url', async () => {
    mockFetch((_input, _init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/screenshot')) {
        return jsonResponse({
          image: 'data:image/png;base64,abc123',
          description: 'A webpage',
          url: 'https://example.com',
          title: 'Example',
        });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.takeScreenshot();
    });

    await waitFor(() => {
      expect(result.current.screenshot).toBe('data:image/png;base64,abc123');
    });
    expect(result.current.url).toBe('https://example.com');
    expect(result.current.title).toBe('Example');
  });

  it('takeScreenshot sets error on failure', async () => {
    mockFetch((_input, _init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/screenshot')) {
        return new Response(JSON.stringify({ error: 'no screenshot' }), { status: 500, headers: { 'Content-Type': 'application/json' } });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.takeScreenshot();
    });

    await waitFor(() => {
      expect(result.current.error).toBeTruthy();
    });
  });

  // ── getSnapshot ───────────────────────────────────────────

  it('getSnapshot updates elements and url', async () => {
    mockFetch((_input, _init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/snapshot')) {
        return jsonResponse({
          url: 'https://example.com',
          title: 'Example',
          elements: [
            { id: 1, tag: 'button', text: 'Click me', selector: 'button' },
          ],
        });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.getSnapshot();
    });

    await waitFor(() => {
      expect(result.current.elements).toHaveLength(1);
    });
    expect(result.current.elements[0].tag).toBe('button');
    expect(result.current.url).toBe('https://example.com');
  });

  // ── act ───────────────────────────────────────────────────

  it('act navigate sends action and auto-screenshots', async () => {
    let actCalled = false;
    let screenshotCalled = false;

    mockFetch((_input, _init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/act')) {
        actCalled = true;
        return jsonResponse({
          url: 'https://example.com',
          title: 'Example',
          elements: [],
        });
      }
      if (url.includes('/computer-use/screenshot')) {
        screenshotCalled = true;
        return jsonResponse({
          image: 'data:image/png;base64,screenshot',
          url: 'https://example.com',
          title: 'Example',
        });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.act({ action: 'navigate', url: 'https://example.com' });
    });

    await waitFor(() => {
      expect(actCalled).toBe(true);
    });

    // Auto-screenshot happens asynchronously
    await waitFor(() => {
      expect(screenshotCalled).toBe(true);
    }, { timeout: 3000 });
  });

  it('act click sends correct action', async () => {
    let sentBody: any = null;

    mockFetch((_input, init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/act')) {
        sentBody = JSON.parse(init?.body as string);
        return jsonResponse({ url: '', title: '', elements: [] });
      }
      if (url.includes('/computer-use/screenshot')) {
        return jsonResponse({ image: '', url: '', title: '' });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.act({ action: 'click', element: 5 });
    });

    await waitFor(() => {
      expect(sentBody).toBeTruthy();
    });
    expect(sentBody.action).toBe('click');
    expect(sentBody.element).toBe(5);
  });

  it('act type sends correct action', async () => {
    let sentBody: any = null;

    mockFetch((_input, init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/act')) {
        sentBody = JSON.parse(init?.body as string);
        return jsonResponse({ url: '', title: '', elements: [] });
      }
      if (url.includes('/computer-use/screenshot')) {
        return jsonResponse({ image: '', url: '', title: '' });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.act({ action: 'type', element: 3, text: 'hello', submit: true });
    });

    await waitFor(() => {
      expect(sentBody).toBeTruthy();
    });
    expect(sentBody.action).toBe('type');
    expect(sentBody.text).toBe('hello');
    expect(sentBody.submit).toBe(true);
  });

  it('act scroll sends correct action', async () => {
    let sentBody: any = null;

    mockFetch((_input, init) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/computer-use/enable')) {
        return jsonResponse({ enabled: true, sandboxId: 'sb-1', cdpPort: 19222 });
      }
      if (url.includes('/computer-use/act')) {
        sentBody = JSON.parse(init?.body as string);
        return jsonResponse({ url: '', title: '', elements: [] });
      }
      if (url.includes('/computer-use/screenshot')) {
        return jsonResponse({ image: '', url: '', title: '' });
      }
      return jsonResponse({});
    });

    const { result } = renderHook(() => useComputerUse());

    await act(async () => {
      await result.current.enableComputerUse('sb-1');
    });

    await waitFor(() => {
      expect(result.current.enabled).toBe(true);
    });

    await act(async () => {
      await result.current.act({ action: 'scroll', direction: 'up', amount: 500 });
    });

    await waitFor(() => {
      expect(sentBody).toBeTruthy();
    });
    expect(sentBody.action).toBe('scroll');
    expect(sentBody.direction).toBe('up');
    expect(sentBody.amount).toBe(500);
  });
});
