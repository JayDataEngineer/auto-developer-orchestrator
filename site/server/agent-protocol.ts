// Agent Protocol client — thin HTTP wrapper over the harness REST API.
// The harness runs at PUX_API_URL (default http://127.0.0.1:9988) and
// exposes the standard Agent Protocol endpoints:
//   POST /agents/search       → agent descriptors
//   POST /threads             → create a thread
//   POST /threads/search      → list threads
//   GET  /threads/:id         → thread state
//   DELETE /threads/:id       → delete a thread
//   GET  /threads/:id/history → checkpoint history
//   POST /threads/:id/runs    → background run
//   GET  /threads/:id/runs    → list runs
//   POST /runs/wait           → blocking run (create+run+return)
//   GET  /runs/:id/wait       → wait for a background run
//   POST /runs/:id/cancel     → cancel a run

const DEFAULT_HARNESS_URL = "http://127.0.0.1:9988";

export interface AgentDescriptor {
  agent_id: string;
  name: string;
  description: string;
  metadata?: Record<string, unknown>;
}

export interface ThreadState {
  thread_id: string;
  agent_id: string;
  status: string;
  values?: Record<string, unknown>;
  next?: string[];
}

export interface ThreadRecord {
  thread_id: string;
  agent_id: string;
  metadata?: Record<string, unknown>;
  created_at?: string;
}

export interface RunRecord {
  run_id: string;
  thread_id: string;
  agent_id: string;
  status: string;
  output?: string;
  error?: string | null;
  alive?: boolean;
  started_at?: string;
}

export interface HistoryEntry {
  checkpoint_id?: string;
  parent_checkpoint_id?: string;
  next?: string[];
  values?: Record<string, unknown>;
}

// Message shape as returned by the harness _jsonable() helper.
export interface AgentMessage {
  role: string;
  content: string | Array<{ type: string; text?: string }>;
  tool_calls?: unknown;
  id?: string;
  tool_call_id?: string;
  name?: string;
}

function harnessUrl(): string {
  return (typeof process !== "undefined" && process.env?.PUX_HARNESS_URL)
    || DEFAULT_HARNESS_URL;
}

async function api<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const url = `${harnessUrl()}${path}`;
  const opts: RequestInit = {
    method,
    headers: { "content-type": "application/json" },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }
  const r = await fetch(url, opts);
  if (!r.ok) {
    const text = await r.text().catch(() => "");
    throw new Error(`${method} ${path} → ${r.status}: ${text}`);
  }
  return r.json() as Promise<T>;
}

// --- agents ---

export async function listAgents(): Promise<AgentDescriptor[]> {
  return api("POST", "/agents/search");
}

export async function getAgent(agentId: string): Promise<AgentDescriptor> {
  return api("GET", `/agents/${encodeURIComponent(agentId)}`);
}

// --- threads ---

export async function createThread(
  agentId: string,
  metadata?: Record<string, unknown>,
): Promise<ThreadState> {
  return api("POST", "/threads", { agent_id: agentId, metadata: metadata ?? {} });
}

export async function listThreads(
  agentId?: string,
): Promise<ThreadRecord[]> {
  return api("POST", "/threads/search", { agent_id: agentId ?? null });
}

export async function getThread(threadId: string): Promise<ThreadState> {
  return api("GET", `/threads/${encodeURIComponent(threadId)}`);
}

export async function deleteThread(threadId: string): Promise<void> {
  await api("DELETE", `/threads/${encodeURIComponent(threadId)}`);
}

export async function getThreadHistory(
  threadId: string,
): Promise<HistoryEntry[]> {
  return api("GET", `/threads/${encodeURIComponent(threadId)}/history`);
}

// --- runs ---

export async function createRun(
  threadId: string,
  input: string | { messages: Array<{ role: string; content: string }> },
  recursionLimit?: number,
): Promise<RunRecord> {
  return api("POST", `/threads/${encodeURIComponent(threadId)}/runs`, {
    input,
    recursion_limit: recursionLimit ?? 60,
  });
}

export async function listRuns(threadId: string): Promise<RunRecord[]> {
  return api("GET", `/threads/${encodeURIComponent(threadId)}/runs`);
}

export async function waitForRun(runId: string): Promise<RunRecord> {
  return api("GET", `/runs/${encodeURIComponent(runId)}/wait`);
}

export async function cancelRun(runId: string): Promise<RunRecord> {
  return api("POST", `/runs/${encodeURIComponent(runId)}/cancel`);
}

export async function ephemeralRun(
  agentId: string,
  input: string | { messages: Array<{ role: string; content: string }> },
  recursionLimit?: number,
): Promise<RunRecord & { thread_id: string }> {
  return api("POST", "/runs/wait", {
    agent_id: agentId,
    input,
    recursion_limit: recursionLimit ?? 60,
  });
}

// --- message extraction helpers ---

/** Extract text content from an AgentMessage (handles multimodal blocks). */
export function messageText(msg: AgentMessage): string {
  if (typeof msg.content === "string") return msg.content;
  if (Array.isArray(msg.content)) {
    return msg.content
      .map((b) => ("text" in b ? b.text ?? "" : ""))
      .join("\n");
  }
  return "";
}

/** Get messages from a thread state's values. */
export function threadMessages(values?: Record<string, unknown>): AgentMessage[] {
  if (!values) return [];
  const msgs = values.messages;
  if (!Array.isArray(msgs)) return [];
  return msgs as AgentMessage[];
}
