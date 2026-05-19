import React, { useMemo } from "react";
import { Box, Text } from "ink";
import { useColors } from "../theme.js";
import * as fs from "fs";
import * as path from "path";

interface PathAutocompleteProps {
  text: string;
  cwd: string;
  selectedIdx: number;
}

export interface Completion {
  fullPath: string;
  display: string;
  isDir: boolean;
}

const PATH_RE = /(?:^|\s)((?:\.\.?\/|[~/])(?:[^\s"'`|;]*)?)$/;

function expandHome(p: string): string {
  if (p.startsWith("~")) {
    const home = process.env.HOME || "/home/ubuntu";
    return home + p.slice(1);
  }
  return p;
}

export function getCompletions(input: string, cwd: string): Completion[] {
  const match = input.match(PATH_RE);
  if (!match || !cwd) return [];

  const rawPrefix = match[1];
  const absPrefix = path.resolve(cwd, expandHome(rawPrefix));
  const dir = absPrefix.endsWith("/") ? absPrefix : path.dirname(absPrefix);
  const partial = absPrefix.endsWith("/") ? "" : path.basename(absPrefix);

  let entries: string[];
  try {
    entries = fs.readdirSync(dir);
  } catch {
    return [];
  }

  const filtered = entries.filter((e) => e.startsWith(partial));
  const sorted = filtered.sort((a, b) => {
    let aIsDir = false, bIsDir = false;
    try {
      aIsDir = fs.statSync(path.join(dir, a)).isDirectory();
      bIsDir = fs.statSync(path.join(dir, b)).isDirectory();
    } catch {}
    if (aIsDir && !bIsDir) return -1;
    if (!aIsDir && bIsDir) return 1;
    return a.localeCompare(b);
  });

  const baseDir = rawPrefix.endsWith("/") ? rawPrefix : path.dirname(rawPrefix);
  const prefixShow = baseDir === "." ? "" : baseDir.endsWith("/") ? baseDir : baseDir + "/";

  return sorted.slice(0, 20).map((e) => {
    const full = path.join(dir, e);
    let isDir = false;
    try { isDir = fs.statSync(full).isDirectory(); } catch {}
    return {
      fullPath: full,
      display: prefixShow + e + (isDir ? "/" : ""),
      isDir,
    };
  });
}

export function PathAutocomplete({ text, cwd, selectedIdx }: PathAutocompleteProps) {
  const colors = useColors();

  const completions = useMemo(() => {
    if (!text || !cwd) return [];
    return getCompletions(text, cwd);
  }, [text, cwd]);

  if (completions.length === 0) return null;

  const MAX_VISIBLE = 5;
  const startIdx = Math.max(0, Math.min(selectedIdx - MAX_VISIBLE + 1, completions.length - MAX_VISIBLE));
  const visible = completions.slice(startIdx, startIdx + MAX_VISIBLE);

  return (
    <Box flexDirection="column" paddingX={1}>
      {visible.map((c, i) => {
        const globalIdx = startIdx + i;
        return (
          <Text key={c.fullPath}>
            {globalIdx === selectedIdx ? (
              <Text bold color={colors.brand}>{c.display}</Text>
            ) : (
              <Text>{c.display}</Text>
            )}
            {" "}
            {c.isDir ? (
              <Text color="cyan">dir</Text>
            ) : (
              <Text dimColor color="gray">
                {path.extname(c.display) || "file"}
              </Text>
            )}
          </Text>
        );
      })}
      <Text dimColor color="gray"> Tab autocomplete path</Text>
    </Box>
  );
}
