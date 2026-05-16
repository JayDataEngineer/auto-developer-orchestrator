import React, { useState, useCallback, useMemo } from "react";
import { Box, Text, useInput, useStdout, type Key } from "ink";
import { usePuxStore, type MCPServerInfo } from "@pux/shared";
import { useColors, symbols } from "../theme.js";

export function MCPOverlay() {
  const show = usePuxStore((s) => s.showMCPOverlay);
  const servers = usePuxStore((s) => s.mcpServers);
  const closeMCPOverlay = usePuxStore((s) => s.closeMCPOverlay);

  const [selectedIdx, setSelectedIdx] = useState(0);
  const [expandedServer, setExpandedServer] = useState<string | null>(null);

  const { stdout } = useStdout();
  const rows = stdout?.rows ?? 24;
  const cols = stdout?.columns ?? 80;

  const expanded = useMemo(
    () => expandedServer ? servers.find((s) => s.prefix === expandedServer) ?? null : null,
    [expandedServer, servers],
  );

  const maxVisible = rows - 8;
  const scrollOffset = Math.max(0, selectedIdx - maxVisible + 3);

  useInput(
    useCallback(
      (_input: string, key: Key) => {
        if (!show) return;

        if (key.escape) {
          if (expandedServer) {
            setExpandedServer(null);
            return;
          }
          closeMCPOverlay();
          return;
        }

        if (expandedServer) {
          if (key.upArrow) {
            setSelectedIdx((i) => Math.max(0, i - 1));
            return;
          }
          if (key.downArrow) {
            const tools = expanded?.tools ?? [];
            setSelectedIdx((i) => Math.min(tools.length, i + 1));
            return;
          }
          return;
        }

        if (key.upArrow) {
          setSelectedIdx((i) => Math.max(0, i - 1));
          return;
        }
        if (key.downArrow) {
          setSelectedIdx((i) => Math.min(servers.length - 1, i + 1));
          return;
        }
        if (key.return) {
          const server = servers[Math.min(selectedIdx, servers.length - 1)];
          if (server) {
            setExpandedServer(server.prefix);
            setSelectedIdx(0);
          }
        }
      },
      [show, selectedIdx, servers, expandedServer, expanded, closeMCPOverlay],
    ),
  );

  if (!show) return null;

  if (expandedServer) {
    if (!expanded) {
      return (
        <Box flexDirection="column" flexGrow={1}>
          <Header cols={cols} label="MCP Servers" />
          <Box paddingX={2} flexGrow={1}>
            <Text dimColor>Server not found.</Text>
          </Box>
          <Footer cols={cols} hint="Esc back" />
        </Box>
      );
    }

    const tools = expanded.tools ?? [];
    const visible = tools.slice(scrollOffset, scrollOffset + maxVisible - 2);

    return (
      <Box flexDirection="column" flexGrow={1}>
        <Header cols={cols} label={`MCP: ${expanded.prefix}`} />
        <Box flexDirection="column" paddingX={2} flexGrow={1}>
          <Box flexDirection="column">
            <Box>
              <Text color={expanded.available ? "green" : "red"} bold>
                {expanded.available ? symbols.toolDone : symbols.toolError}
              </Text>
              <Text> </Text>
              <Text bold>{expanded.prefix}</Text>
              <Text dimColor>  {expanded.endpoint}</Text>
            </Box>
            <Text dimColor>
              {expanded.toolCount} tool{expanded.toolCount !== 1 ? "s" : ""}
              {expanded.available ? " · available" : " · unavailable"}
            </Text>
          </Box>
          {tools.length > 0 && (
            <Box flexDirection="column" marginTop={1}>
              <Text bold underline>Tools:</Text>
              {visible.map((tool, vi) => {
                const globalIdx = scrollOffset + vi;
                const isSelected = globalIdx === selectedIdx;
                return (
                  <Text key={tool} backgroundColor={isSelected ? "gray" : undefined} bold={isSelected}>
                    {"   "} {tool}
                  </Text>
                );
              })}
              {tools.length > maxVisible - 2 && (
                <Text dimColor>... and {tools.length - maxVisible + 2} more</Text>
              )}
            </Box>
          )}
          {tools.length === 0 && (
            <Box marginTop={1}>
              <Text dimColor>No tools registered for this server.</Text>
            </Box>
          )}
        </Box>
        <Footer cols={cols} hint="↑↓ navigate · Esc back" />
      </Box>
    );
  }

  const visibleServers = servers.slice(scrollOffset, scrollOffset + maxVisible);

  return (
    <Box flexDirection="column" flexGrow={1}>
      <Header cols={cols} label="MCP Servers" />
      <Box flexDirection="column" paddingX={2} flexGrow={1}>
        {servers.length === 0 && (
          <Box flexDirection="column">
            <Text dimColor>No MCP servers configured.</Text>
            <Text dimColor>Servers are configured in the backend at startup.</Text>
          </Box>
        )}
        {visibleServers.map((server, vi) => {
          const globalIdx = scrollOffset + vi;
          const isSelected = globalIdx === selectedIdx;
          return (
            <ServerRow
              key={server.prefix}
              server={server}
              isSelected={isSelected}
            />
          );
        })}
      </Box>
      <Footer cols={cols} hint={servers.length > 0 ? "↑↓ navigate · Enter expand · Esc close" : "Esc close"} />
    </Box>
  );
}

function ServerRow({ server, isSelected }: { server: MCPServerInfo; isSelected: boolean }) {
  const colors = useColors();
  const statusIcon = server.available ? "●" : "○";
  const statusColor = server.available ? "green" : "red";

  return (
    <Text backgroundColor={isSelected ? "gray" : undefined}>
      {"  "}
      <Text color={statusColor}>{statusIcon} </Text>
      <Text bold={isSelected}>{server.prefix.padEnd(12)}</Text>
      <Text dimColor>{server.endpoint}</Text>
      <Text dimColor>  ({server.toolCount} tool{server.toolCount !== 1 ? "s" : ""})</Text>
      {!server.available && <Text color={colors.warning}> [offline]</Text>}
    </Text>
  );
}

function Header({ cols, label }: { cols: number; label: string }) {
  return (
    <Box flexDirection="column">
      <Box paddingX={1}>
        <Text backgroundColor="cyan" bold> {label} </Text>
      </Box>
      <Text color="cyan">{"═".repeat(cols)}</Text>
    </Box>
  );
}

function Footer({ cols, hint }: { cols: number; hint: string }) {
  return (
    <Box flexDirection="column">
      <Text color="cyan">{"═".repeat(cols)}</Text>
      <Box paddingX={2}>
        <Text dimColor color="gray">{hint}</Text>
      </Box>
    </Box>
  );
}
