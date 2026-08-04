// Thread-state fetcher — routes through the BFF (single ingress point).
// The BFF proxies to Aegra's langgraph-api on :9988.

const SITE_URL = import.meta.env.VITE_PUX_SITE_URL ?? "http://127.0.0.1:3001";

export interface ThreadState {
  thread_id: string;
  agent_id: string;
  status: string;
  values?: Record<string, unknown>;
  next?: string[];
}

async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const url = `${SITE_URL}${path}`;
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
  return api("GET", `/api/thread/${encodeURIComponent(threadId)}`);
}
