// Sidebar thread list — fetches /api/pi/threads, lets user switch the active
// thread, create new ones, delete old ones. Switching threads is hoisted to
// the App via setActiveThreadId; that re-mounts PiRuntimeProvider with the
// new threadId.

import { useCallback, useEffect, useRef, useState } from "react";
import {
  Plus,
  Trash2,
  PencilIcon,
  MessageSquare,
  Loader2,
  CheckIcon,
  XIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

interface PiThreadMetadata {
  id: string;
  title?: string;
  status: "idle" | "running" | "failed";
  messageCount?: number;
  updatedAt?: string;
  createdAt?: string;
}

interface ThreadListProps {
  activeThreadId: string | null;
  onSelect: (id: string) => void;
  onCreated: (id: string) => void;
}

function relativeTime(iso?: string): string {
  if (!iso) return "";
  const then = new Date(iso).getTime();
  if (!then) return "";
  const diff = Date.now() - then;
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / 86_400_000)}d ago`;
}

export function ThreadList({
  activeThreadId,
  onSelect,
  onCreated,
}: ThreadListProps) {
  const [threads, setThreads] = useState<PiThreadMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const resp = await fetch("/api/pi/threads");
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      const data = (await resp.json()) as PiThreadMetadata[];
      // newest first; updatedAt may be missing on brand-new threads
      data.sort((a, b) => {
        const at = a.updatedAt ?? a.createdAt ?? "";
        const bt = b.updatedAt ?? b.createdAt ?? "";
        return bt.localeCompare(at);
      });
      setThreads(data);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 10_000);
    return () => clearInterval(id);
  }, [refresh]);

  const createThread = async () => {
    setCreating(true);
    try {
      const resp = await fetch("/api/pi/threads", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({}),
      });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      // POST returns PiThreadSnapshot — { metadata: { id, ... }, ... }.
      // Empty threads don't appear in listThreads (pi-mono behavior), so we
      // switch to the new thread immediately by ID without waiting for refresh.
      const snap = (await resp.json()) as { metadata: PiThreadMetadata };
      onCreated(snap.metadata.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setCreating(false);
    }
  };

  const deleteThread = async (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      const resp = await fetch(`/api/pi/threads/${id}`, { method: "DELETE" });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      await refresh();
      if (activeThreadId === id) {
        // pick the next available thread or null
        const next = threads.find((t) => t.id !== id);
        onSelect(next?.id ?? "");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const renameThread = useCallback(
    async (id: string, title: string) => {
      const trimmed = title.trim();
      if (!trimmed) return;
      // Optimistically patch local state so the UI feels instant.
      setThreads((prev) =>
        prev.map((t) => (t.id === id ? { ...t, title: trimmed } : t)),
      );
      try {
        const resp = await fetch(`/api/pi/threads/${id}`, {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ title: trimmed }),
        });
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
        void refresh();
      }
    },
    [refresh],
  );

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between px-3 py-2">
        <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          threads
        </div>
        <Button
          size="icon"
          variant="ghost"
          className="size-7"
          onClick={createThread}
          disabled={creating}
          title="New thread"
        >
          {creating ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <Plus className="size-3.5" />
          )}
        </Button>
      </div>

      {error && (
        <div className="mx-3 mb-2 rounded border border-destructive/40 bg-destructive/10 px-2 py-1 text-[11px] text-destructive">
          {error}
        </div>
      )}

      <div className="flex-1 overflow-y-auto px-1 pb-2">
        {loading ? (
          <div className="space-y-1 px-2 pt-1">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : threads.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-muted-foreground">
            No threads yet. Click + to start.
          </div>
        ) : (
          threads.map((t) => (
            <ThreadRow
              key={t.id}
              thread={t}
              isActive={t.id === activeThreadId}
              onSelect={onSelect}
              onDelete={deleteThread}
              onRename={renameThread}
            />
          ))
        )}
      </div>
    </div>
  );
}

interface ThreadRowProps {
  thread: PiThreadMetadata;
  isActive: boolean;
  onSelect: (id: string) => void;
  onDelete: (id: string, e: React.MouseEvent) => void;
  onRename: (id: string, title: string) => Promise<void>;
}

function ThreadRow({ thread, isActive, onSelect, onDelete, onRename }: ThreadRowProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(thread.title ?? "");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  const commit = useCallback(async () => {
    const next = draft.trim();
    setEditing(false);
    if (next && next !== (thread.title ?? "")) {
      await onRename(thread.id, next);
    }
  }, [draft, thread.id, thread.title, onRename]);

  const cancel = useCallback(() => {
    setDraft(thread.title ?? "");
    setEditing(false);
  }, [thread.title]);

  if (editing) {
    return (
      <div className="group mb-0.5 flex w-full items-center gap-1 rounded-md bg-accent px-2 py-1.5">
        <MessageSquare className="size-3.5 shrink-0 opacity-70" />
        <input
          ref={inputRef}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(e) => {
            if (e.key === "Enter") void commit();
            else if (e.key === "Escape") cancel();
          }}
          className="min-w-0 flex-1 rounded-sm border border-border bg-background px-1 py-0.5 text-[13px] outline-none"
        />
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            void commit();
          }}
          className="text-emerald-600 hover:text-emerald-500"
          title="Save"
        >
          <CheckIcon className="size-3" />
        </button>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            cancel();
          }}
          className="text-muted-foreground hover:text-foreground"
          title="Cancel"
        >
          <XIcon className="size-3" />
        </button>
      </div>
    );
  }

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => onSelect(thread.id)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect(thread.id);
        }
      }}
      className={cn(
        "group mb-0.5 flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors",
        isActive
          ? "bg-accent text-accent-foreground"
          : "hover:bg-accent/50 text-muted-foreground hover:text-foreground",
      )}
    >
      <MessageSquare className="size-3.5 shrink-0 opacity-70" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px]">
          {thread.title?.trim() || (
            <span className="italic text-muted-foreground/70">untitled</span>
          )}
        </div>
        <div className="text-[10px] text-muted-foreground/70">
          {thread.messageCount != null && `${thread.messageCount} msg · `}
          {relativeTime(thread.updatedAt ?? thread.createdAt)}
          {thread.status === "running" && (
            <span className="ml-1 text-emerald-500">●</span>
          )}
        </div>
      </div>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          console.log("[rename] clicked, entering edit mode", thread.id);
          setDraft(thread.title ?? "");
          setEditing(true);
        }}
        className="opacity-0 transition-opacity group-hover:opacity-100 hover:text-foreground"
        title="Rename"
      >
        <PencilIcon className="size-3" />
      </button>
      <button
        type="button"
        onClick={(e) => void onDelete(thread.id, e)}
        className="opacity-0 transition-opacity group-hover:opacity-100 hover:text-destructive"
        title="Delete"
      >
        <Trash2 className="size-3" />
      </button>
    </div>
  );
}

