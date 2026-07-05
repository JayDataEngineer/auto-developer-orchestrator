// Agent Protocol client — browser-side HTTP wrapper over the harness REST API.
// Uses VITE_PUX_HARNESS_URL for the backend URL.

const HARNES_URL = import.meta.env.VITE_PUX_HARNESS_URL ?? "http://127.0.0.1:9988";

export interface ThreadState {
  thread_id: string;
  agent_id: string;
  status: string;
  values?: Record<string, unknown>;
  next?: string[];
}

async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const url = `${HARNES_URL}${path}`;
  const opts: RequestInit = {
    method,
    headers: { "content-type": "application/json" },
  };
  if (body !== undefined) opts.body = JSON.stringify(body);
  const r = await fetch(url, opts);
  if (!r.ok) {
    const text = await r.text().catch(() => "");
    throw new Error(`${method} ${path} → ${r.status}: ${text}`);
  }
  return r.json() as Promise<T>;
}

export async function getThread(threadId: string): Promise<ThreadState> {
  return api("GET", `/threads/${encodeURIComponent(threadId)}`);
}
