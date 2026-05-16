import React, { useState, useEffect, useCallback, useRef } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

export function SessionSwitcher() {
  const show = usePuxStore((s) => s.showSessionSwitcher);
  const conversations = usePuxStore((s) => s.conversations);
  const activeAgentId = usePuxStore((s) => s.activeAgentId);
  const activeProject = usePuxStore((s) => s.activeProject);
  const setConversation = usePuxStore((s) => s.setConversation);
  const closeSwitcher = usePuxStore((s) => s.closeSessionSwitcher);
  const colors = useColors();
  const [filter, setFilter] = useState("");
  const [idx, setIdx] = useState(0);

  // Load conversations when opened
  useEffect(() => {
    if (show) {
      usePuxStore.getState().loadConversations();
      setFilter("");
      setIdx(0);
    }
  }, [show]);

  const filtered = conversations.filter((c) => {
    if (!filter) return true;
    const q = filter.toLowerCase();
    return (
      c.title?.toLowerCase().includes(q) ||
      c.agentId?.toLowerCase().includes(q) ||
      c.project?.toLowerCase().includes(q)
    );
  });

  useInput(
    useCallback(
      (input: string, key: any) => {
        if (!show) return;

        if (key.escape) {
          closeSwitcher();
          return;
        }

        if (key.return) {
          const conv = filtered[idx];
          if (conv) {
            setConversation(conv.project, conv.agentId);
            closeSwitcher();
          }
          return;
        }

        if (key.upArrow) {
          setIdx((prev) => Math.max(0, prev - 1));
          return;
        }
        if (key.downArrow) {
          setIdx((prev) => Math.min(filtered.length - 1, prev + 1));
          return;
        }

        if (key.backspace || key.delete) {
          setFilter((prev) => prev.slice(0, -1));
          return;
        }

        // Printable characters → filter
        if (input && !key.ctrl && !key.meta && !key.escape && !key.return && !key.upArrow && !key.downArrow) {
          setFilter((prev) => prev + input);
          return;
        }
      },
      [show, filtered, idx, closeSwitcher, setConversation],
    ),
  );

  if (!show) return null;

  return (
    <Box flexDirection="column" paddingY={1} paddingX={1}>
      <Box backgroundColor="cyan" paddingX={1}>
        <Text bold> {symbols.dot} Switch Session</Text>
      </Box>

      {/* Filter input */}
      <Box marginTop={1} paddingLeft={1}>
        <Text color={colors.brand} bold>{">"} </Text>
        <Text>{filter}</Text>
        <Text dimColor>{"\u2588"}</Text>
      </Box>

      {/* Results */}
      <Box flexDirection="column" marginTop={1} paddingLeft={1}>
        {filtered.length === 0 && (
          <Text dimColor>No matching conversations.</Text>
        )}
        {filtered.slice(0, 10).map((conv, i) => {
          const isActive = conv.agentId === activeAgentId && conv.project === activeProject;
          const selected = i === idx;
          return (
            <Text key={`${conv.project}:${conv.agentId}`} backgroundColor={selected ? "gray" : undefined}>
              {"  "}
              <Text
                color={isActive ? colors.brand : undefined}
                bold={isActive || selected}
              >
                {conv.title?.slice(0, 40) || "(untitled)"}
              </Text>
              <Text dimColor color="gray">
                {" "}{conv.messageCount}msgs {symbols.dot} {conv.project}
              </Text>
              {isActive && <Text color={colors.brand}> ←</Text>}
            </Text>
          );
        })}
        {filtered.length > 10 && (
          <Text dimColor>... and {filtered.length - 10} more</Text>
        )}
      </Box>

      <Box marginTop={1} paddingLeft={1}>
        <Text dimColor>
          Type to filter · ↑↓ navigate · Enter switch · Esc close
        </Text>
      </Box>
    </Box>
  );
}
