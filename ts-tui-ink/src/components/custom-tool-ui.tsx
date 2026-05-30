/**
 * Custom tool UIs — specialized renderers for specific tool types.
 *
 * Uses makeAssistantToolUI from @assistant-ui/react-ink to register
 * custom renderers per tool name. Each tool gets its own visual treatment.
 *
 * Phase 3: DelegateToolUI now shows sub-agent tool calls from Zustand store.
 *
 * Note: status is an object { type: "running" | "complete" | "incomplete" | "requires-action" }
 * not a string. Access status.type for comparison.
 */

import React, { useState, useEffect, useMemo } from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import {
	makeAssistantToolUI,
	makeAssistantTool,
	useAuiState,
	DiffView,
} from "@assistant-ui/react-ink";
import { usePuxStore, getToolArgPreview } from "@pux/shared";
import type { SubAgentRecord } from "@pux/shared";
import { TerminalImage } from "./terminal-image.js";
import { useColors, symbols, BLACK_CIRCLE, BLOCKQUOTE_BAR } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

// ── Bash execution tool UI ──

export const BashToolUI = makeAssistantToolUI({
	toolName: "bash",
	render: ({ args, result, isError, status }) => {
		const colors = useColors();
		const command = (args as any)?.command || (args as any)?.cmd || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";
		const cmdPreview = command.length > 80 ? command.slice(0, 77) + "..." : command;

		return (
			<Box paddingLeft={2} marginBottom={1}>
				<Text color={isError ? colors.error : isDone ? colors.success : colors.running}>
					{BLACK_CIRCLE}{" "}
				</Text>
				<Text bold color={isRunning ? colors.running : undefined}>
					bash
				</Text>
				<Text color="gray">({cmdPreview})</Text>
				{isDone && !isError && <Text color="gray"> done</Text>}
				{isError && <Text color={colors.error}> failed</Text>}
			</Box>
		);
	},
});

// ── Delegate/sub-agent tool UI ──
// Phase 3: Shows sub-agent tool calls from the Zustand agent store.

export const DelegateToolUI = makeAssistantToolUI({
	toolName: "delegate_to",
	render: ({ args, result, status, toolCallId }) => {
		return (
			<DelegateRenderer
				toolName="delegate_to"
				args={args}
				result={result}
				status={status}
				toolCallId={toolCallId}
			/>
		);
	},
});

export const DelegateAsyncToolUI = makeAssistantToolUI({
	toolName: "delegate_async",
	render: ({ args, result, status, toolCallId }) => {
		return (
			<DelegateRenderer
				toolName="delegate_async"
				args={args}
				result={result}
				status={status}
				toolCallId={toolCallId}
			/>
		);
	},
});

// Truncate at word boundary — never cuts mid-word
function trunc(s: string, max: number): string {
	if (s.length <= max) return s;
	const cut = s.slice(0, max - 1);
	const lastSpace = cut.lastIndexOf(" ");
	if (lastSpace < max * 0.5) return cut + "…";
	return cut.slice(0, lastSpace) + "…";
}

function DelegateRenderer({
	toolName,
	args,
	result,
	status,
	toolCallId,
}: {
	toolName: string;
	args: unknown;
	result: unknown;
	status: { type: string };
	toolCallId?: string;
}) {
	const colors = useColors();
	const { cols } = useTerminalSize();
	const role = (args as any)?.role || (args as any)?.instructions || "agent";
	const label = `${toolName} → ${role}`;
	const task = (args as any)?.task || (args as any)?.prompt || "";
	const injectedId = (args as any)?.__agentId as string | undefined;
	const isDone = status.type === "complete";
	const isRunning = status.type === "running";
	const isError = status.type === "incomplete";

	// Look up sub-agent details from Zustand store
	// Priority: exact ID match > name+task match > running fallback
	const agents = usePuxStore((s) => s.agents);
	const agentState = useMemo(() => {
		if (injectedId) {
			const byId = agents.get(injectedId);
			if (byId) return byId;
		}
		const candidates = [...agents.values()].filter(a => a.agentName === role);
		if (candidates.length === 0) return undefined;
		if (candidates.length === 1) return candidates[0];
		const byTask = candidates.find(
			a => task.startsWith(a.task) || a.task.startsWith(task),
		);
		return byTask ?? candidates.find(a => a.status === "running") ?? candidates[0];
	}, [agents, role, task, injectedId]);

	// Persisted sub-agent trace from tool_calls JSON (available after reload)
	const persistedSubAgent = (args as any)?.__subAgent as SubAgentRecord | undefined;

	// Priority: live Zustand state → persisted subAgent from DB
	const toolCalls = agentState?.toolCalls
		?? persistedSubAgent?.toolCalls?.map(tc => ({
			toolName: tc.name,
			args: tc.args,
			result: tc.result,
			isError: !!tc.error,
			timestamp: 0,
			endedAt: tc.result != null ? 0 : undefined,
		}))
		?? [];
	const thinkingText = agentState?.thinkingText ?? persistedSubAgent?.thinking;
	const agentText = agentState?.text ?? persistedSubAgent?.text;
	const subToolCount = toolCalls.length;

	// Width budget for tool call lines
	const toolIndent = 7;
	const maxArgLen = Math.max(15, cols - toolIndent - 20);
	const headerOverhead = 6;
	const maxTaskLen = Math.max(20, cols - headerOverhead - label.length - 10);
	const taskPreview = trunc(task, Math.min(maxTaskLen, 50));

	// Duration
	const duration = agentState
		? agentState.endedAt
			? `${((agentState.endedAt - agentState.startedAt) / 1000).toFixed(1)}s`
			: `${((Date.now() - agentState.startedAt) / 1000).toFixed(1)}s`
		: "";

	// Tick every second while running to update duration
	const [, setTick] = useState(0);
	useEffect(() => {
		if (!isRunning) return;
		const timer = setInterval(() => setTick((t) => t + 1), 1000);
		return () => clearInterval(timer);
	}, [isRunning]);

	// ── Collapsed summary when done ──
	if (isDone) {
		const doneSuffix = subToolCount > 0
			? ` done · ${subToolCount} tool${subToolCount !== 1 ? "s" : ""} · ${duration}`
			: " done";
		return (
			<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
				<Text wrap="truncate-end">
					<Text color={colors.success}>{BLACK_CIRCLE} </Text>
					<Text bold color={colors.brand}>{label}</Text>
					{taskPreview && <Text color="gray"> {taskPreview}</Text>}
					<Text color="gray">{doneSuffix}</Text>
				</Text>
				{/* Agent output preview when done — first 3 lines */}
				{agentText && agentText.trim() && (
					<Box paddingLeft={4}>
						<Text dimColor color="gray">
							{agentText.trim().split("\n").slice(0, 3).map((line, i, arr) =>
								`${BLOCKQUOTE_BAR} ${trunc(line, cols - 6)}${i < arr.length - 1 ? "\n" : ""}`
							).join("")}
							{agentText.trim().split("\n").length > 3 ? `\n${BLOCKQUOTE_BAR} ...` : ""}
						</Text>
					</Box>
				)}
			</Box>
		);
	}

	// ── Error: tool failed before producing a result ──
	if (isError) {
		const errMsg = typeof result === "string" ? result : "";
		return (
			<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
				<Text wrap="truncate-end">
					<Text color={colors.error}>{symbols.toolError} </Text>
					<Text bold color={colors.brand}>{label}</Text>
					{taskPreview && <Text color="gray"> {taskPreview}</Text>}
					<Text color={colors.error}> failed</Text>
				</Text>
				{errMsg && (
					<Box paddingLeft={4}>
						<Text dimColor color={colors.error}>{trunc(errMsg, cols - 6)}</Text>
					</Box>
				)}
			</Box>
		);
	}

	// ── Running: show nested tool snippets ──
	const maxShow = 5;
	const visibleTools = toolCalls.length > maxShow
		? toolCalls.slice(-maxShow)
		: toolCalls;
	const hiddenCount = toolCalls.length - visibleTools.length;

	return (
		<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
			<Text wrap="truncate-end">
				<Text color={colors.running}>
					{symbols.toolRunning}{" "}
				</Text>
				<Text bold color={colors.brand}>{label}</Text>
				{taskPreview && <Text color="gray"> {taskPreview}</Text>}
				{subToolCount > 0 && (
					<Text color="gray"> · {subToolCount} tool{subToolCount !== 1 ? "s" : ""}</Text>
				)}
			</Text>

			{hiddenCount > 0 && (
				<Text dimColor color="gray">
					{"  └ "}{symbols.dot} {hiddenCount} earlier
				</Text>
			)}

			{visibleTools.map((tc, i) => {
				const isActive = !tc.endedAt;
				const isLast = i === visibleTools.length - 1;
				const rawArg = getToolArgPreview(tc.toolName, tc.args as Record<string, unknown> | undefined, maxArgLen);
				const argPreview = trunc(rawArg, maxArgLen);
				const sym = tc.isError ? symbols.toolError : tc.endedAt ? symbols.toolDone : symbols.toolRunning;
				return (
					<Text key={`${tc.toolName}-${tc.timestamp}-${i}`} wrap="truncate-end">
						<Text dimColor color="gray">{"  └ "}</Text>
						<Text color={tc.isError ? colors.error : tc.endedAt ? colors.success : colors.running}>
							{sym}
						</Text>
						<Text> </Text>
						<Text bold color={isActive ? colors.running : undefined}>
							{tc.toolName}
						</Text>
						{argPreview && <Text color="gray"> {argPreview}</Text>}
						{isActive && isLast && isRunning && (
							<Text color={colors.running}> <Spinner type="dots" /></Text>
						)}
					</Text>
				);
			})}

			{toolCalls.length === 0 && !thinkingText && (
				<Text dimColor color="gray">{"  └ "}starting...</Text>
			)}

			{/* Show agent thinking preview while running */}
			{thinkingText && isRunning && (
				<Text dimColor color="gray">
					{"  └ "}{BLOCKQUOTE_BAR} {trunc(thinkingText.split("\n").pop() || thinkingText, cols - 8)}
				</Text>
			)}
		</Box>
	);
}

// ── File write tool UI (shows full-file diff) ──

export const FileEditToolUI = makeAssistantToolUI({
	toolName: "write_file",
	render: ({ args, isError, status }) => {
		const colors = useColors();
		const path = (args as any)?.path || (args as any)?.file_path || "";
		const content = (args as any)?.content || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";

		return (
			<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
				<Box>
					<Text
						color={
							isError
								? colors.error
								: isDone
									? colors.success
									: colors.running
						}
					>
						{BLACK_CIRCLE}{" "}
					</Text>
					<Text bold color={isRunning ? colors.running : undefined}>
						write
					</Text>
					<Text color="gray"> {path.slice(0, 60)}</Text>
					{isDone && !isError && (
						<Text color={colors.success}> {symbols.check}</Text>
					)}
				</Box>
				{isDone && !isError && content && (
					<Box paddingLeft={0} marginTop={1}>
						<DiffView
							newFile={{ content, name: path }}
							showLineNumbers={true}
							contextLines={3}
							maxLines={50}
						/>
					</Box>
				)}
			</Box>
		);
	},
});

// ── File edit tool UI (shows old→new replacement diff) ──

function EditDiffRenderer({
	args,
	isError,
	status,
}: {
	args: unknown;
	isError?: boolean;
	status: { type: string };
}) {
	const colors = useColors();
	const path = (args as any)?.path || (args as any)?.file_path || "";
	const oldStr = (args as any)?.old_string || "";
	const newStr = (args as any)?.new_string || "";
	const isDone = status.type === "complete";
	const isRunning = status.type === "running";
	const oldLines = oldStr.split("\n");
	const newLines = newStr.split("\n");

	return (
		<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
			<Box>
				<Text
					color={
						isError
							? colors.error
							: isDone
								? colors.success
								: colors.running
					}
				>
					{BLACK_CIRCLE}{" "}
				</Text>
				<Text bold color={isRunning ? colors.running : undefined}>
					edit
				</Text>
				<Text color="gray"> {path.slice(0, 60)}</Text>
				{isDone && !isError && (
					<Text color={colors.success}> {symbols.check}</Text>
				)}
			</Box>
			{isDone && !isError && (oldStr || newStr) && (
				<Box flexDirection="column" paddingLeft={0} marginTop={1}>
					{oldStr && (
						<Box flexDirection="column">
							{oldLines.slice(0, 8).map((line: string, i: number) => (
								<Text key={i} color="red">
									{BLOCKQUOTE_BAR}- {line}
								</Text>
							))}
						</Box>
					)}
					{newStr && (
						<Box flexDirection="column">
							{newLines.slice(0, 8).map((line: string, i: number) => (
								<Text key={i} color="green">
									{BLOCKQUOTE_BAR}+ {line}
								</Text>
							))}
						</Box>
					)}
					{(oldLines.length > 8 || newLines.length > 8) && (
						<Text dimColor color="gray">
							{"  "}... diff truncated
						</Text>
					)}
				</Box>
			)}
		</Box>
	);
}

export const FileEditPatchToolUI = makeAssistantToolUI({
	toolName: "file_edit",
	render: ({ args, isError, status }) => (
		<EditDiffRenderer args={args} isError={isError} status={status} />
	),
});

// ── File read tool UI ──

export const FileReadToolUI = makeAssistantToolUI({
	toolName: "read_file",
	render: ({ args, status }) => {
		const colors = useColors();
		const path = (args as any)?.path || (args as any)?.file_path || "";
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";

		return (
			<Box paddingLeft={2} marginBottom={1}>
				<Text
					color={isDone ? colors.success : colors.running}
				>
					{BLACK_CIRCLE}{" "}
				</Text>
				<Text bold color={isRunning ? colors.running : undefined}>
					read
				</Text>
				<Text color="gray"> {path.slice(0, 60)}</Text>
			</Box>
		);
	},
});

// ── Client-side /exit tool (for slash command passthrough) ──

export const ExitTool = makeAssistantTool({
	toolName: "/exit",
	description: "Exit the TUI",
	parameters: {},
	execute: async () => {
		return "exit";
	},
});

// ── Image Data URI Detection ──

const DATA_URI_RE = /^data:image\/(png|jpeg|jpg|gif|webp);base64,/;

function tryExtractImageDataURI(result: unknown): string | null {
  if (typeof result === "string") {
    const trimmed = result.trim();
    // Common patterns: standalone data URI, or data URI embedded in backtick block
    const match = trimmed.match(DATA_URI_RE);
    if (match) {
      // Return the full data URI from the start
      const endIdx = trimmed.indexOf('"', match.index! + match[0].length);
      return endIdx > 0 ? trimmed.slice(match.index, endIdx) : trimmed;
    }
    // Try to find any data URI in the string
    const anyMatch = trimmed.match(DATA_URI_RE);
    if (anyMatch) {
      const start = anyMatch.index!;
      const end = trimmed.indexOf(" ", start + anyMatch[0].length);
      return end > 0 ? trimmed.slice(start, end) : trimmed.slice(start);
    }
  }
  if (result && typeof result === "object") {
    const obj = result as Record<string, unknown>;
    for (const val of Object.values(obj)) {
      const uri = tryExtractImageDataURI(val);
      if (uri) return uri;
    }
  }
  return null;
}

// ── Screenshot / Image Tool UI ──

function extractScreenshotURI(result: unknown): string | null {
  // Direct data URI
  const direct = tryExtractImageDataURI(result);
  if (direct) return direct;

  // Structured JSON result with screenshot field
  if (result && typeof result === "object" && !Array.isArray(result)) {
    const obj = result as Record<string, unknown>;
    if (typeof obj.screenshot === "string") {
      const dataUri = obj.screenshot as string;
      if (dataUri.startsWith("data:image")) return dataUri;
      // Maybe it's raw base64 — wrap it as PNG
      if (/^[A-Za-z0-9+/=]+$/.test(dataUri) && dataUri.length > 100) {
        return `data:image/png;base64,${dataUri}`;
      }
    }
    // Check nested result object
    if (obj.result && typeof obj.result === "object") {
      return extractScreenshotURI(obj.result);
    }
  }

  return null;
}

function ScreenshotRenderer(p: { result?: unknown; isError?: boolean; status: { type: string } }) {
  const colors = useColors();
  const isDone = p.status.type === "complete";
  const imageUri = isDone && !p.isError ? extractScreenshotURI(p.result) : null;
  return (
    <Box flexDirection="column" paddingLeft={2} marginBottom={1}>
      <Box>
        <Text color={p.isError ? colors.error : isDone ? colors.success : colors.running}>
          {p.isError ? symbols.toolError : isDone ? symbols.toolDone : symbols.toolRunning}
        </Text>
        <Text> </Text>
        <Text bold>screenshot</Text>
      </Box>
      {imageUri && !p.isError && (
        <Box paddingLeft={2} marginTop={1}>
          <TerminalImage image={imageUri} filename="screenshot.png" />
        </Box>
      )}
      {!imageUri && isDone && !p.isError && (
        <Box paddingLeft={2}>
          <Text dimColor>  {BLOCKQUOTE_BAR} (image not available in terminal)</Text>
        </Box>
      )}
      {p.isError && (
        <Box paddingLeft={2}>
          <Text color={colors.error}>  {symbols.cross} failed</Text>
        </Box>
      )}
    </Box>
  );
}

export const ScreenshotToolUI = makeAssistantToolUI({
  toolName: "screenshot",
  render: ScreenshotRenderer,
});

const DesktopScreenshotToolUI = makeAssistantToolUI({
  toolName: "desktop_screenshot",
  render: ScreenshotRenderer,
});
const ComputerScreenshotToolUI = makeAssistantToolUI({
  toolName: "computer_screenshot",
  render: ScreenshotRenderer,
});
const TakeScreenshotToolUI = makeAssistantToolUI({
  toolName: "take_screenshot",
  render: ScreenshotRenderer,
});
const BrowserScreenshotToolUI = makeAssistantToolUI({
  toolName: "browser_screenshot",
  render: ScreenshotRenderer,
});
const WebScreenshotToolUI = makeAssistantToolUI({
  toolName: "web_screenshot",
  render: ScreenshotRenderer,
});
const ObserveToolUI = makeAssistantToolUI({
  toolName: "observe",
  render: ScreenshotRenderer,
});
const DesktopObserveToolUI = makeAssistantToolUI({
  toolName: "desktop_observe",
  render: ScreenshotRenderer,
});

// ── Todo list tool UI ──

type TodoItem = { content?: string; status?: string };

export const TodoToolUI = makeAssistantToolUI({
	toolName: "todo",
	render: ({ args, result, status }) => {
		const colors = useColors();
		const isDone = status.type === "complete";
		const isRunning = status.type === "running";

		// Parse todos from args (while running) or result (when complete)
		let todos: TodoItem[] = [];
		if (isDone && result && typeof result === "object") {
			const r = result as { todos?: TodoItem[] };
			todos = Array.isArray(r.todos) ? r.todos : [];
		} else if (args && typeof args === "object") {
			const a = args as { todos?: TodoItem[] };
			todos = Array.isArray(a.todos) ? a.todos : [];
		}

		if (isRunning && todos.length === 0) {
			return (
				<Box paddingLeft={2} marginBottom={1}>
					<Text color={colors.running}>{BLACK_CIRCLE} </Text>
					<Text bold color={colors.running}>todo</Text>
					<Text color="gray"> loading...</Text>
				</Box>
			);
		}

		if (todos.length === 0) return null;

		return (
			<Box flexDirection="column" paddingLeft={2} marginBottom={1}>
				<Box>
					<Text color={isDone ? colors.success : colors.running}>
						{BLACK_CIRCLE}{" "}
					</Text>
					<Text bold color={isRunning ? colors.running : undefined}>
						todo
					</Text>
					<Text color="gray"> {todos.length} item{todos.length !== 1 ? "s" : ""}</Text>
				</Box>
				{todos.map((item, i) => {
					const s = item.status ?? "pending";
					let icon = "○";
					let color = "gray";
					if (s === "completed") { icon = symbols.check; color = colors.success; }
					else if (s === "in_progress") { icon = "●"; color = colors.running; }
					return (
						<Box key={i} paddingLeft={2}>
							<Text color={color}>{icon} </Text>
							<Text
								color={s === "completed" ? "gray" : undefined}
								strikethrough={s === "completed"}
							>
								{item.content ?? "untitled"}
							</Text>
						</Box>
					);
				})}
			</Box>
		);
	},
});

// ── Tool Registry — mount inside AssistantRuntimeProvider ──
// Each makeAssistantToolUI returns a component that registers
// itself via hooks when mounted within the runtime context.

export function ToolRegistry() {
	return (
		<>
			<BashToolUI />
			<DelegateToolUI />
			<DelegateAsyncToolUI />
			<FileEditToolUI />
			<FileEditPatchToolUI />
			{/* Model sometimes calls edit_file instead of file_edit */}
			<FileEditPatchToolAliasUI />
			<FileReadToolUI />
			<ScreenshotToolUI />
			<DesktopScreenshotToolUI />
			<ComputerScreenshotToolUI />
			<TakeScreenshotToolUI />
			<BrowserScreenshotToolUI />
			<WebScreenshotToolUI />
			<ObserveToolUI />
			<DesktopObserveToolUI />
			<TodoToolUI />
		</>
	);
}

// Also register under the "edit_file" alias that some models call
const FileEditPatchToolAliasUI = makeAssistantToolUI({
	toolName: "edit_file",
	render: ({ args, isError, status }) => (
		<EditDiffRenderer args={args} isError={isError} status={status} />
	),
});
