// VNC viewer: iframe pointing at the BFF-served noVNC page. The BFF reverse-
// proxies both the static assets (/api/sandbox/vnc/**) and the WS endpoint
// (/api/sandbox/vnc/websockify), so the iframe is same-origin with the rest
// of the site — no port juggling for remote operators.
//
// noVNC's vnc.html handles mouse/keyboard itself; we just frame it. The
// header strip exposes "open in new tab" + reload.

import { useCallback, useEffect, useRef, useState, type FC } from "react";
import { ExternalLinkIcon, RefreshCwIcon, Loader2Icon, AlertTriangleIcon } from "lucide-react";
import { Button } from "@/components/ui/button";

interface SandboxInfo {
  id: string;
  running: boolean;
  vncUrl: string | null;
}

export const VncViewer: FC = () => {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [info, setInfo] = useState<SandboxInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reloadKey, setReloadKey] = useState(0);

  const refresh = useCallback(async () => {
    try {
      const r = await fetch("/api/sandbox");
      if (!r.ok) throw new Error(`status ${r.status}`);
      const body = (await r.json()) as SandboxInfo | null;
      setInfo(body);
      setError(body?.running ? null : "sandbox not running");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const id = setInterval(refresh, 5000);
    return () => clearInterval(id);
  }, [refresh]);

  const vncUrl = info?.vncUrl ?? null;

  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <div className="flex h-9 items-center gap-2 border-b border-border px-3 text-xs">
        <span className="font-semibold uppercase tracking-wider text-muted-foreground">
          vnc
        </span>
        <div className="ml-2 text-muted-foreground">
          {info
            ? info.running
              ? <span><span className="text-emerald-500">●</span> connected to <code className="font-mono">{info.id}</code></span>
              : <span><span className="text-amber-500">●</span> sandbox not running</span>
            : "checking sandbox…"}
        </div>
        <div className="ml-auto flex items-center gap-1">
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setLoading(true);
              setReloadKey((k) => k + 1);
            }}
            disabled={!info?.running}
            className="h-7 gap-1 text-xs"
            title="Reload VNC view"
          >
            <RefreshCwIcon className="size-3" />
            reload
          </Button>
          {vncUrl && (
            <a
              href={vncUrl}
              target="_blank"
              rel="noreferrer"
              className="flex h-7 items-center gap-1 rounded-md border px-2 text-xs hover:bg-accent"
              title="Open noVNC in a new tab"
            >
              <ExternalLinkIcon className="size-3" />
              pop out
            </a>
          )}
        </div>
      </div>

      <div className="relative min-h-0 flex-1 bg-black">
        {error && (
          <div className="absolute inset-0 flex items-center justify-center p-4">
            <div className="max-w-sm rounded-md border border-destructive/40 bg-destructive/10 p-4 text-center text-xs text-destructive">
              <AlertTriangleIcon className="mx-auto mb-2 size-5" />
              <div className="font-medium">VNC unavailable</div>
              <div className="mt-1 text-destructive/80">{error}</div>
            </div>
          </div>
        )}

        {loading && !error && (
          <div className="absolute inset-0 flex items-center justify-center text-xs text-muted-foreground">
            <Loader2Icon className="mr-2 size-3 animate-spin" />
            connecting…
          </div>
        )}

        {vncUrl && !error && (
          <iframe
            key={reloadKey}
            ref={iframeRef}
            src={vncUrl}
            title="pux sandbox desktop"
            className="absolute inset-0 size-full border-0"
            onLoad={() => setLoading(false)}
            allow="clipboard-read; clipboard-write"
          />
        )}
      </div>
    </div>
  );
};
