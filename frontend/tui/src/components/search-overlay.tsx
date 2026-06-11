import React, { useState, useCallback, useMemo } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useAuiState } from "@assistant-ui/react-ink";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

interface Match {
  messageIdx: number;
  role: string;
  text: string;
  snippet: string;
  spanStart: number;
  spanEnd: number;
}

function collectText(messages: readonly any[]): string[] {
  const texts: string[] = [];
  for (const msg of messages) {
    if (msg.role === "user") {
      if (msg.content?.[0]?.text) {
        texts.push(msg.content[0].text);
      }
    } else if (msg.role === "assistant") {
      const parts = msg.content || msg.parts || [];
      for (const part of parts) {
        if (part.type === "text" && part.text) {
          texts.push(part.text);
        }
      }
    }
  }
  return texts;
}

function findMatches(texts: string[], query: string): Match[] {
  if (!query) return [];
  const q = query.toLowerCase();
  const results: Match[] = [];

  for (let mi = 0; mi < texts.length; mi++) {
    const lower = texts[mi].toLowerCase();
    let idx = 0;
    while (true) {
      idx = lower.indexOf(q, idx);
      if (idx === -1) break;

      // Build snippet: 40 chars before and after
      const start = Math.max(0, idx - 40);
      const end = Math.min(texts[mi].length, idx + query.length + 40);
      let snippet = texts[mi].slice(start, end);
      if (start > 0) snippet = "..." + snippet;
      if (end < texts[mi].length) snippet = snippet + "...";

      results.push({
        messageIdx: mi,
        role: mi < texts.length ? "unknown" : "unknown",
        text: texts[mi],
        snippet,
        spanStart: idx,
        spanEnd: idx + query.length,
      });
      idx += 1;
    }
  }

  return results;
}

export function SearchOverlay() {
  const show = usePuxStore((s) => s.showSearchOverlay);
  const close = usePuxStore((s) => s.closeSearchOverlay);
  const messages = useAuiState((s) => s.thread.messages);
  const colors = useColors();

  const [query, setQuery] = useState("");
  const [matchIdx, setMatchIdx] = useState(0);

  const texts = useMemo(() => collectText(messages || []), [messages]);

  const matches = useMemo(() => findMatches(texts, query), [texts, query]);

  // Reset match index when query changes
  const prevQueryLen = React.useRef(0);
  if (query.length !== prevQueryLen.current) {
    prevQueryLen.current = query.length;
    if (matchIdx >= matches.length) setMatchIdx(Math.max(0, matches.length - 1));
    if (matchIdx < 0) setMatchIdx(0);
  }

  useInput(
    useCallback(
      (input: string, key: any) => {
        if (!show) return;

        if (key.escape) { close(); setQuery(""); return; }

        if (key.return || key.tab) {
          // Enter/Tab with no query + matches = move to next match
          if (matches.length > 0) {
            setMatchIdx((prev) => (prev + 1) % matches.length);
          }
          return;
        }

        if (key.upArrow) {
          if (matches.length > 0) {
            setMatchIdx((prev) => (prev <= 0 ? matches.length - 1 : prev - 1));
          }
          return;
        }
        if (key.downArrow) {
          if (matches.length > 0) {
            setMatchIdx((prev) => (prev + 1) % matches.length);
          }
          return;
        }

        if (key.backspace || key.delete) {
          setQuery((prev) => prev.slice(0, -1));
          setMatchIdx(0);
          return;
        }

        if (input && !key.ctrl && !key.meta && !key.escape && !key.return && !key.upArrow && !key.downArrow && !key.tab) {
          setQuery((prev) => prev + input);
          setMatchIdx(0);
          return;
        }
      },
      [show, close, matches.length],
    ),
  );

  if (!show) return null;

  const currentMatch = matches[matchIdx];

  return (
    <Box flexDirection="column" flexGrow={1}>
      <Box backgroundColor="cyan" paddingX={1}>
        <Text bold> {symbols.dot} Search</Text>
      </Box>

      {/* Search input */}
      <Box marginTop={1} paddingX={1}>
        <Text color={colors.brand} bold>{">"} </Text>
        <Text>{query}</Text>
        <Text dimColor>{"\u2588"}</Text>
      </Box>

      {/* Match count */}
      {query && (
        <Box paddingX={1} marginTop={0}>
          <Text dimColor>
            {matches.length} match{matches.length !== 1 ? "es" : ""}
            {matches.length > 0 ? ` (${matchIdx + 1}/${matches.length})` : ""}
          </Text>
        </Box>
      )}

      {/* Results */}
      <Box flexGrow={1} flexDirection="column" paddingX={1} marginTop={1}>
        {!query && (
          <Text dimColor>Type to search through the current conversation.</Text>
        )}
        {query && matches.length === 0 && (
          <Text dimColor>No matches found.</Text>
        )}

        {currentMatch && (
          <Box flexDirection="column">
            <Text dimColor>
              {BLOCKQUOTE_BAR} Message {currentMatch.messageIdx + 1}
            </Text>
            <Box marginTop={0}>
              <Text color={colors.brand} bold>
                {currentMatch.snippet}
              </Text>
            </Box>
          </Box>
        )}
      </Box>

      {/* Footer */}
      <Box paddingX={1}>
        <Text dimColor>
          Type to search · ↑↓ navigate · Enter/↓ next · Esc close
        </Text>
      </Box>
    </Box>
  );
}
