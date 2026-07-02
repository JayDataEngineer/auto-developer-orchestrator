// Editor panel: file tree on the left, Monaco on the right. Reads/writes
// through the /api/files/** BFF.

import { useCallback, useEffect, useRef, useState, type FC } from "react";
import Editor, { type OnMount } from "@monaco-editor/react";
import { SaveIcon, Loader2Icon } from "lucide-react";
import { cn } from "@/lib/utils";
import { FileTree } from "./file-tree";
import { Button } from "@/components/ui/button";

type Loaded = {
  path: string;
  language: string;
  content: string;
  original: string;
};

const EDITOR_OPTIONS = {
  minimap: { enabled: false },
  fontSize: 12,
  fontLigatures: true,
  fontFamily:
    "'JetBrains Mono', 'Fira Code', 'SF Mono', Menlo, Consolas, monospace",
  scrollBeyondLastLine: false,
  renderWhitespace: "selection",
  tabSize: 2,
  automaticLayout: true,
  wordWrap: "on" as const,
  smoothScrolling: true,
  cursorBlinking: "smooth" as const,
  cursorSmoothCaretAnimation: "on" as const,
} as const;

export const EditorPanel: FC = () => {
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Keep a ref to the latest onSave so Monaco's keybinding (registered once
  // on mount) can call the freshest version without re-registering.
  const onSaveRef = useRef<(() => void) | null>(null);

  const onSelect = useCallback(async (path: string) => {
    setLoading(true);
    setError(null);
    try {
      const r = await fetch(`/api/files/read?path=${encodeURIComponent(path)}`);
      if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
      const data = (await r.json()) as {
        path: string;
        language: string;
        content: string;
      };
      setLoaded({
        path: data.path,
        language: data.language,
        content: data.content,
        original: data.content,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  const onChange = useCallback((value: string | undefined) => {
    setLoaded((cur) => (cur ? { ...cur, content: value ?? "" } : cur));
  }, []);

  const onSave = useCallback(async () => {
    if (!loaded) return;
    setSaving(true);
    setError(null);
    try {
      const r = await fetch("/api/files/write", {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ path: loaded.path, content: loaded.content }),
      });
      if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
      setLoaded({ ...loaded, original: loaded.content });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }, [loaded]);

  onSaveRef.current = onSave;

  // Monaco swallows Ctrl+S before window sees it — register inside the editor.
  const onMount: OnMount = useCallback((_editor, monaco) => {
    _editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      onSaveRef.current?.();
    });
  }, []);

  const dirty = loaded ? loaded.content !== loaded.original : false;

  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <div className="flex h-9 items-center gap-2 border-b border-border px-3 text-xs">
        <span className="font-semibold uppercase tracking-wider text-muted-foreground">
          editor
        </span>
        <div className="ml-2 flex items-center gap-2">
          {loaded && (
            <>
              <code className="font-mono text-foreground">{loaded.path}</code>
              {dirty && (
                <span className="size-1.5 rounded-full bg-amber-500" title="unsaved" />
              )}
            </>
          )}
          {!loaded && !loading && (
            <span className="text-muted-foreground">no file selected</span>
          )}
          {loading && (
            <span className="flex items-center gap-1 text-muted-foreground">
              <Loader2Icon className="size-3 animate-spin" /> loading…
            </span>
          )}
        </div>
        <div className="ml-auto flex items-center gap-1">
          <Button
            size="sm"
            variant={dirty ? "default" : "outline"}
            onClick={onSave}
            disabled={!loaded || !dirty || saving}
            className="h-7 gap-1 text-xs"
          >
            {saving ? (
              <Loader2Icon className="size-3 animate-spin" />
            ) : (
              <SaveIcon className="size-3" />
            )}
            save
          </Button>
        </div>
      </div>

      <div className="flex min-h-0 flex-1">
        <aside className="w-56 shrink-0 overflow-y-auto border-r border-border bg-muted/20">
          <FileTree
            rootLabel="workspace"
            selectedPath={loaded?.path ?? null}
            onSelect={onSelect}
          />
        </aside>

        <div className="relative min-w-0 flex-1">
          {error && (
            <div className="absolute inset-x-0 top-0 z-10 border-b border-destructive/40 bg-destructive/10 px-3 py-1.5 text-xs text-destructive">
              {error}
            </div>
          )}
          {loaded ? (
            <Editor
              height="100%"
              theme="vs-dark"
              language={loaded.language}
              value={loaded.content}
              onChange={onChange}
              onMount={onMount}
              options={EDITOR_OPTIONS}
              loading={
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2Icon className="size-3 animate-spin" /> loading editor…
                </div>
              }
            />
          ) : (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
              select a file from the tree
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
