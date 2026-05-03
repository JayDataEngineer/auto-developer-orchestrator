import React, { useState, useCallback, useReducer, useRef, useEffect } from "react";
import { Box, Text, useApp, Spacer, useInput, useStdout, useStdin } from "ink";
import TextInput from "ink-text-input";
import Spinner from "ink-spinner";
import { ApiClient } from "./api/client";
import type { ToolStartData, ToolEndData, ApprovalData, AgentEndData } from "./api/types";
import { renderImage, imagePlaceholder, hasImageSupport } from "./terminal-image";

// ── helpers ──────────────────────────────────────────────────────────────────

const ensureStr = (v: unknown): string => {
  if (typeof v === "string") return v;
  if (v === null || v === undefined) return "";
  try { return JSON.stringify(v, null, 1); } catch { return String(v); }
};

const trunc = (s: string, max: number): string => {
  if (!s || s.length <= max) return s;
  return s.slice(0, max) + "…";
};

const fmtArgs = (raw: unknown): string => {
  const s = typeof raw === "string" ? raw : JSON.stringify(raw);
  if (!s || s === "{}" || s === "null") return "";
  try {
    const obj = JSON.parse(s);
    const entries = Object.entries(obj as Record<string, unknown>);
    if (entries.length === 0) return "";
    // Extract the primary "meaningful" key
    const priKey = entries.find(([k]) =>
      k === "command" || k === "path" || k === "file" || k === "url" ||
      k === "message" || k === "query" || k === "content" || k === "text"
    );
    if (priKey) {
      const v = ensureStr(priKey[1]);
      return v.length > 70 ? v.slice(0, 67) + "…" : v;
    }
    // Fallback: just the first value
    const fv = entries[0]?.[1];
    if (fv === undefined || fv === null) return "";
    const v = ensureStr(fv);
    return v.length > 50 ? v.slice(0, 47) + "…" : v;
  } catch {
    return s.length > 70 ? s.slice(0, 67) + "…" : s;
  }
};

const renderMd = (text: string): string => {
  let out = text;
  out = out.replace(/```(\w*)\n([\s\S]*?)```/g, (_m, _lang, code) => `\x1b[2m\x1b[3m${code.trim()}\x1b[0m`);
  out = out.replace(/`([^`]+)`/g, (_m, c) => `\x1b[2m${c}\x1b[0m`);
  out = out.replace(/\*\*([^*]+)\*\*/g, (_m, c) => `\x1b[1m${c}\x1b[0m`);
  out = out.replace(/\*([^*]+)\*/g, (_m, c) => `\x1b[3m${c}\x1b[0m`);
  return out;
};

const renderDiff = (text: string): React.ReactNode =>
  text.split("\n").slice(0, 30).map((line, i) => {
    if (line.startsWith("+")) return <Text key={i} color="green">{line}</Text>;
    if (line.startsWith("-")) return <Text key={i} color="red">{line}</Text>;
    if (line.startsWith("@@")) return <Text key={i} color="blue">{line}</Text>;
    return <Text key={i} dimColor>{line}</Text>;
  });

const renderToolResult = (tool: ToolCall): React.ReactNode => {
  if (tool.error) return <Text color="red">✗ {trunc(tool.error, 120)}</Text>;
  const r = tool.result;
  if (!r) return null;

  // Screenshot tools — image display
  const isScreenshot = tool.name === "browse_to" || tool.name === "read_page" ||
    tool.name === "desktop_screenshot" || tool.name === "screenshot";
  if (isScreenshot && r.length > 100) {
    if (r.startsWith("iVBOR") || r.startsWith("/9j/") || r.startsWith("R0lG")) {
      const imgSeq = renderImage(r, { maxW: 50, maxH: 12 });
      if (imgSeq) return <Box flexDirection="column"><Text>{imgSeq}</Text><Text dimColor>screenshot</Text></Box>;
      return <Text dimColor>{imagePlaceholder(r, tool.name)}</Text>;
    }
  }

  // Any tool: show at most 3 lines, trimmed
  const lines = r.split("\n").slice(0, 3);
  let preview = lines.join("\n");
  // Trim per-line
  if (preview.length > 300) preview = preview.slice(0, 297) + "…";
  return <Text dimColor>{preview}</Text>;
};

// ── types ────────────────────────────────────────────────────────────────────

interface ToolCall { name: string; id: string; args: string; result: string; error: string; done: boolean; }
interface Message { id: number; role: "user" | "assistant"; content: string; thinking: string; tools: ToolCall[]; tokens?: { in: number; out: number }; timestamp: number; }
interface PendingApproval { requestId: string; toolName: string; toolArgs: string; message: string; risk: string; type: string; }
interface Conversation { id: string; title: string; lastMessage: string; messageCount: number; lastAt: string; }
type Mode = "chat" | "scheduler" | "help" | "history" | "artifacts";

interface State {
  messages: Message[]; mode: Mode; streaming: boolean;
  sText: string; sThink: string; sTools: ToolCall[];
  approval: PendingApproval | null; thinkOpen: boolean;
  tokIn: number; tokOut: number; history: string[]; histIdx: number;
  thinkStart: number; conversations: Conversation[]; convId: string;
  slashMode: boolean;
  artifacts: { id: string; type: string; title: string; content: string }[];
}

type Action =
  | { type: "USER"; text: string } | { type: "STREAM" } | { type: "TEXT"; text: string }
  | { type: "THINK"; text: string } | { type: "TOOL_ON"; tool: ToolStartData }
  | { type: "TOOL_OFF"; tool: ToolEndData } | { type: "ASK"; approval: ApprovalData }
  | { type: "END"; data: AgentEndData } | { type: "CLOSE" } | { type: "ERR"; error: string }
  | { type: "ABORT" } | { type: "THINK_TGL" } | { type: "MODE"; mode: Mode }
  | { type: "OK" } | { type: "LOAD_CONVS"; convs: Conversation[] }
  | { type: "SET_CONV"; id: string; messages: Message[] } | { type: "NEW_CONV" }
  | { type: "SLASH"; on: boolean } | { type: "HIST_IDX"; idx: number }
  | { type: "RENAME_CONV"; title: string }
  | { type: "LOAD_ARTIFACTS"; artifacts: State["artifacts"] }
  | { type: "SYNC_AGENT"; id: string };

function genConvId(): string { return "conv_" + Date.now().toString(36) + Math.random().toString(36).slice(2, 6); }

function init(): State {
  return {
    messages: [], mode: "chat", streaming: false, sText: "", sThink: "", sTools: [],
    approval: null, thinkOpen: true, tokIn: 0, tokOut: 0, history: [], histIdx: -1,
    thinkStart: 0, conversations: [], convId: "default", slashMode: false,
    artifacts: [],
  };
}

function reduce(state: State, a: Action): State {
  switch (a.type) {
    case "USER":
      return { ...state, messages: [...state.messages, { id: Date.now(), role: "user", content: a.text, thinking: "", tools: [], timestamp: Date.now() }], history: [a.text, ...state.history].slice(0, 100), histIdx: -1, slashMode: false };
    case "STREAM":
      return { ...state, streaming: true, sText: "", sThink: "", sTools: [], approval: null, thinkStart: Date.now() };
    case "TEXT": return { ...state, sText: state.sText + a.text };
    case "THINK": return { ...state, sThink: state.sThink + a.text };
    case "TOOL_ON":
      return { ...state, sTools: [...state.sTools, { name: a.tool.toolName, id: a.tool.toolId, args: fmtArgs(a.tool.args), result: "", error: "", done: false }] };
    case "TOOL_OFF":
      return { ...state, sTools: state.sTools.map(t => t.name === a.tool.toolName && !t.done ? { ...t, result: ensureStr(a.tool.result), error: a.tool.error, done: true } : t) };
    case "ASK":
      return { ...state, approval: { requestId: a.approval.requestId, toolName: a.approval.toolName, toolArgs: a.approval.toolArgs, message: a.approval.message, risk: a.approval.risk, type: (a.approval as any).type || "" } };
    case "END": {
      const m: Message = { id: Date.now(), role: "assistant", content: state.sText, thinking: state.sThink, tools: [...state.sTools], tokens: { in: a.data.inputTokens, out: a.data.outputTokens }, timestamp: Date.now() };
      return { ...state, streaming: false, messages: (m.content || m.thinking || m.tools.length) ? [...state.messages, m] : state.messages, sText: "", sThink: "", sTools: [], tokIn: state.tokIn + (a.data.inputTokens || 0), tokOut: state.tokOut + (a.data.outputTokens || 0) };
    }
    case "CLOSE": {
      const m: Message = { id: Date.now(), role: "assistant", content: state.sText, thinking: state.sThink, tools: [...state.sTools], timestamp: Date.now() };
      return { ...state, streaming: false, messages: (m.content || m.thinking || m.tools.length) ? [...state.messages, m] : state.messages, sText: "", sThink: "", sTools: [] };
    }
    case "ABORT": {
      const m: Message = { id: Date.now(), role: "assistant", content: state.sText || "(cancelled)", thinking: state.sThink, tools: [...state.sTools], timestamp: Date.now() };
      return { ...state, streaming: false, messages: [...state.messages, m], sText: "", sThink: "", sTools: [] };
    }
    case "ERR":
      return { ...state, streaming: false, messages: [...state.messages, { id: Date.now(), role: "assistant", content: "**Error:** " + a.error, thinking: "", tools: [], timestamp: Date.now() }], sText: "", sThink: "", sTools: [] };
    case "THINK_TGL": return { ...state, thinkOpen: !state.thinkOpen };
    case "MODE": return { ...state, mode: a.mode };
    case "OK": return { ...state, approval: null };
    case "LOAD_CONVS": return { ...state, conversations: a.convs };
    case "SET_CONV": return { ...state, convId: a.id, messages: a.messages, tokIn: 0, tokOut: 0, mode: "chat" };
    case "NEW_CONV": {
      const newId = genConvId();
      const conv: Conversation = { id: newId, title: "New conversation", lastMessage: "", messageCount: 0, lastAt: new Date().toISOString() };
      return { ...state, convId: newId, messages: [], tokIn: 0, tokOut: 0, mode: "chat", conversations: [conv, ...state.conversations] };
    }
    case "SLASH": return { ...state, slashMode: a.on };
    case "HIST_IDX": return { ...state, histIdx: a.idx };
    case "RENAME_CONV":
      return { ...state, conversations: state.conversations.map(c => c.id === state.convId ? { ...c, title: a.title } : c) };
    case "LOAD_ARTIFACTS":
      return { ...state, artifacts: a.artifacts };
    case "SYNC_AGENT":
      return {
        ...state,
        convId: a.id,
        conversations: state.conversations.map(c => c.id === state.convId ? { ...c, id: a.id } : c),
      };
    default: return state;
  }
}

// ── slash commands ───────────────────────────────────────────────────────────

const SLASH_COMMANDS: { cmd: string; desc: string; action: string }[] = [
  { cmd: "/help", desc: "Show keyboard shortcuts", action: "help" },
  { cmd: "/jobs", desc: "Open scheduler", action: "scheduler" },
  { cmd: "/clear", desc: "Clear current messages", action: "clear" },
  { cmd: "/new", desc: "Start new conversation", action: "new" },
  { cmd: "/history", desc: "List conversations", action: "history" },
  { cmd: "/rename <title>", desc: "Rename current conversation", action: "rename" },
  { cmd: "/delete", desc: "Delete current conversation", action: "delete" },
  { cmd: "/artifacts", desc: "View plan/todo/notes artifacts", action: "artifacts" },
  { cmd: "/model", desc: "Show available models", action: "model" },
  { cmd: "/quit", desc: "Exit", action: "quit" },
];

function findCommand(input: string): string | null {
  if (!input.startsWith("/")) return null;
  const parts = input.trim().split(/\s+/);
  return parts[0] ?? null;
}

function SlashHint({ input }: { input: string }) {
  if (!input || !input.startsWith("/")) return null;
  const cmd = findCommand(input);
  const matches = !cmd
    ? SLASH_COMMANDS
    : SLASH_COMMANDS.filter(c => c.cmd.startsWith(cmd));
  return (
    <Box flexDirection="column" marginLeft={4} marginTop={1}>
      {matches.map(c => (
        <Box key={c.cmd}>
          <Text color="yellow">{c.cmd}</Text>
          <Text dimColor> — {c.desc}</Text>
        </Box>
      ))}
    </Box>
  );
}

function executeSlash(input: string, dispatch: React.Dispatch<Action>, client: ApiClient, project: string, convId: string, onExit: () => void): boolean {
  const parts = input.trim().split(/\s+/);
  const cmd = parts[0];

  switch (cmd) {
    case "/help": dispatch({ type: "MODE", mode: "help" }); return true;
    case "/jobs": dispatch({ type: "MODE", mode: "scheduler" }); return true;
    case "/clear": dispatch({ type: "NEW_CONV" }); return true;
    case "/new": dispatch({ type: "NEW_CONV" }); return true;
    case "/history": dispatch({ type: "MODE", mode: "history" }); return true;
    case "/artifacts": dispatch({ type: "MODE", mode: "artifacts" }); return true;
    case "/quit": onExit(); return true;
    case "/rename":
      if (parts.length > 1) {
        const title = parts.slice(1).join(" ");
        client.renameConversation(project, convId, title);
        dispatch({ type: "RENAME_CONV", title });
      }
      return true;
    case "/delete":
      client.deleteConversation(project, convId);
      dispatch({ type: "NEW_CONV" });
      return true;
    default:
      return false;
  }
}

// ── Components ───────────────────────────────────────────────────────────────

const Dim = ({ children }: { children: React.ReactNode }) => <Text dimColor>{children}</Text>;
const Accent = ({ children }: { children: React.ReactNode }) => <Text color="blueBright" bold>{children}</Text>;

function Line() { return <Text color="grey">{"─".repeat(80)}</Text>; }

function ThinkBlock({ text, expanded, startMs }: { text: string; expanded: boolean; startMs: number }) {
  if (!text) return null;
  const words = text.trim().split(/\s+/).length;
  const dur = startMs ? `${Math.round((Date.now() - startMs) / 1000)}s` : "";
  if (!expanded) return (
    <Box marginBottom={1}>
      <Text dimColor italic>  ∴ Thought {words} words{dur ? ` · ${dur}` : ""} · Ctrl+T expand</Text>
    </Box>
  );
  return (
    <Box flexDirection="column" marginBottom={1}>
      <Text dimColor italic>·· thinking {dur ? `(${dur})` : ""} ··</Text>
      <Box paddingLeft={2}><Text dimColor italic>{text.trim()}</Text></Box>
    </Box>
  );
}

// ── tool color by category ──────────────────────────────────────────────────

const toolColor = (name: string): string => {
  if (name === "bash" || name === "exec") return "yellow";
  if (name === "edit" || name === "write" || name === "patch") return "blue";
  if (name === "read" || name === "view" || name === "cat" || name === "grep") return "green";
  if (name.startsWith("browse") || name.startsWith("click") || name.startsWith("type") || name.startsWith("search")) return "cyan";
  if (name.startsWith("desktop") || name.startsWith("screenshot") || name.startsWith("key")) return "magenta";
  if (name === "delegate_to") return "red";
  return "dim";
};

const ICONS: Record<string, string> = {
  bash: ">_", exec: ">_", edit: "✎", write: "✎", patch: "✎",
  read: "☰", view: "☰", cat: "☰", grep: "⌕",
  browse_to: "◈", read_page: "◈", click_element: "◈", type_text: "◈", search_web: "◈",
  desktop_screenshot: "▣", desktop_click: "▣", desktop_type: "▣", desktop_key: "▣",
  delegate_to: "▶",
};

function ToolCard({ tool, elapsed }: { tool: ToolCall; elapsed?: number }) {
  const icon = ICONS[tool.name] ?? "●";
  const cls = tool.done ? (tool.error ? "red" : toolColor(tool.name)) : "yellow";
  const status = tool.done ? (tool.error ? "✗" : "✓") : "●";
  const dur = elapsed != null ? `${(elapsed / 1000).toFixed(1)}s` : "";
  const args = fmtArgs(tool.args);

  return (
    <Box flexDirection="column" marginBottom={1} paddingLeft={1}>
      <Box flexDirection="row">
        <Text color={cls}>{status} </Text>
        <Text color={cls} bold>{icon} {tool.name}</Text>
        {args ? <Dim> {args}</Dim> : null}
        {dur ? <Dim> · {dur}</Dim> : null}
      </Box>
      {tool.done && tool.result ? (
        <Box flexDirection="column" paddingLeft={3}>
          {renderToolResult(tool)}
        </Box>
      ) : null}
      {tool.error ? (
        <Box paddingLeft={3}><Text color="red">{trunc(tool.error, 120)}</Text></Box>
      ) : null}
      {!tool.done ? (
        <Box paddingLeft={3}><Text color="yellow"><Spinner type="dots" /> running…</Text></Box>
      ) : null}
    </Box>
  );
}

function AsstMsg({ msg, thinkOpen }: { msg: Message; thinkOpen: boolean }) {
  return (
    <Box flexDirection="column" marginBottom={1}>
      {msg.thinking ? <ThinkBlock text={msg.thinking} expanded={thinkOpen} startMs={msg.timestamp} /> : null}
      {msg.content ? <Box marginBottom={1}><Text>{renderMd(msg.content)}</Text></Box> : null}
      {msg.tools.map((t, i) => <ToolCard key={i} tool={t} />)}
      {msg.tokens ? <Dim>↑{msg.tokens.in} ↓{msg.tokens.out}</Dim> : null}
    </Box>
  );
}

function StreamBlock({ text, think, tools, thinkOpen, thinkStart }: { text: string; think: string; tools: ToolCall[]; thinkOpen: boolean; thinkStart: number }) {
  if (!text && !think && tools.length === 0) return null;
  return (
    <Box flexDirection="column" marginBottom={1}>
      {think ? <ThinkBlock text={think} expanded={thinkOpen} startMs={thinkStart} /> : null}
      {text ? <Box marginBottom={1}><Text>{renderMd(text)}</Text></Box> : null}
      {tools.map((t, i) => <ToolCard key={i} tool={t} />)}
      <Box><Text color="green">● </Text><Dim>thinking...</Dim></Box>
    </Box>
  );
}

function Approve({ approval, onR }: { approval: PendingApproval; onR: (a: "approve" | "deny" | "answer", msg?: string) => void }) {
  const [v, sv] = useState("");
  return (
    <Box flexDirection="column" borderStyle="round" borderColor="yellow" paddingX={1} marginY={1}>
      <Box><Text color="yellow" bold>AUTHORIZATION REQUIRED</Text><Text>  </Text><Text color={approval.risk === "high" ? "red" : "green"} bold>{approval.risk.toUpperCase()}</Text></Box>
      <Box marginTop={1}><Text bold color="yellow">{approval.toolName}</Text><Dim> {trunc(approval.toolArgs, 60)}</Dim></Box>
      {approval.message ? <Box marginTop={1}><Dim>{approval.message}</Dim></Box> : null}
      {approval.type === "bash" || approval.toolName === "bash" ? <Box marginTop={1} paddingLeft={2}><Text bold>{approval.toolArgs}</Text></Box> : null}
      <Box marginTop={1}><Text color="green">[y]</Text><Dim> Approve  </Dim><Text color="red">[n]</Text><Dim> Deny  </Dim><Text color="cyan">[a:text]</Text><Dim> Answer</Dim></Box>
      <Box marginTop={1}>
        <TextInput value={v} onChange={sv} placeholder="y / n / a:answer..."
          onSubmit={(val) => {
            const x = val.trim().toLowerCase();
            if (x === "y" || x === "yes") onR("approve");
            else if (x === "n" || x === "no") onR("deny");
            else if (x.startsWith("a:") || x.startsWith("answer:")) onR("answer", x.replace(/^(a|answer):/, "").trim());
          }} />
      </Box>
    </Box>
  );
}

function Head({ project, streaming }: { project: string; streaming: boolean }) {
  return (
    <Box flexDirection="row" paddingX={1}>
      <Accent>pux</Accent>
      <Dim> · {project}</Dim>
      <Spacer />
      {streaming ? <Text color="green">● streaming</Text> : null}
    </Box>
  );
}

function HelpView({ onDismiss }: { onDismiss: () => void }) {
  useInput((_, key) => { if (key.escape) onDismiss(); });
  const rows: [string, string][] = [
    ["Enter", "Send (trailing \\ for newline)"],
    ["Shift+Enter", "Insert newline"],
    ["Up/Down", "Input history"],
    ["Ctrl+T", "Toggle thinking"],
    ["Ctrl+J", "Scheduler"],
    ["Ctrl+C", "Quit / abort"],
    ["/help", "This help"],
    ["/jobs", "Scheduler"],
    ["/new", "New conversation"],
    ["/history", "Conversation list"],
    ["/rename", "Rename conversation"],
    ["/clear", "Clear messages"],
    ["/quit", "Exit"],
  ] as const;
  return (
    <Box flexDirection="column" padding={1}>
      <Box borderStyle="round" borderColor="blueBright" padding={1}>
        <Box flexDirection="column">
          <Text bold color="blueBright">Keyboard & Slash Commands</Text>
          <Box flexDirection="column" marginTop={1}>
            {rows.map(([k, d]) => <Box key={k}><Text color="green">{k.padEnd(16)}</Text><Dim>{d}</Dim></Box>)}
          </Box>
          <Box marginTop={1}><Dim>esc to dismiss</Dim></Box>
        </Box>
      </Box>
    </Box>
  );
}

// ── Conversation History View ────────────────────────────────────────────────

function HistoryView({ client, project, conversations, currentId, onSwitch, onNew, onDismiss }: {
  client: ApiClient; project: string; conversations: Conversation[];
  currentId: string; onSwitch: (id: string) => void; onNew: () => void; onDismiss: () => void;
}) {
  const [sel, setSel] = useState(0);
  const all = [{ id: "default", title: "Default", lastMessage: "", messageCount: 0, lastAt: "" }, ...conversations.filter(c => c.id !== "default")];

  useInput((input, key) => {
    if (key.escape) { onDismiss(); return; }
    if (key.upArrow) { setSel(s => Math.max(0, s - 1)); return; }
    if (key.downArrow) { setSel(s => Math.min(all.length - 1, s + 1)); return; }
    if (key.return && all[sel]) { onSwitch(all[sel].id); return; }
    if (input === "n") { onNew(); return; }
    if (input === "d" && all[sel] && all[sel].id !== "default") {
      client.deleteConversation(project, all[sel].id);
      onDismiss();
      return;
    }
  });

  return (
    <Box flexDirection="column" padding={1}>
      <Accent>Conversations</Accent>
      <Line />
      <Box marginY={1}><Dim>│ # │ Title │ Messages │ Last active</Dim></Box>
      {all.map((c, i) => (
        <Box key={c.id}>
          <Text color={i === sel ? "blue" : undefined}>
            {c.id === currentId ? "▶ " : "  "}
            {c.id.slice(0, 8)} │ {c.title.slice(0, 24).padEnd(24)} │ {String(c.messageCount).padStart(4)} │ {(c.lastAt || "—").slice(0, 19)}
          </Text>
        </Box>
      ))}
      <Line />
      <Box marginTop={1}><Dim>↑↓ nav · enter switch · n new · d delete · esc back</Dim></Box>
    </Box>
  );
}

// ── Artifacts View ────────────────────────────────────────────────────────────

function ArtifactView({ client, convId, onDismiss }: { client: ApiClient; convId: string; onDismiss: () => void }) {
  const [arts, setArts] = useState<{ id: string; type: string; title: string; content: string }[]>([]);
  const [sel, setSel] = useState(0);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    setLoading(true);
    try { const a = await client.getArtifacts(convId); setArts(a || []); } catch {}
    setLoading(false);
  }, [convId]);

  useEffect(() => { refresh(); }, []);

  useInput((_input, key) => {
    if (key.escape) { onDismiss(); return; }
    if (key.upArrow) { setSel(s => Math.max(0, s - 1)); return; }
    if (key.downArrow) { setSel(s => Math.min(arts.length - 1, s + 1)); return; }
    if (key.ctrl && key.return) { /* expand selected artifact */ return; }
  });

  const selected = arts[sel];

  return (
    <Box flexDirection="column" padding={1}>
      <Box marginBottom={1}><Accent>Artifacts</Accent><Spacer /><Dim>{arts.length} items</Dim></Box>
      <Line />
      {/* Tab bar */}
      <Box marginY={1} flexDirection="row">
        {arts.map((a, i) => (
          <Box key={a.id} marginRight={1}>
            <Text color={i === sel ? "blueBright" : undefined} bold={i === sel}>
              [{a.type}] {a.title.slice(0, 30)}
            </Text>
          </Box>
        ))}
      </Box>
      <Line />
      {/* Selected artifact content */}
      {loading ? <Box marginY={1}><Dim>Loading...</Dim></Box>
        : !selected ? <Box marginY={1}><Dim>No artifacts. The agent creates plans, todos, and notes as it works.</Dim></Box>
        : (
          <Box flexDirection="column" marginY={1}>
            <Text dimColor>── {selected.title} ──</Text>
            <Box marginTop={1}><Text>{renderMd(selected.content.slice(0, 2000))}</Text></Box>
            {selected.content.length > 2000 ? <Dim>...truncated ({selected.content.length} chars total)</Dim> : null}
          </Box>
        )
      }
      <Line />
      <Box marginTop={1} flexDirection="row">
        <Dim>↑↓ nav · esc back · type: </Dim>
        {arts.length > 0 ? <Dim>{arts.map(a => a.type).filter((v, i, s) => s.indexOf(v) === i).join(", ")}</Dim> : null}
      </Box>
    </Box>
  );
}

// ── Scheduler ────────────────────────────────────────────────────────────────

interface Job { id: string; name: string; cronExpr?: string; scheduleType: string; enabled: boolean; lastRunAt?: string; message: string; }

function Sched({ client, onBack }: { client: ApiClient; onBack: () => void }) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [sel, setSel] = useState(0);
  const [load, setLoad] = useState(true);
  const [notif, setNotif] = useState("");

  const refresh = useCallback(async () => {
    setLoad(true);
    try { const j = await client.getJobs(); const arr = Array.isArray(j) ? j : []; setJobs(arr); if (sel >= arr.length) setSel(Math.max(0, arr.length - 1)); } catch { setNotif("Load failed"); }
    setLoad(false);
  }, [sel]);

  useEffect(() => { refresh(); }, []);

  useInput((_input, key) => {
    if (key.escape) { onBack(); return; }
    if (key.upArrow) { setSel(s => Math.max(0, s - 1)); return; }
    if (key.downArrow) { setSel(s => Math.min(jobs.length - 1, s + 1)); return; }
    if (key.return && jobs[sel]) { client.updateJob(jobs[sel].id, { enabled: !jobs[sel].enabled }).then(() => { setNotif("Toggled"); refresh(); }); return; }
    if (key.delete && jobs[sel]) { client.deleteJob(jobs[sel].id).then(() => { setNotif("Deleted"); refresh(); }).catch(e => setNotif(String(e))); return; }
  });

  return (
    <Box flexDirection="column" paddingX={1}>
      <Box marginBottom={1}><Accent>pux Scheduler</Accent><Spacer /><Dim>{jobs?.length ?? 0} jobs</Dim></Box>
      <Line />
      <Box marginY={1}><Dim>ID       │ En │ Schedule      │ Last Run              │ Name</Dim></Box>
      {load ? <Dim>Loading...</Dim>
        : jobs.length === 0 ? <Dim>No jobs. Press d/t/r for actions.</Dim>
        : jobs.map((j, i) => (
            <Box key={j.id}>
              <Text color={i === sel ? "blue" : undefined}>
                {j.id.slice(0, 8)} │ {j.enabled ? <Text color="green">✓</Text> : <Text color="red">✗</Text>} │ {(j.cronExpr || j.scheduleType).slice(0, 13).padEnd(13)} │ {(j.lastRunAt || "never").slice(0, 19).padEnd(21)} │ {j.name}
              </Text>
            </Box>
          ))}
      <Line />
      <Box marginTop={1}><Dim>↑↓ nav · enter toggle · del delete · t trigger · r refresh · esc back</Dim></Box>
      {notif ? <Box marginTop={1}><Text color="green">{notif}</Text></Box> : null}
    </Box>
  );
}

// ── App ──────────────────────────────────────────────────────────────────────

interface AppProps { serverUrl: string; project: string; agentId?: string; }

export default function App({ serverUrl, project, agentId: initialAgentId = "default" }: AppProps) {
  const [state, dispatch] = useReducer(reduce, undefined, () => ({ ...init(), convId: initialAgentId }));
  const [input, setInput] = useState("");
  const [pasteExpanded, setPasteExpanded] = useState(false);
  const wasPasted = useRef(false);
  const [health, setHealth] = useState<{ llm: string; sandbox: string; version: string } | null>(null);
  const client = useRef(new ApiClient(serverUrl));
  const { exit } = useApp();
  const abort = useRef<AbortController | null>(null);
  const { stdout } = useStdout();
  const h = stdout?.rows ?? 40;
  const tabIdx = useRef(-1);       // tab completion cycle index
  const lastTab = useRef(0);       // timestamp of last tab press

  // ── load conversations ──
  useEffect(() => {
    client.current.getConversations().then((summaries) => {
      if (summaries && summaries.length > 0) {
        const convs: Conversation[] = summaries.map((s: any) => ({
          id: s.agentId,
          title: s.title || s.agentId?.slice(0, 8) || "untitled",
          lastMessage: s.lastMessage?.slice(0, 80) || "",
          messageCount: s.messageCount || 0,
          lastAt: s.lastAt || new Date().toISOString(),
        }));
        dispatch({ type: "LOAD_CONVS", convs });
        // Auto-switch to the first (most recent) conversation
        if (convs.length > 0 && convs[0]) {
          dispatch({ type: "SET_CONV", id: convs[0].id, messages: [] });
        }
      }
    }).catch(() => {});
  }, [project]);

  // ── health polling ──
  useEffect(() => {
    const poll = async () => {
      try {
        const res = await fetch(`${serverUrl}/api/health`);
        if (res.ok) {
          const data = await res.json();
          setHealth(data as any);
        }
      } catch { setHealth(null); }
    };
    poll();
    const iv = setInterval(poll, 30000);
    return () => clearInterval(iv);
  }, []);

  // ── send ──
  const send = useCallback(async (text: string) => {
    const t = text.trim();
    if (!t || state.streaming) return;
    setInput("");
    wasPasted.current = false;

    // Handle slash commands
    if (t.startsWith("/")) {
      if (executeSlash(t, dispatch, client.current, project, state.convId, exit)) return;
      // Unknown command — send as message
    }

    dispatch({ type: "USER", text: t });
    dispatch({ type: "STREAM" });

    const ctrl = new AbortController();
    abort.current = ctrl;

    try {
      const stream = client.current.streamPrompt({ message: t, project, agentId: state.convId });
      for await (const e of stream) {
        if (ctrl.signal.aborted) break;
        switch (e.type) {
          case "agent_spawned":
            // Sync server-assigned agentId
            const aid = (e.data as any).agentId as string;
            if (aid && aid !== state.convId) {
              dispatch({ type: "SYNC_AGENT", id: aid });
            }
            break;
          case "text_delta": dispatch({ type: "TEXT", text: ensureStr((e.data as any).text) }); break;
          case "thinking_delta": dispatch({ type: "THINK", text: ensureStr((e.data as any).text) }); break;
          case "tool_execution_start": dispatch({ type: "TOOL_ON", tool: e.data as any }); break;
          case "tool_execution_end": dispatch({ type: "TOOL_OFF", tool: e.data as any }); break;
          case "approval_request": dispatch({ type: "ASK", approval: e.data as any }); return;
          case "agent_end": {
            // Server sends {input, output} not {inputTokens, outputTokens}
            const d = e.data as any;
            dispatch({ type: "END", data: { inputTokens: d.input || 0, outputTokens: d.output || 0 } });
            break;
          }
          case "error": dispatch({ type: "ERR", error: ensureStr((e.data as any).error) }); break;
        }
      }
      dispatch({ type: "CLOSE" });
    } catch (err: any) {
      if (!ctrl.signal.aborted) dispatch({ type: "ERR", error: err.message || String(err) });
    }
  }, [state.streaming, project, state.convId, exit]);

  const approve = useCallback(async (a: "approve" | "deny" | "answer", msg?: string) => {
    if (!state.approval) return;
    const ap = state.approval;
    dispatch({ type: "OK" });
    await client.current.approve({ project, agentId: state.convId, requestId: ap.requestId, action: a, message: msg });
  }, [state.approval, project, state.convId]);

  const navHistory = useCallback((dir: -1 | 1) => {
    if (state.history.length === 0) return;
    const cur = state.histIdx === -1 ? (dir === -1 ? 0 : state.history.length - 1) : Math.max(0, Math.min(state.history.length - 1, state.histIdx + dir));
    dispatch({ type: "HIST_IDX", idx: dir === 1 && state.histIdx === state.history.length - 1 ? -1 : cur });
    setInput(state.history[cur] || "");
  }, [state.history, state.histIdx]);

  const switchConv = useCallback(async (id: string) => {
    try {
      const msgs = await client.current.getHistory(project, id);
      const mapped: Message[] = (msgs || []).map((m: any, i: number) => ({
        id: Date.now() + i,
        role: m.role === "user" ? "user" as const : "assistant" as const,
        content: m.content || m.text || "",
        thinking: m.thinking || "",
        tools: Array.isArray(m.toolCalls) ? m.toolCalls.map((t: any) => ({
          name: t.toolName || t.name || "unknown",
          id: t.toolId || t.id || `tool_${i}`,
          args: t.args || "",
          result: t.result || "",
          error: t.error || "",
          done: true,
        })) : [],
        timestamp: m.createdAt ? Date.parse(m.createdAt) : Date.now(),
      }));
      dispatch({ type: "SET_CONV", id, messages: mapped });
    } catch { /* fallback to empty */ dispatch({ type: "SET_CONV", id, messages: [] }); }
  }, [project]);

  // keyboard
  useEffect(() => {
    const handler = () => { if (state.streaming) { abort.current?.abort(); dispatch({ type: "ABORT" }); } else exit(); };
    process.on("SIGINT", handler);
    return () => { process.off("SIGINT", handler); };
  }, [state.streaming, exit]);

  useInput((inp, key) => {
    if (key.escape) { if (state.mode !== "chat") dispatch({ type: "MODE", mode: "chat" }); return; }
    // Up/Down arrow handling
    if (key.upArrow && !state.streaming && state.mode === "chat") {
      // With text + multi-line: merge last line into previous
      if (input && input.includes("\n")) {
        const lines = input.split("\n");
        const last = lines.pop() || "";
        setInput(lines.join("\n") + (last ? " " + last : ""));
        return;
      }
      // Empty input: history navigation
      if (!input && state.history.length > 0) { navHistory(-1); return; }
    }
    if (key.downArrow && !input && state.history.length > 0 && !state.streaming) { navHistory(1); return; }

    // Backspace at start of empty last line: merge with previous line
    if ((key.backspace || key.delete) && input.endsWith("\n") && !state.streaming) {
      setInput(input.slice(0, -1));
      return;
    }

    // Enter handling
    if (key.return && !state.streaming && state.mode === "chat") {
      // Was Shift+Enter, already handled by raw stdin handler above
      if (suppressEnterRef.current) { suppressEnterRef.current = false; return; }
      if (state.approval) {
        const v = input.trim().toLowerCase();
        if (v === "y" || v === "yes") approve("approve");
        else if (v === "n" || v === "no") approve("deny");
        else if (v.startsWith("a:") || v.startsWith("answer:")) approve("answer", v.replace(/^(a|answer):/, "").trim());
        setInput("");
        return;
      }
      // Bracketed paste: insert newlines, don't send
      if ((key as any).paste) { wasPasted.current = true; setInput(v => v + "\n"); return; }
      // Backslash at end = newline
      if (input.endsWith("\\")) { setInput(input.slice(0, -1) + "\n"); return; }
      // Plain Enter = send
      if (input.trim()) send(input);
      return;
    }

    if (key.ctrl && inp === "h") { dispatch({ type: "MODE", mode: state.mode === "help" ? "chat" : "help" }); return; }
    if (key.ctrl && inp === "j") { dispatch({ type: "MODE", mode: state.mode === "scheduler" ? "chat" : "scheduler" }); return; }
    if (key.ctrl && inp === "t") { dispatch({ type: "THINK_TGL" }); return; }
    if (key.ctrl && inp === "o") { setPasteExpanded(v => !v); return; }
  });

  // Tab completion for slash commands via raw stdin (TextInput consumes Tab before useInput)
  const { stdin } = useStdin();
  const suppressEnterRef = useRef(false);
  const suppressTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const setSuppress = () => {
    suppressEnterRef.current = true;
    if (suppressTimer.current) clearTimeout(suppressTimer.current);
    suppressTimer.current = setTimeout(() => { suppressEnterRef.current = false; suppressTimer.current = null; }, 200);
  };
  const inputRef = useRef(input);
  useEffect(() => { inputRef.current = input; }, [input]);
  const streamingRef = useRef(state.streaming);
  useEffect(() => { streamingRef.current = state.streaming; }, [state.streaming]);

  // Detect Shift+Enter via raw stdin.
  // Ink's useInput parser does NOT understand CSI-u escape sequences
  // (Kitty/Ghostty/WezTerm keyboard protocol) — so we must detect them
  // ourselves and directly call setInput.
  useEffect(() => {
    if (!stdin) return;
    const handler = (data: Buffer) => {
      if (streamingRef.current) return;
      const s = data.toString();
      // ---- Shift+Enter ----
      // CSI-u: \x1b[codepoint;mod u  — modifier is 1-indexed:
      //   1=none, 2=shift, 3=alt, 4=shift+alt, 5=ctrl, 6=ctrl+shift
      // shift = ((mod-1) & 1) === 1   (bit 0 of 0-based modifier)
      // Codepoints: 13=Enter, 57414=KP Enter
      const m = s.match(/^\x1b\[(13|57414);([\d]+)(u|~)/);
      if (m && m[2]) {
        const mod = parseInt(m[2], 10);
        if (((mod - 1) & 1) === 1) { setSuppress(); setInput((v: string) => v + "\n"); }
        return;
      }
      // xterm modifyOtherKeys: \x1b[27;mod;code~ (format 1) or \x1b[code;mod~ (format 2)
      // Same 1-indexed modifier encoding as CSI-u
      const m3 = s.match(/^\x1b\[27;(\d+);(\d+)~/);
      if (m3 && m3[1] && m3[2]) {
        const mod = parseInt(m3[1], 10);
        const code = parseInt(m3[2], 10);
        if ((code === 13 || code === 57414) && ((mod - 1) & 1) === 1) { setSuppress(); setInput((v: string) => v + "\n"); }
        return;
      }
      const m2 = s.match(/^\x1b\[(\d+);(\d+)~/);
      if (m2 && m2[1] && m2[2]) {
        const code = parseInt(m2[1], 10);
        const mod = parseInt(m2[2], 10);
        if ((code === 13 || code === 57414) && ((mod - 1) & 1) === 1) { setSuppress(); setInput((v: string) => v + "\n"); }
        return;
      }
      // Kitty custom mapping: \x1b\r
      if (s === "\x1b\r") { setSuppress(); setInput((v: string) => v + "\n"); return; }
      // Ctrl+J (universal newline) + Ghostty/Kitty Shift+Enter: \n
      // On 99% of terminals, Enter = \r in raw mode, so \n is always intentional newline
      if (s === "\n") { setSuppress(); setInput((v: string) => v + "\n"); return; }
      // ---- Tab completion ----
      if (s === "\t" && inputRef.current.startsWith("/")) {
        const partial = inputRef.current.trim().split(/\s+/)[0] || inputRef.current;
        const matches = SLASH_COMMANDS.filter(c => c.cmd.startsWith(partial));
        if (matches.length === 0) return;
        if (matches.length === 1) { setInput(matches[0]!.cmd + " "); tabIdx.current = -1; return; }
        const now = Date.now();
        if (now - lastTab.current > 1500) tabIdx.current = -1;
        lastTab.current = now;
        tabIdx.current = (tabIdx.current + 1) % matches.length;
        const cmd = matches[tabIdx.current];
        if (cmd) setInput(cmd.cmd + " ");
      }
    };
    stdin.on("data", handler);
    return () => { stdin.off("data", handler); };
  }, [stdin]); // stable registration — only register once

  // render
  if (state.mode === "help") return <HelpView onDismiss={() => dispatch({ type: "MODE", mode: "chat" })} />;
  if (state.mode === "scheduler") return <Sched client={client.current} onBack={() => dispatch({ type: "MODE", mode: "chat" })} />;
  if (state.mode === "history") return (
    <HistoryView client={client.current} project={project}
      conversations={state.conversations} currentId={state.convId}
      onSwitch={switchConv} onNew={() => dispatch({ type: "NEW_CONV" })}
      onDismiss={() => dispatch({ type: "MODE", mode: "chat" })} />
  );
  if (state.mode === "artifacts") return (
    <ArtifactView client={client.current} convId={state.convId}
      onDismiss={() => dispatch({ type: "MODE", mode: "chat" })} />
  );

  return (
    <Box flexDirection="column" minHeight={h} paddingX={1}>

      {/* BODY — split pane */}
      <Box flexDirection="row" flexGrow={1}>

        {/* LEFT SIDEBAR — tools + sub-agents */}
        <Box flexDirection="column" width="28%" paddingX={1}>
          <Text bold color="blueBright">Activity</Text>

          {/* Streaming status */}
          {state.streaming ? (
            <Box flexDirection="column" marginTop={1}>
              <Box>
                <Text color="green"><Spinner type="dots" /></Text>
                <Text color="green"> active</Text>
              </Box>
              {state.sThink && !state.sText ? (
                <Box marginTop={1}>
                  <Dim>thinking...</Dim>
                </Box>
              ) : null}
              {state.sText ? (
                <Box marginTop={1}>
                  <Dim>generating response</Dim>
                </Box>
              ) : null}
            </Box>
          ) : (
            <Box marginTop={1}>
              <Dim>idle</Dim>
            </Box>
          )}

          {/* Current tools running */}
          {state.sTools.length > 0 ? (
            <Box flexDirection="column" marginTop={2}>
              <Text bold color="yellow">Tools</Text>
              {state.sTools.map(t => (
                <Box key={t.name} flexDirection="row">
                  {t.done ? (
                    t.error ? <Text color="red">✗</Text> : <Text color="green">✓</Text>
                  ) : (
                    <Text color="yellow"><Spinner type="dots" /></Text>
                  )}
                  <Text dimColor> {t.name}</Text>
                </Box>
              ))}
            </Box>
          ) : null}

          {/* Browser/desktop indicators */}
          {state.sTools.filter(t => t.name.includes("desktop") || t.name.includes("browse") || t.name.includes("click") || t.name.includes("screenshot")).length > 0 ? (
            <Box flexDirection="column" marginTop={2}>
              <Text bold color="magenta">Visual</Text>
              {state.sTools.filter(t => t.name.includes("desktop") || t.name.includes("browse") || t.name.includes("click") || t.name.includes("screenshot")).map(t => (
                <Text key={t.name} dimColor>
                  {t.done ? "  " : "  "}{t.name} {t.done ? (t.error ? "failed" : "done") : ""}
                </Text>
              ))}
            </Box>
          ) : null}

          {/* Conversation list */}
          <Box flexDirection="column" marginTop={2}>
            <Text bold color="blueBright">Chats</Text>
            <Dim>{state.conversations.length} conversations</Dim>
            {state.conversations.slice(0, 6).map(c => (
              <Box key={c.id}>
                <Text dimColor>{c.id === state.convId ? "▶" : " "} {c.title.slice(0, 20)}</Text>
                <Spacer />
                <Text dimColor>{c.messageCount}</Text>
              </Box>
            ))}
          </Box>
        </Box>

        {/* RIGHT PANEL — messages: no clipping, terminal scrolls naturally */}
        <Box flexDirection="column" flexGrow={1} paddingX={1}>
          <Box flexDirection="column" flexGrow={1}>
            {state.messages.length === 0 && !state.streaming ? (
              <Box paddingY={1}><Dim>No messages. /help for commands.</Dim></Box>
            ) : (
              state.messages.map(m =>
                m.role === "user" ? (
                  <Box key={m.id} marginBottom={1}>
                    <Text color="cyan" bold>❯ </Text>
                    <Text color="cyan">{m.content}</Text>
                  </Box>
                ) : (
                  <Box key={m.id} flexDirection="column" marginBottom={1}>
                    {m.thinking ? <ThinkBlock text={m.thinking} expanded={state.thinkOpen} startMs={m.timestamp} /> : null}
                    {m.content ? <Box marginBottom={1}><Text>{renderMd(m.content)}</Text></Box> : null}
                    {m.tools.map((t, i) => (
                      <ToolCard key={i} tool={t} />
                    ))}
                    {m.tokens ? <Dim>↑{m.tokens.in} ↓{m.tokens.out}</Dim> : null}
                  </Box>
                )
              )
            )}
            {state.streaming ? (
              <Box flexDirection="column" marginBottom={1}>
                {state.sThink ? <ThinkBlock text={state.sThink} expanded={state.thinkOpen} startMs={state.thinkStart} /> : null}
                {state.sText ? <Box marginBottom={1}><Text>{state.sText}</Text></Box> : null}
                {state.sTools.map((t, i) => (
                  <ToolCard key={i} tool={t} />
                ))}
                {!state.sText && !state.sThink && state.sTools.length === 0 ? (
                  <Box><Text color="green"><Spinner type="dots" /></Text><Dim> thinking...</Dim></Box>
                ) : !state.sText && state.sTools.length > 0 ? null : (
                  state.sText ? <Box><Text color="green"><Spinner type="dots" /></Text><Dim> generating...</Dim></Box> : null
                )}
              </Box>
            ) : null}
          </Box>
        </Box>
      </Box>

      {/* Approval prompt */}
      {state.approval ? <Approve approval={state.approval} onR={approve} /> : null}

      {/* INPUT */}
      {!state.approval ? (
        <Box flexDirection="column">
          <SlashHint input={input} />
          {/* Dynamic input box */}
          {(() => {
            const lines = input.split("\n");
            // Box grows with content, capped for reasonable height
            const visibleLines = Math.min(10, lines.length);
            return (
              <Box borderStyle="single" borderColor="cyan" paddingX={1} height={visibleLines + 2}>
                <Box flexDirection="column" flexGrow={1} justifyContent="flex-end">
                  {/* Paste placeholder or multi-line display */}
                  {wasPasted.current && lines.length > 3 && !pasteExpanded ? (
                    <Box>
                      <Text color="yellow">[Pasted ~{lines.length} lines</Text>
                      <Text dimColor> — Ctrl+O to expand]</Text>
                    </Box>
                  ) : lines.length > 1 ? (
                    <Box flexDirection="column">
                      {lines.slice(0, -1).map((line, i) => (
                        <Text key={i} dimColor>{line || " "}</Text>
                      ))}
                    </Box>
                  ) : null}
                  {/* Active line */}
                  <Box flexDirection="row">
                    <Box flexGrow={1}>
                      <TextInput
                        value={lines[lines.length - 1] || ""}
                        onChange={(newLast) => {
                          const prev = lines.slice(0, -1).join("\n");
                          const full = prev ? prev + "\n" + newLast : newLast;
                          setInput(full);
                          setPasteExpanded(false);
                          if (full.split("\n").length <= 3) wasPasted.current = false;
                        }}
                        onSubmit={() => {}} // no-op: handled by useInput above
                        placeholder={lines.length <= 1 ? "Type a message..." : undefined}
                        focus={!state.streaming}
                        showCursor={!state.streaming}
                      />
                    </Box>
                  </Box>
                </Box>
              </Box>
            );
          })()}
        </Box>
      ) : null}

      {/* FOOTER — health status */}
      <Box flexDirection="row" paddingX={1}>
        <Spacer />
        {health ? (
          <Dim>
            v{health.version}
            {"  "}<Text color={health.llm === "healthy" ? "green" : "red"}>{health.llm === "healthy" ? "llm" : "llm down"}</Text>
            {"  "}<Text color={health.sandbox === "available" ? "green" : "yellow"}>{health.sandbox === "available" ? "sandbox" : "sandbox busy"}</Text>
          </Dim>
        ) : <Dim>offline</Dim>}
      </Box>
    </Box>
  );
}
