import React, { useState, useCallback, useEffect, useMemo, useRef } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useAui } from "@assistant-ui/react-ink";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";
import * as fs from "fs";
import * as path from "path";

const IGNORE_DIRS = new Set(["node_modules", ".git", ".next", "dist", ".cache", "__pycache__", ".venv", "venv", ".svelte-kit", ".output", "target", "build"]);

interface DirEntry {
  name: string;
  fullPath: string;
  isDir: boolean;
}

function listDir(dir: string, prefix: string): DirEntry[] {
  try {
    const entries = fs.readdirSync(dir);
    const result: DirEntry[] = [];
    for (const e of entries) {
      if (IGNORE_DIRS.has(e)) continue;
      if (e.startsWith(".") && e !== "." && e !== "..") continue;
      const full = path.join(dir, e);
      let isDir = false;
      try { isDir = fs.statSync(full).isDirectory(); } catch {}
      result.push({ name: e, fullPath: full, isDir });
    }
    result.sort((a, b) => {
      if (a.isDir && !b.isDir) return -1;
      if (!a.isDir && b.isDir) return 1;
      return a.name.localeCompare(b.name);
    });
    return result;
  } catch {
    return [];
  }
}

function fuzzyMatch(text: string, query: string): boolean {
  const lower = text.toLowerCase();
  const q = query.toLowerCase();
  let qi = 0;
  for (let ti = 0; ti < lower.length && qi < q.length; ti++) {
    if (lower[ti] === q[qi]) qi++;
  }
  return qi === q.length;
}

export function FilePicker() {
  const show = usePuxStore((s) => s.showFilePicker);
  const projectPath = usePuxStore((s) => s.activeProjectPath);
  const close = usePuxStore((s) => s.closeFilePicker);
  const aui = useAui();
  const colors = useColors();

  const [filter, setFilter] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [rootDir, setRootDir] = useState("");

  // Set root directory when opened
  useEffect(() => {
    if (show) {
      const dir = projectPath || process.cwd();
      setRootDir(dir);
      setFilter("");
      setExpanded(new Set());
      setSelectedIdx(0);
    }
  }, [show, projectPath]);

  // Build flat list of visible entries
  const visibleEntries = useMemo(() => {
    if (!rootDir) return [];
    const result: Array<{ display: string; fullPath: string; isDir: boolean; depth: number }> = [];

    function walk(dir: string, depth: number) {
      const entries = listDir(dir, "");
      for (const e of entries) {
        const display = e.fullPath.replace(rootDir, "") || "/";
        const match = !filter || fuzzyMatch(display, filter);

        if (match || e.isDir) {
          result.push({ display, fullPath: e.fullPath, isDir: e.isDir, depth });
        }

        if (e.isDir && (expanded.has(e.fullPath) || (filter && match))) {
          walk(e.fullPath, depth + 1);
        }
      }
    }

    walk(rootDir, 0);

    // If filter is active, also search deeper
    if (filter) {
      const allFiles = result.filter((r) => r.isDir || fuzzyMatch(r.display, filter));
      return allFiles;
    }

    return result;
  }, [rootDir, expanded, filter]);

  const filtered = useMemo(() => {
    if (!filter) return visibleEntries;
    return visibleEntries.filter((e) => fuzzyMatch(e.display, filter));
  }, [visibleEntries, filter]);

  useInput(
    useCallback(
      (input: string, key: any) => {
        if (!show) return;

        if (key.escape) { close(); return; }

        if (key.return) {
          const entry = filtered[selectedIdx];
          if (!entry) return;
          if (entry.isDir) {
            // Toggle expand
            setExpanded((prev) => {
              const next = new Set(prev);
              if (next.has(entry.fullPath)) next.delete(entry.fullPath);
              else next.add(entry.fullPath);
              return next;
            });
          } else {
            // Insert file path and close
            const relative = entry.display.startsWith("/") ? "." + entry.display : "./" + entry.display;
            const composer = aui.composer();
            const currentText = composer.getState().text || "";
            const newText = currentText ? `${currentText} ${relative}` : relative;
            composer.setText(newText);
            close();
          }
          return;
        }

        if (key.upArrow) {
          setSelectedIdx((prev) => Math.max(0, prev - 1));
          return;
        }
        if (key.downArrow) {
          setSelectedIdx((prev) => Math.min(filtered.length - 1, prev + 1));
          return;
        }

        if (key.backspace || key.delete) {
          setFilter((prev) => prev.slice(0, -1));
          setSelectedIdx(0);
          return;
        }

        // Printable chars → filter
        if (input && !key.ctrl && !key.meta && !key.escape && !key.return && !key.upArrow && !key.downArrow && !key.tab) {
          setFilter((prev) => prev + input);
          setSelectedIdx(0);
          return;
        }
      },
      [show, filtered, selectedIdx, close, aui],
    ),
  );

  if (!show) return null;

  return (
    <Box flexDirection="column" flexGrow={1}>
      <Box backgroundColor="cyan" paddingX={1}>
        <Text bold> {symbols.dot} File Picker</Text>
        {rootDir && <Text dimColor>  {rootDir}</Text>}
      </Box>

      {/* Filter */}
      <Box marginTop={1} paddingX={1}>
        <Text color={colors.brand} bold>{">"} </Text>
        <Text>{filter}</Text>
        <Text dimColor>{"\u2588"}</Text>
      </Box>

      {/* File list */}
      <Box flexGrow={1} flexDirection="column" paddingX={1} marginTop={1}>
        {filtered.length === 0 && (
          <Text dimColor>No files match your filter.</Text>
        )}
        {filtered.slice(0, 20).map((entry, i) => {
          const selected = i === selectedIdx;
          const prefix = entry.depth > 0 ? "  ".repeat(entry.depth) : "";
          return (
            <Box key={entry.fullPath}>
              <Text backgroundColor={selected ? "gray" : undefined} bold={selected}>
                {" "}{selected ? symbols.arrow : " "}{" "}
              </Text>
              <Text
                color={entry.isDir ? "cyan" : selected ? colors.brand : undefined}
                bold={entry.isDir || selected}
              >
                {prefix}{entry.isDir ? symbols.dot + " " : ""}{entry.display.split("/").pop()}
              </Text>
              {entry.isDir && (
                <Text dimColor color="cyan">/</Text>
              )}
            </Box>
          );
        })}
        {filtered.length > 20 && (
          <Text dimColor>... and {filtered.length - 20} more</Text>
        )}
      </Box>

      {/* Footer */}
      <Box paddingX={1}>
        <Text dimColor>
          Type to filter · ↑↓ navigate · Enter open/select · Esc close
        </Text>
      </Box>
    </Box>
  );
}
