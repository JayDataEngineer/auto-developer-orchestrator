// Thread history sidebar — uses CopilotKit's native useThreads() hook.
//
// CopilotKit's CopilotThreadsDrawer UI is license-gated (Intelligence Cloud),
// but the useThreads() data hook is NOT — it fetches through the CopilotKit
// runtime, which our BFF proxies to Aegra's langgraph-api /threads (see
// server/copilotkit.ts handleThreadListProxy). This gives us the native data
// layer + native styling without a Cloud subscription.
//
// Aegra stores threads with empty names (the runtime never names them), so
// titles are derived lazily from each thread's first user message via the
// existing /api/thread/:id state route — cached, with a 60s negative-cache.

import { useEffect, useRef, useState } from "react";
import { useThreads } from "@copilotkit/react-core/v2";
import {
  MessageSquareIcon,
  MessageSquarePlusIcon,
  RefreshCwIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface ThreadState {
  values?: { messages?: unknown };
}

interface Props {
  activeThreadId: string | null;
  onSelect: (threadId: string) => void;
  onNew: () => void;
}

const POLL_MS = 5000;
const TITLE_FETCH_BUDGET = 10;
const NEGATIVE_CACHE_MS = 60_000;

function relativeTime(iso: string | undefined): string {
  if (!iso) return "";
  const diff = Date.now() - new Date(iso).getTime();
  const min = Math.floor(diff / 60_000);
  if (min < 1) return "now";
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h`;
  const day = Math.floor(hr / 24);
  if (day < 7) return `${day}d`;
  return new Date(iso).toLocaleDateString();
}

function firstUserMessage(messages: unknown): string | null {
  if (!Array.isArray(messages)) return null;
  for (const m of messages) {
    if (m && typeof m === "object" && "type" in m && (m as { type?: unknown }).type === "user") {
      const content = (m as { content?: unknown }).content;
      if (typeof content === "string" && content.trim()) return content.trim();
      if (Array.isArray(content)) {
        for (const c of content) {
          if (c && typeof c === "object" && "type" in c && (c as { type?: unknown }).type === "text") {
            const text = (c as { text?: unknown }).text;
            if (typeof text === "string" && text.trim()) return text.trim();
          }
        }
      }
    }
  }
  return null;
}

export function ThreadHistory({ activeThreadId, onSelect, onNew }: Props) {
  const { threads, isLoading, error, refetchThreads } = useThreads({
    agentId: "general",
    limit: 50,
  });

  // Poll for live updates (matches the previous 5s cadence).
  useEffect(() => {
    const t = setInterval(refetchThreads, POLL_MS);
    return () => clearInterval(t);
  }, [refetchThreads]);

  // Derive titles lazily for unnamed threads (cached, budgeted per poll).
  const titles = useRef<Map<string, string>>(new Map());
  const fetchedAt = useRef<Map<string, number>>(new Map());
  const [titlesTick, setTitlesTick] = useState(0);
  void titlesTick;

  const unnamed = threads.filter(
    (t) =>
      !t.name &&
      !titles.current.has(t.id) &&
      (fetchedAt.current.get(t.id) ?? 0) + NEGATIVE_CACHE_MS < Date.now(),
  );

  useEffect(() => {
    if (unnamed.length === 0) return;
    let cancelled = false;
    (async () => {
      let fetched = 0;
      for (const t of unnamed) {
        if (fetched >= TITLE_FETCH_BUDGET) break;
        fetched += 1;
        fetchedAt.current.set(t.id, Date.now());
        try {
          const r = await fetch(`/api/thread/${encodeURIComponent(t.id)}`);
          if (!r.ok) continue;
          const st = (await r.json()) as ThreadState;
          const title = firstUserMessage(st.values?.messages);
          if (title) titles.current.set(t.id, title);
        } catch {
          // leave it for the next poll cycle
        }
      }
      if (!cancelled) setTitlesTick((n) => n + 1);
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [unnamed.map((t) => t.id).join(",")]);

  const sorted = [...threads].sort((a, b) =>
    (b.updatedAt ?? "").localeCompare(a.updatedAt ?? ""),
  );

  return (
    <aside className="flex h-full flex-col bg-[#0d1320]">
      <div className="flex h-11 shrink-0 items-center justify-between border-b border-border px-3">
        <span className="text-sm font-semibold tracking-tight text-foreground">
          Threads
        </span>
        <div className="flex items-center gap-1">
          <button
            onClick={() => refetchThreads()}
            title="Refresh"
            className="flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <RefreshCwIcon className="size-4" />
          </button>
          <button
            onClick={onNew}
            title="New chat"
            className="flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <MessageSquarePlusIcon className="size-4" />
          </button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
        {isLoading && threads.length === 0 && (
          <div className="px-3 py-2 text-sm text-muted-foreground">Loading…</div>
        )}
        {error && (
          <div className="px-3 py-2 text-sm text-red-400">
            History: {error instanceof Error ? error.message : String(error)}
          </div>
        )}
        {!isLoading && !error && threads.length === 0 && (
          <div className="px-3 py-2 text-sm text-muted-foreground">
            No conversations yet.
          </div>
        )}

        {sorted.map((t) => {
          const title = t.name || titles.current.get(t.id) || "New chat";
          return (
            <button
              key={t.id}
              onClick={() => onSelect(t.id)}
              className={cn(
                "flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm hover:bg-accent",
                activeThreadId === t.id && "bg-accent",
              )}
              title={t.id}
            >
              <MessageSquareIcon className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate font-medium">{title}</span>
              <span className="shrink-0 text-xs text-muted-foreground/60">
                {relativeTime(t.updatedAt)}
              </span>
            </button>
          );
        })}
      </div>
    </aside>
  );
}
