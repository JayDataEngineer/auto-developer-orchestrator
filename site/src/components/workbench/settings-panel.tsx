// Settings panel — per-thread model picker + thinking-level control.
// Calls the BFF routes that wrap piClient:
//   GET    /api/pi/models
//   GET    /api/pi/threads/:id
//   POST   /api/pi/threads/:id/model       { provider, modelId }
//   POST   /api/pi/threads/:id/thinking    { level }

import { useCallback, useEffect, useState, type FC } from "react";
import { Loader2Icon, SaveIcon } from "lucide-react";
import { Button } from "@/components/ui/button";

interface PiModelInfo {
  provider: string;
  modelId: string;
  displayName?: string;
  contextWindow?: number;
  priority?: number;
}

interface ThreadConfig {
  provider?: string;
  modelId?: string;
  thinkingLevel?: string;
}

interface SettingsPanelProps {
  threadId: string | null;
}

const THINKING_LEVELS = ["minimal", "low", "medium", "high"] as const;

function groupByProvider(models: PiModelInfo[]): Record<string, PiModelInfo[]> {
  const out: Record<string, PiModelInfo[]> = {};
  for (const m of models) {
    (out[m.provider] ??= []).push(m);
  }
  // Deterministic order: provider asc, then modelId asc.
  for (const k of Object.keys(out)) {
    out[k].sort((a, b) => a.modelId.localeCompare(b.modelId));
  }
  return out;
}

function modelKey(c?: { provider?: string; modelId?: string }): string {
  return c?.provider && c?.modelId ? `${c.provider}/${c.modelId}` : "";
}

export const SettingsPanel: FC<SettingsPanelProps> = ({ threadId }) => {
  const [models, setModels] = useState<PiModelInfo[]>([]);
  const [cfg, setCfg] = useState<ThreadConfig>({});
  const [draftModel, setDraftModel] = useState<string>("");
  const [draftThinking, setDraftThinking] = useState<string>("");
  const [loadingModels, setLoadingModels] = useState(true);
  const [savingModel, setSavingModel] = useState(false);
  const [savingThinking, setSavingThinking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load available models once.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const r = await fetch("/api/pi/models");
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const data = (await r.json()) as PiModelInfo[];
        if (cancelled) return;
        setModels(data);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      } finally {
        if (!cancelled) setLoadingModels(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Load thread config whenever threadId changes.
  const refreshCfg = useCallback(async () => {
    if (!threadId) {
      setCfg({});
      setDraftModel("");
      setDraftThinking("");
      return;
    }
    try {
      const r = await fetch(`/api/pi/threads/${threadId}`);
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const snap = (await r.json()) as { metadata?: { config?: ThreadConfig } };
      const next = snap.metadata?.config ?? {};
      setCfg(next);
      setDraftModel(modelKey(next));
      setDraftThinking(next.thinkingLevel ?? "low");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [threadId]);

  useEffect(() => {
    void refreshCfg();
  }, [refreshCfg]);

  const saveModel = useCallback(async () => {
    if (!threadId) return;
    const [provider, modelId] = draftModel.split("/");
    if (!provider || !modelId) return;
    if (draftModel === modelKey(cfg)) return;
    setSavingModel(true);
    setError(null);
    try {
      const r = await fetch(`/api/pi/threads/${threadId}/model`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ provider, modelId }),
      });
      if (!r.ok) throw new Error(`HTTP ${r.status}: ${await r.text()}`);
      setCfg((prev) => ({ ...prev, provider, modelId }));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSavingModel(false);
    }
  }, [threadId, draftModel, cfg]);

  const saveThinking = useCallback(async () => {
    if (!threadId) return;
    if (draftThinking === (cfg.thinkingLevel ?? "")) return;
    setSavingThinking(true);
    setError(null);
    try {
      const r = await fetch(`/api/pi/threads/${threadId}/thinking`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ level: draftThinking }),
      });
      if (!r.ok) throw new Error(`HTTP ${r.status}: ${await r.text()}`);
      setCfg((prev) => ({ ...prev, thinkingLevel: draftThinking }));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSavingThinking(false);
    }
  }, [threadId, draftThinking, cfg]);

  const grouped = groupByProvider(models);
  const providersSorted = Object.keys(grouped).sort();

  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <div className="flex h-9 items-center gap-2 border-b border-border px-3 text-xs">
        <span className="font-semibold uppercase tracking-wider text-muted-foreground">
          settings
        </span>
        {!threadId && (
          <span className="text-muted-foreground">— no thread selected</span>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-auto p-4 text-xs">
        {error && (
          <div className="mb-3 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1 text-destructive">
            {error}
          </div>
        )}

        {!threadId ? (
          <div className="text-muted-foreground">Select a thread first.</div>
        ) : loadingModels ? (
          <div className="flex items-center gap-2 text-muted-foreground">
            <Loader2Icon className="size-3 animate-spin" /> loading models…
          </div>
        ) : (
          <div className="space-y-6">
            <section>
              <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                model
              </div>
              <div className="flex items-center gap-2">
                <select
                  value={draftModel}
                  onChange={(e) => setDraftModel(e.target.value)}
                  className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1.5 font-mono text-xs outline-none focus:border-ring"
                >
                  <option value="" disabled>
                    pick a model…
                  </option>
                  {providersSorted.map((provider) => (
                    <optgroup key={provider} label={provider}>
                      {grouped[provider].map((m) => (
                        <option key={`${m.provider}/${m.modelId}`} value={`${m.provider}/${m.modelId}`}>
                          {m.displayName ?? m.modelId}
                          {m.contextWindow ? ` · ${Math.round(m.contextWindow / 1000)}k ctx` : ""}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </select>
                <Button
                  size="sm"
                  onClick={saveModel}
                  disabled={savingModel || draftModel === modelKey(cfg) || !draftModel}
                  className="h-8 gap-1"
                >
                  {savingModel ? (
                    <Loader2Icon className="size-3 animate-spin" />
                  ) : (
                    <SaveIcon className="size-3" />
                  )}
                  apply
                </Button>
              </div>
              <div className="mt-1 text-[11px] text-muted-foreground/70">
                current: <code className="font-mono">{modelKey(cfg) || "—"}</code>
              </div>
            </section>

            <section>
              <div className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                thinking level
              </div>
              <div className="flex items-center gap-2">
                <div className="flex overflow-hidden rounded-md border border-border">
                  {THINKING_LEVELS.map((lvl) => (
                    <button
                      key={lvl}
                      type="button"
                      onClick={() => setDraftThinking(lvl)}
                      className={
                        "px-3 py-1.5 capitalize transition-colors " +
                        (draftThinking === lvl
                          ? "bg-primary text-primary-foreground"
                          : "bg-background hover:bg-accent")
                      }
                    >
                      {lvl}
                    </button>
                  ))}
                </div>
                <Button
                  size="sm"
                  onClick={saveThinking}
                  disabled={
                    savingThinking ||
                    draftThinking === (cfg.thinkingLevel ?? "")
                  }
                  className="h-8 gap-1"
                >
                  {savingThinking ? (
                    <Loader2Icon className="size-3 animate-spin" />
                  ) : (
                    <SaveIcon className="size-3" />
                  )}
                  apply
                </Button>
              </div>
              <div className="mt-1 text-[11px] text-muted-foreground/70">
                current: <code className="font-mono">{cfg.thinkingLevel ?? "—"}</code>
              </div>
            </section>
          </div>
        )}
      </div>
    </div>
  );
};
