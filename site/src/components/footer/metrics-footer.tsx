// Metrics footer — small status strip across the bottom of the chat panel.
// Polls the active thread's state from the Agent Protocol harness and shows:
//   ● running | idle | finished | error
//   org name
//   last updated

import { useCallback, useEffect, useState, type FC } from "react";
import { getThread } from "../../../server/agent-protocol";

interface MetricsFooterProps {
  threadId: string | null;
}

interface ThreadMeta {
  thread_id: string;
  agent_id?: string;
  status?: string;
}

function timeAgo(iso?: string): string {
  if (!iso) return "";
  const t = new Date(iso).getTime();
  if (!t) return "";
  const diff = Date.now() - t;
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`;
  return `${Math.floor(diff / 86_400_000)}d`;
}

const STATUS_COLOR: Record<string, string> = {
  running: "text-emerald-500",
  idle: "text-muted-foreground/50",
  finished: "text-muted-foreground/50",
  error: "text-destructive",
};

export const MetricsFooter: FC<MetricsFooterProps> = ({ threadId }) => {
  const [meta, setMeta] = useState<ThreadMeta | null>(null);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!threadId) {
      setMeta(null);
      return;
    }
    try {
      const snap = await getThread(threadId);
      setMeta(snap);
      setUpdatedAt(new Date().toISOString());
    } catch {
      // best-effort
    }
  }, [threadId]);

  useEffect(() => {
    void refresh();
    const id = setInterval(refresh, 3000);
    return () => clearInterval(id);
  }, [refresh]);

  if (!threadId || !meta) {
    return (
      <footer className="flex h-6 items-center border-t border-border bg-muted/30 px-3 text-[10px] uppercase tracking-wider text-muted-foreground/60">
        no thread
      </footer>
    );
  }

  const status = meta.status ?? "idle";

  return (
    <footer className="flex h-6 items-center gap-4 border-t border-border bg-muted/30 px-3 text-[10px] text-muted-foreground">
      <span className="flex items-center gap-1">
        <span className={(STATUS_COLOR[status] ?? "") + " text-base leading-none"}>●</span>
        <span>{status}</span>
      </span>
      {meta.agent_id && (
        <span className="font-mono">{meta.agent_id}</span>
      )}
      {updatedAt && <span className="ml-auto">updated {timeAgo(updatedAt)} ago</span>}
    </footer>
  );
};
