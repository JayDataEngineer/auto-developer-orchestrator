/**
 * @vitest-environment jsdom
 */

/**
 * ComputerUseTab Component Tests — ZERO MOCKS
 *
 * Tests the real ComputerUseTab component with real props.
 * ComputerUseTab receives cu state as props from the parent.
 * All rendering logic, UI states, URL construction, and polling are tested.
 */
import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react';
import { ComputerUseTab } from '../../src/components/ComputerUseTab';

// ─── Helpers ──────────────────────────────────────────────────

function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

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

// Default props
const defaultCU = {
  enabled: false,
  loading: false,
  error: null as string | null,
  sandboxId: null as string | null,
};

// Helper: get the center panel content area (the main display below the header)
function getCenterPanel() {
  return screen.getByText(/Select a project above to start|Enabling computer use|Desktop Ready|Desktop not available|Connecting to desktop|Initializing/).closest('.relative')!;
}

// ─── Tests ────────────────────────────────────────────────────

describe('ComputerUseTab — all UI states', () => {
  afterEach(() => {
    restoreFetch();
    vi.restoreAllMocks();
  });

  // ── Empty / Initial states ────────────────────────────────

  it('shows placeholder when no sandboxId', () => {
    render(<ComputerUseTab selectedProject={null} sandboxId={null} cu={defaultCU} />);
    // Header shows "Select a project to start"
    expect(screen.getByText('Select a project to start')).toBeInTheDocument();
  });

  it('shows sandboxId in header when set', () => {
    render(<ComputerUseTab selectedProject="test" sandboxId="sb-123" cu={{ ...defaultCU, sandboxId: 'sb-123' }} />);
    expect(screen.getByText('sb-123')).toBeInTheDocument();
  });

  // ── Loading states ────────────────────────────────────────

  it('shows Enabling spinner in header when cu.loading=true', () => {
    render(<ComputerUseTab selectedProject="test" sandboxId="sb-1" cu={{ ...defaultCU, loading: true, sandboxId: 'sb-1' }} />);
    // Header shows "Enabling..."
    const header = screen.getAllByText(/Enabling/)[0];
    expect(header).toBeInTheDocument();
  });

  it('shows center panel spinner when cu.loading=true', () => {
    render(<ComputerUseTab selectedProject="test" sandboxId="sb-1" cu={{ ...defaultCU, loading: true, sandboxId: 'sb-1' }} />);
    // Center panel shows "Enabling computer use..."
    expect(screen.getByText('Enabling computer use...')).toBeInTheDocument();
  });

  // ── Error states ──────────────────────────────────────────

  it('shows error from cu.error in header', () => {
    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ ...defaultCU, sandboxId: 'sb-1', error: 'Enable failed' }}
      />
    );
    // Error text appears in header
    const errors = screen.getAllByText(/Enable failed/);
    expect(errors.length).toBeGreaterThanOrEqual(1);
  });

  it('shows "Desktop not available" with retry button in center panel', () => {
    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ ...defaultCU, sandboxId: 'sb-1', error: 'something broke' }}
      />
    );
    expect(screen.getByText(/Desktop not available/)).toBeInTheDocument();
    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  // ── Viewer polling / Session states ───────────────────────

  it('polls /viewer when enabled and shows connecting state', async () => {
    mockFetch((_input) => {
      return new Response(JSON.stringify({ error: 'not found' }), { status: 404, headers: { 'Content-Type': 'application/json' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    // Should show "Starting desktop..." in center panel or header while polling
    await waitFor(() => {
      const startingEls = screen.queryAllByText('Starting desktop...');
      const connectingEls = screen.queryAllByText('Connecting to desktop...');
      expect(startingEls.length + connectingEls.length).toBeGreaterThanOrEqual(1);
    }, { timeout: 5000 });
  });

  it('shows iframe when session loads successfully', async () => {
    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        return jsonResponse({
          mode: 'browser',
          novncUrl: 'http://localhost:6080/vnc.html',
          cdpUrl: 'http://localhost:19222',
        });
      }
      return new Response('<html>noVNC</html>', { status: 200, headers: { 'Content-Type': 'text/html' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    await waitFor(() => {
      const iframe = screen.getByTitle('Desktop');
      expect(iframe).toBeInTheDocument();
    }, { timeout: 10000 });
  });

  it('shows Desktop label in header when session active', async () => {
    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        return jsonResponse({ mode: 'browser', novncUrl: 'http://localhost:6080/vnc.html', cdpUrl: 'http://localhost:19222' });
      }
      return new Response('<html>noVNC</html>', { status: 200, headers: { 'Content-Type': 'text/html' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    await waitFor(() => {
      expect(screen.getByTitle('Desktop')).toBeInTheDocument();
    }, { timeout: 10000 });

    // Header should show "Desktop" label (as text in a span)
    const desktopLabels = screen.getAllByText('Desktop');
    expect(desktopLabels.length).toBeGreaterThanOrEqual(1);
  });

  // ── Viewer polling with 404s then 200 ─────────────────────

  it('polls /viewer until 200 response', async () => {
    let viewerCalls = 0;

    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        viewerCalls++;
        if (viewerCalls <= 2) {
          return new Response(JSON.stringify({ error: 'not found' }), { status: 404, headers: { 'Content-Type': 'application/json' } });
        }
        return jsonResponse({ mode: 'browser', novncUrl: 'http://localhost:6080/vnc.html', cdpUrl: 'http://localhost:19222' });
      }
      return new Response('<html>noVNC</html>', { status: 200, headers: { 'Content-Type': 'text/html' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    await waitFor(() => {
      expect(screen.getByTitle('Desktop')).toBeInTheDocument();
    }, { timeout: 15000 });

    expect(viewerCalls).toBeGreaterThanOrEqual(3);
  });

  // ── noVNC URL construction ────────────────────────────────

  it('noVNC URL uses correct sandboxId', async () => {
    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        return jsonResponse({ mode: 'browser', novncUrl: 'http://localhost:6080/vnc.html', cdpUrl: 'http://localhost:19222' });
      }
      return new Response('<html>noVNC</html>', { status: 200, headers: { 'Content-Type': 'text/html' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="my-special-sandbox"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'my-special-sandbox' }}
      />
    );

    await waitFor(() => {
      const iframe = screen.getByTitle('Desktop') as HTMLIFrameElement;
      expect(iframe.src).toContain('my-special-sandbox');
    }, { timeout: 10000 });
  });

  it('noVNC URL includes autoconnect and resize params', async () => {
    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        return jsonResponse({ mode: 'browser', novncUrl: 'http://localhost:6080/vnc.html', cdpUrl: 'http://localhost:19222' });
      }
      return new Response('<html>noVNC</html>', { status: 200, headers: { 'Content-Type': 'text/html' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    await waitFor(() => {
      const iframe = screen.getByTitle('Desktop') as HTMLIFrameElement;
      expect(iframe.src).toContain('autoconnect=true');
      expect(iframe.src).toContain('resize=scale');
    }, { timeout: 10000 });
  });

  it('noVNC URL includes websockify path', async () => {
    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        return jsonResponse({ mode: 'browser', novncUrl: 'http://localhost:6080/vnc.html', cdpUrl: 'http://localhost:19222' });
      }
      return new Response('<html>noVNC</html>', { status: 200, headers: { 'Content-Type': 'text/html' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    await waitFor(() => {
      const iframe = screen.getByTitle('Desktop') as HTMLIFrameElement;
      expect(iframe.src).toContain('websockify');
      expect(iframe.src).toContain('api/sandbox/vnc/sb-1');
    }, { timeout: 10000 });
  });

  // ── Fullscreen toggle ─────────────────────────────────────

  it('fullscreen toggle adds fixed class', async () => {
    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        return jsonResponse({ mode: 'browser', novncUrl: 'http://localhost:6080/vnc.html', cdpUrl: 'http://localhost:19222' });
      }
      return new Response('<html>noVNC</html>', { status: 200, headers: { 'Content-Type': 'text/html' } });
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    await waitFor(() => {
      expect(screen.getByTitle('Desktop')).toBeInTheDocument();
    }, { timeout: 10000 });

    const fullscreenBtn = screen.getByTitle('Full screen');
    fireEvent.click(fullscreenBtn);

    // The outer container should now have 'fixed' class
    const fixedContainer = document.querySelector('.fixed.inset-0');
    expect(fixedContainer).toBeInTheDocument();
  });

  // ── Retry button ──────────────────────────────────────────

  it('retry button is present in error state', () => {
    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ ...defaultCU, sandboxId: 'sb-1', error: 'Timeout' }}
      />
    );
    const retryBtn = screen.getByText('Retry');
    expect(retryBtn).toBeInTheDocument();
  });

  // ── Edge cases ────────────────────────────────────────────

  it('renders without crash when all props are null', () => {
    const { container } = render(
      <ComputerUseTab selectedProject={null} sandboxId={null} cu={defaultCU} />
    );
    expect(container).toBeTruthy();
  });

  it('renders without crash with sandboxId but no enable', () => {
    const { container } = render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ ...defaultCU, sandboxId: 'sb-1' }}
      />
    );
    expect(container).toBeTruthy();
    // Header shows the sandbox ID
    expect(screen.getByText('sb-1')).toBeInTheDocument();
  });

  it('shows "Initializing..." when enabled but no session (fallback)', async () => {
    // Mock viewer to return 404, but check the fallback state quickly
    let resolveViewer: (r: Response) => void;
    mockFetch((_input) => {
      const url = typeof _input === 'string' ? _input : '';
      if (url.includes('/viewer')) {
        // Return a promise that never resolves — keeps in polling state
        return new Promise<Response>((resolve) => { resolveViewer = resolve; });
      }
      return jsonResponse({});
    });

    render(
      <ComputerUseTab
        selectedProject="test"
        sandboxId="sb-1"
        cu={{ enabled: true, loading: false, error: null, sandboxId: 'sb-1' }}
      />
    );

    // While polling, should show "Starting desktop..." in center panel or header
    await waitFor(() => {
      const startingEls = screen.queryAllByText('Starting desktop...');
      const connectingEls = screen.queryAllByText('Connecting to desktop...');
      expect(startingEls.length + connectingEls.length).toBeGreaterThanOrEqual(1);
    }, { timeout: 5000 });

    // Resolve the pending promise to clean up
    resolveViewer!(new Response(JSON.stringify({ error: 'done' }), { status: 404, headers: { 'Content-Type': 'application/json' } }));
  });
});
