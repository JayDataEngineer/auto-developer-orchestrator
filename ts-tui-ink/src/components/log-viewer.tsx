import React, { useState, useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { useColors, symbols, BLOCKQUOTE_BAR } from "../theme.js";

type LogSection = "agents" | "usage" | "context" | "info";

const SECTIONS: LogSection[] = ["agents", "usage", "context", "info"];
const SECTION_LABELS: Record<LogSection, string> = {
  agents: "Agent Activity",
  usage: "Token Usage",
  context: "Context",
  info: "Session Info",
};

export function LogViewer() {
  const show = usePuxStore((s) => s.showLogViewer);
  const agents = usePuxStore((s) => s.agents);
  const lastUsage = usePuxStore((s) => s.lastUsage);
  const contextMetrics = usePuxStore((s) => s.contextMetrics);
  const activeProject = usePuxStore((s) => s.activeProject);
  const activeModel = usePuxStore((s) => s.activeModel);
  const activeAgentId = usePuxStore((s) => s.activeAgentId);
  const conversations = usePuxStore((s) => s.conversations);
  const close = usePuxStore((s) => s.closeLogViewer);
  const colors = useColors();
  const [section, setSection] = useState<LogSection>("agents");

  useInput(
    useCallback(
      (input: string, key: any) => {
        if (!show) return;
        if (key.escape) { close(); return; }
        if (key.leftArrow || key.rightArrow) {
          const idx = SECTIONS.indexOf(section);
          const next = key.rightArrow
            ? (idx + 1) % SECTIONS.length
            : (idx - 1 + SECTIONS.length) % SECTIONS.length;
          setSection(SECTIONS[next]);
          return;
        }
      },
      [show, section, close],
    ),
  );

  if (!show) return null;

  return (
    <Box flexDirection="column" flexGrow={1}>
      <Box backgroundColor="cyan" paddingX={1}>
        <Text bold> {symbols.dot} Diagnostics</Text>
      </Box>
      <Text dimColor>{'═'.repeat(80)}</Text>

      {/* Section tabs */}
      <Box paddingX={1} paddingY={1} gap={1}>
        {SECTIONS.map((s) => (
          <Text key={s} bold={s === section} color={s === section ? colors.brand : "gray"}>
            {s === section ? symbols.arrow : " "} {SECTION_LABELS[s]}
          </Text>
        ))}
      </Box>

      <Text dimColor>{'─'.repeat(80)}</Text>

      {/* Content */}
      <Box flexGrow={1} flexDirection="column" paddingX={1}>
        {section === "agents" && <AgentLog agents={agents} />}
        {section === "usage" && <UsageLog lastUsage={lastUsage} />}
        {section === "context" && <ContextLog metrics={contextMetrics} />}
        {section === "info" && <SessionInfo />}
      </Box>

      <Text dimColor>{'═'.repeat(80)}</Text>
      <Box paddingX={1}>
        <Text dimColor>← → switch tab · Esc close</Text>
      </Box>
    </Box>
  );
}

function AgentLog({ agents }: { agents: Map<string, any> }) {
  const colors = useColors();
  const entries = Array.from(agents.values());

  if (entries.length === 0) {
    return <Text dimColor>No agent activity yet.</Text>;
  }

  return (
    <Box flexDirection="column">
      {entries.map((agent: any) => {
        const dur = agent.endedAt
          ? `${Math.round((agent.endedAt - agent.startedAt) / 1000)}s`
          : "running";
        return (
          <Box key={agent.agentId} flexDirection="column" marginBottom={1}>
            <Box>
              <Text
                color={
                  agent.status === "error"
                    ? colors.error
                    : agent.status === "running"
                      ? colors.running
                      : colors.success
                }
                bold
              >
                {agent.status === "running" ? symbols.toolRunning : symbols.toolDone}{" "}
                {agent.agentName || agent.agentId.slice(0, 8)}
              </Text>
              <Text dimColor> {dur} {symbols.dot} {agent.task?.slice(0, 50)}</Text>
            </Box>
            {agent.toolCalls?.length > 0 && (
              <Box paddingLeft={3} flexDirection="column">
                {agent.toolCalls.slice(-5).map((tc: any, i: number) => (
                  <Text key={i} dimColor>
                    {BLOCKQUOTE_BAR} {tc.isError ? symbols.cross : symbols.dot}{" "}
                    {tc.toolName}
                    {tc.timestamp ? ` ${new Date(tc.timestamp).toLocaleTimeString()}` : ""}
                  </Text>
                ))}
                {agent.toolCalls.length > 5 && (
                  <Text dimColor>{BLOCKQUOTE_BAR} ... and {agent.toolCalls.length - 5} more</Text>
                )}
              </Box>
            )}
          </Box>
        );
      })}
    </Box>
  );
}

function UsageLog({ lastUsage }: { lastUsage: any }) {
  if (!lastUsage) {
    return <Text dimColor>No usage data yet.</Text>;
  }
  return (
    <Box flexDirection="column">
      <Text dimColor>{BLOCKQUOTE_BAR} Input tokens:  {lastUsage.input?.toLocaleString() || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Output tokens: {lastUsage.output?.toLocaleString() || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Cache tokens:  {lastUsage.cache?.toLocaleString() || "—"}</Text>
      {lastUsage.model && (
        <Text dimColor>{BLOCKQUOTE_BAR} Model:        {lastUsage.model}</Text>
      )}
    </Box>
  );
}

function ContextLog({ metrics }: { metrics: any }) {
  if (!metrics) {
    return <Text dimColor>No context metrics yet.</Text>;
  }
  return (
    <Box flexDirection="column">
      <Text dimColor>{BLOCKQUOTE_BAR} Context tokens: {metrics.contextTokens?.toLocaleString() || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Context size:   {metrics.contextSize?.toLocaleString() || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Utilization:    {metrics.contextUtil != null ? `${Math.round(metrics.contextUtil * 100)}%` : "—"}</Text>
      {metrics.compactionType && (
        <Text dimColor>{BLOCKQUOTE_BAR} Last compact:   {metrics.compactionType}</Text>
      )}
    </Box>
  );
}

function SessionInfo() {
  const colors = useColors();
  const activeProject = usePuxStore((s) => s.activeProject);
  const activeProjectPath = usePuxStore((s) => s.activeProjectPath);
  const activeModel = usePuxStore((s) => s.activeModel);
  const activeAgentId = usePuxStore((s) => s.activeAgentId);
  const activeConversationId = usePuxStore((s) => s.activeConversationId);
  const conversationKey = usePuxStore((s) => s.conversationKey);
  const providers = usePuxStore((s) => s.providers);
  const modelList = usePuxStore((s) => s.modelList);

  return (
    <Box flexDirection="column">
      <Text dimColor>{BLOCKQUOTE_BAR} Project:   {activeProject || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Project path: {activeProjectPath || "—"}</Text>
      {activeProjectPath && <Text dimColor>{BLOCKQUOTE_BAR}   (cwd for path autocomplete)</Text>}
      <Text dimColor>{BLOCKQUOTE_BAR} Model:     {activeModel || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Agent ID:  {activeAgentId || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Conversation: {conversationKey || "—"}</Text>
      <Text dimColor>{BLOCKQUOTE_BAR} Providers: {Object.keys(providers).length} configured, {modelList.length} models</Text>
    </Box>
  );
}
