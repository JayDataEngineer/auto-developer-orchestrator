// Sandbox panel: shows the global pux sandbox state, exposes start/stop +
// screenshot. Pairs with the VNC tab (Phase 5) for live desktop view.

import { useCallback, useEffect, useState, type FC } from "react";
import { PlayIcon, CircleStopIcon, CameraIcon, Loader2Icon, ExternalLinkIcon } from "lucide-react";
import { Button } from "@/components/ui/button";

interface SandboxInfo {
  id: string;
  containerId: string;
  image: string;
  status: string;
  running: boolean;
  createdAt: string;
  ports: { host: number; container: number; protocol: string }[];
  vncUrl: string | null;
}

export const SandboxPanel: FC = () => {
  const [info, setInfo] = useState<SandboxInfo | null>(null);
  const [loading, setLoading] = useState<"idle" | "create" | "delete" | "shot">("idle");
  const [error, setError] = useState<string | null>(null);
  const [shotUrl, setShotUrl] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const r = await fetch("/api/sandbox");
      if (!r.ok) throw new Error(`${r.status}`);
      setInfo((await r.json()) as SandboxInfo | null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const id = setInterval(refresh, 5000);
    return () => clearInterval(id);
  }, [refresh]);

  const create = useCallback(async () => {
    setLoading("create");
    setError(null);
    try {
      const r = await fetch("/api/sandbox", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      });
      if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading("idle");
    }
  }, [refresh]);

  const remove = useCallback(async () => {
    if (!confirm("Stop + remove the sandbox container?")) return;
    setLoading("delete");
    setError(null);
    try {
      const r = await fetch("/api/sandbox", { method: "DELETE" });
      if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
      setInfo(null);
      setShotUrl(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading("idle");
    }
  }, []);

  const snap = useCallback(async () => {
    setLoading("shot");
    setError(null);
    try {
      const r = await fetch("/api/sandbox/screenshot");
      if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
      const blob = await r.blob();
      setShotUrl((prev) => {
        if (prev) URL.revokeObjectURL(prev);
        return URL.createObjectURL(blob);
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading("idle");
    }
  }, []);

  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <div className="flex h-9 items-center gap-2 border-b border-border px-3 text-xs">
        <span className="font-semibold uppercase tracking-wider text-muted-foreground">
          sandbox
        </span>
        <div className="ml-2 text-muted-foreground">
          {info
            ? (
              <span>
                <span className={info.running ? "text-emerald-500" : "text-amber-500"}>
                  ●
                </span>{" "}
                <code className="font-mono">{info.containerId}</code> {info.status}
              </span>
            )
            : "no sandbox"}
        </div>
        <div className="ml-auto flex items-center gap-1">
          <Button
            size="sm"
            variant="outline"
            onClick={snap}
            disabled={!info?.running || loading !== "idle"}
            className="h-7 gap-1 text-xs"
          >
            {loading === "shot" ? (
              <Loader2Icon className="size-3 animate-spin" />
            ) : (
              <CameraIcon className="size-3" />
            )}
            shot
          </Button>
          {info?.vncUrl && (
            <a
              href={info.vncUrl}
              target="_blank"
              rel="noreferrer"
              className="flex h-7 items-center gap-1 rounded-md border px-2 text-xs hover:bg-accent"
            >
              <ExternalLinkIcon className="size-3" />
              vnc
            </a>
          )}
          {info ? (
            <Button
              size="sm"
              variant="destructive"
              onClick={remove}
              disabled={loading !== "idle"}
              className="h-7 gap-1 text-xs"
            >
              {loading === "delete" ? (
                <Loader2Icon className="size-3 animate-spin" />
              ) : (
                <CircleStopIcon className="size-3" />
              )}
              stop
            </Button>
          ) : (
            <Button
              size="sm"
              onClick={create}
              disabled={loading !== "idle"}
              className="h-7 gap-1 text-xs"
            >
              {loading === "create" ? (
                <Loader2Icon className="size-3 animate-spin" />
              ) : (
                <PlayIcon className="size-3" />
              )}
              start
            </Button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-3 text-xs">
        {error && (
          <div className="mb-2 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1 text-destructive">
            {error}
          </div>
        )}

        {shotUrl && (
          <div className="mb-3">
            <div className="mb-1 text-muted-foreground">latest screenshot</div>
            <img src={shotUrl} alt="sandbox desktop" className="w-full rounded-md border border-border" />
          </div>
        )}

        {info ? (
          <dl className="grid grid-cols-[100px_1fr] gap-x-2 gap-y-1 font-mono">
            <dt className="text-muted-foreground">image</dt>
            <dd className="break-all">{info.image}</dd>
            <dt className="text-muted-foreground">status</dt>
            <dd>{info.status}</dd>
            <dt className="text-muted-foreground">created</dt>
            <dd>{info.createdAt}</dd>
            <dt className="text-muted-foreground">ports</dt>
            <dd>
              {info.ports.length === 0
                ? "—"
                : info.ports.map(p => `${p.host}→${p.container}/${p.protocol}`).join(", ")}
            </dd>
          </dl>
        ) : (
          <div className="text-muted-foreground">
            Press <kbd className="rounded border px-1">start</kbd> to launch the
            global pux sandbox. It binds the workspace at{" "}
            <code className="font-mono">/workspace</code> and exposes noVNC +
            Chrome debug ports.
          </div>
        )}
      </div>
    </div>
  );
};
