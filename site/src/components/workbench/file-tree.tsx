// File tree for the editor panel. Lazily expands directories via the BFF
// /api/files/list endpoint. Hidden directories (.git, node_modules, .pux)
// are filtered server-side.

import { useState, useCallback, type FC } from "react";
import {
  ChevronRightIcon,
  ChevronDownIcon,
  FileIcon,
  FolderIcon,
  FolderOpenIcon,
  FilePlusIcon,
  FolderPlusIcon,
  Trash2Icon,
} from "lucide-react";
import { cn } from "@/lib/utils";

export interface DirEntry {
  name: string;
  path: string;
  type: "file" | "dir" | "symlink";
  size: number;
  mtime: string;
}

type Node = DirEntry & { children?: Node[]; loaded?: boolean };

async function apiList(path: string): Promise<DirEntry[]> {
  const url = `/api/files/list?path=${encodeURIComponent(path)}`;
  const r = await fetch(url);
  if (!r.ok) throw new Error(`list ${path}: ${r.status}`);
  return (await r.json()) as DirEntry[];
}

export const FileTree: FC<{
  rootLabel: string;
  selectedPath: string | null;
  onSelect: (path: string) => void;
}> = ({ rootLabel, selectedPath, onSelect }) => {
  const [root, setRoot] = useState<Node | null>(null);
  const [error, setError] = useState<string | null>(null);

  const ensureRoot = useCallback(async () => {
    if (root) return;
    try {
      const entries = await apiList("");
      setRoot({
        name: rootLabel,
        path: "",
        type: "dir",
        size: 0,
        mtime: "",
        children: entries.map((e) => ({ ...e })),
        loaded: true,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [root, rootLabel]);

  // Trigger load on first mount.
  if (!root && !error) void ensureRoot();

  const loadChildren = useCallback(async (node: Node) => {
    if (node.type !== "dir" || node.loaded) return;
    try {
      const entries = await apiList(node.path);
      node.children = entries.map((e) => ({ ...e }));
      node.loaded = true;
      setRoot({ ...root! }); // shallow trigger
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [root]);

  const toggle = useCallback(async (node: Node) => {
    if (node.type !== "dir") return;
    if (node.loaded) {
      node.children = node.children; // keep loaded, UI toggles via expand state
    } else {
      await loadChildren(node);
    }
    setExpanded((s) => {
      const next = new Set(s);
      if (next.has(node.path)) next.delete(node.path);
      else next.add(node.path);
      return next;
    });
  }, [loadChildren]);

  const [expanded, setExpanded] = useState<Set<string>>(new Set([""]));

  const createChild = useCallback(async (parent: Node, type: "file" | "dir") => {
    const name = window.prompt(type === "file" ? "New file name" : "New folder name", "new.txt");
    if (!name) return;
    const path = parent.path ? `${parent.path}/${name}` : name;
    try {
      const r = await fetch("/api/files/create", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ path, type }),
      });
      if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
      // Reload parent
      const entries = await apiList(parent.path);
      parent.children = entries.map((e) => ({ ...e }));
      parent.loaded = true;
      setExpanded((s) => new Set(s).add(parent.path));
      setRoot({ ...root! });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [root]);

  const removeNode = useCallback(async (node: Node) => {
    if (!window.confirm(`Delete ${node.path}? (moved to .pux/trash)`)) return;
    try {
      const r = await fetch(`/api/files/delete?path=${encodeURIComponent(node.path)}`, {
        method: "DELETE",
      });
      if (!r.ok) throw new Error(`${r.status}: ${await r.text()}`);
      // Reload the entire tree to keep things simple.
      setRoot(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  if (error) {
    return (
      <div className="p-2 text-xs text-destructive">
        file tree error: {error}
      </div>
    );
  }

  if (!root) {
    return <div className="p-2 text-xs text-muted-foreground">loading…</div>;
  }

  const renderNode = (node: Node, depth: number): React.ReactNode => {
    const indent = { paddingLeft: `${depth * 12 + 8}px` };
    if (node.type === "dir") {
      const isOpen = expanded.has(node.path);
      const isRoot = node.path === "";
      return (
        <div key={node.path || "__root__"}>
          <div
            className="group/node flex items-center gap-1 py-0.5 pr-1 text-xs hover:bg-accent/50"
            style={indent}
          >
            <button
              type="button"
              onClick={() => toggle(node)}
              className="flex flex-1 items-center gap-1 text-left"
            >
              {isOpen ? (
                <ChevronDownIcon className="size-3 shrink-0 text-muted-foreground" />
              ) : (
                <ChevronRightIcon className="size-3 shrink-0 text-muted-foreground" />
              )}
              {isOpen ? (
                <FolderOpenIcon className="size-3.5 shrink-0 text-amber-500" />
              ) : (
                <FolderIcon className="size-3.5 shrink-0 text-amber-500" />
              )}
              <span className={cn("truncate", isRoot && "font-semibold")}>
                {node.name}
              </span>
            </button>
            {!isRoot && (
              <button
                type="button"
                onClick={() => removeNode(node)}
                className="hidden size-5 items-center justify-center rounded text-muted-foreground hover:text-destructive group-hover/node:flex"
                title="Delete"
              >
                <Trash2Icon className="size-3" />
              </button>
            )}
            <button
              type="button"
              onClick={() => createChild(node, "file")}
              className="hidden size-5 items-center justify-center rounded text-muted-foreground hover:text-foreground group-hover/node:flex"
              title="New file"
            >
              <FilePlusIcon className="size-3" />
            </button>
            <button
              type="button"
              onClick={() => createChild(node, "dir")}
              className="hidden size-5 items-center justify-center rounded text-muted-foreground hover:text-foreground group-hover/node:flex"
              title="New folder"
            >
              <FolderPlusIcon className="size-3" />
            </button>
          </div>
          {isOpen && node.children && (
            <div>
              {node.children.map((c) => renderNode(c, depth + 1))}
            </div>
          )}
        </div>
      );
    }
    const isSelected = selectedPath === node.path;
    return (
      <div
        key={node.path}
        className="group/node flex items-center gap-1 py-0.5 pr-1 text-xs"
        style={indent}
      >
        <button
          type="button"
          onClick={() => onSelect(node.path)}
          className={cn(
            "flex flex-1 items-center gap-1 rounded px-1 text-left",
            isSelected && "bg-accent",
          )}
        >
          <FileIcon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{node.name}</span>
        </button>
        <button
          type="button"
          onClick={() => removeNode(node)}
          className="hidden size-5 items-center justify-center rounded text-muted-foreground hover:text-destructive group-hover/node:flex"
          title="Delete"
        >
          <Trash2Icon className="size-3" />
        </button>
      </div>
    );
  };

  return <div className="py-1">{renderNode(root, 0)}</div>;
};
