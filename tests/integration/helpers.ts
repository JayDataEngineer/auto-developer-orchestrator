/**
 * Integration Test Helpers
 *
 * Shared utilities for real-backend integration tests.
 * No mocking — these talk to the actual Go backend + LiteLLM.
 */

import type { Page } from '@playwright/test';

/** Frontend base URL (through Vite proxy) */
export const BASE_URL = 'http://localhost:5174';
/** Go backend base URL — paths already include /api/ prefix */
export const API = 'http://localhost:3847';

/** Default project used for testing (must exist on disk) */
export const TEST_PROJECT = 'test-repo';
export const TEST_AGENT = 'default';
export const TEST_MODEL = 'fast'; // Gemini 2 through LiteLLM — fast + cheap

// ─── API helpers ───────────────────────────────────────────────────

export async function apiGet<T = any>(path: string): Promise<{ status: number; data: T }> {
  const res = await fetch(`${API}${path}`);
  const data = await res.json().catch(() => null);
  return { status: res.status, data: data as T };
}

export async function apiPost<T = any>(path: string, body?: any, timeoutMs: number = 15_000): Promise<{ status: number; data: T }> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  let res: Response;
  try {
    res = await fetch(`${API}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
      signal: controller.signal,
    });
  } catch (err: any) {
    return { status: 504, data: { error: err.message } as any };
  } finally {
    clearTimeout(timer);
  }
  const data = await res.json().catch(() => null);
  return { status: res.status, data: data as T };
}

export async function apiPut<T = any>(path: string, body: any): Promise<{ status: number; data: T }> {
  const res = await fetch(`${API}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => null);
  return { status: res.status, data: data as T };
}

export async function apiDelete<T = any>(path: string): Promise<{ status: number; data: T }> {
  const res = await fetch(`${API}${path}`, { method: 'DELETE' });
  const data = await res.json().catch(() => null);
  return { status: res.status, data: data as T };
}

// ─── SSE helpers ───────────────────────────────────────────────────

export interface SSEEvent {
  type: string;
  data: any;
}

/**
 * Send a prompt and collect ALL SSE events from the real backend.
 * Returns once the stream ends (agent_end or error or timeout).
 */
export async function streamPrompt(
  message: string,
  project: string = TEST_PROJECT,
  agentId: string = TEST_AGENT,
  timeoutMs: number = 30_000,
): Promise<{ events: SSEEvent[]; texts: string; status: number }> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  const res = await fetch(`${API}/api/pi/prompt`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, project, agentId }),
    signal: controller.signal,
  });

  const events: SSEEvent[] = [];
  let texts = '';

  if (!res.ok || !res.body) {
    clearTimeout(timer);
    return { events, texts, status: res.status };
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';

      let eventType = '';
      for (const line of lines) {
        if (line.startsWith('event: ')) {
          eventType = line.slice(7).trim();
        } else if (line.startsWith('data: ') && eventType) {
          const data = JSON.parse(line.slice(6));
          events.push({ type: eventType, data });
          if (eventType === 'text_delta' && data.text) {
            texts += data.text;
          }
          eventType = '';
        }
      }

      // Stop after agent_end or error
      if (events.some(e => e.type === 'agent_end' || e.type === 'error')) {
        break;
      }
    }
  } finally {
    clearTimeout(timer);
    reader.cancel().catch(() => {});
  }

  return { events, texts, status: res.status };
}

// ─── Frontend helpers ──────────────────────────────────────────────

/** Navigate to the app and wait for it to be interactive */
export async function gotoApp(page: Page) {
  await page.goto('/');
  await page.waitForLoadState('networkidle');
  // Wait for the top bar to render (proves backend responded)
  await page.waitForSelector('.h-10.border-b', { timeout: 10_000 });
}

/** Wait for agent streaming to complete */
export async function waitForStreamEnd(page: Page, timeoutMs = 45_000) {
  await page.waitForFunction(
    () => {
      const indicators = document.querySelectorAll('[data-testid="streaming-indicator"]');
      return indicators.length === 0;
    },
    { timeout: timeoutMs },
  ).catch(() => {}); // Don't fail if indicator is gone already
}
