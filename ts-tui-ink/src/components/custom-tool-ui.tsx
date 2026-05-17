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

import React from "react";
import { Box, Text } from "ink";
import {
	makeAssistantToolUI,
	makeAssistantTool,
	useAuiState,
	DiffView,
} from "@assistant-ui/react-ink";
import { usePuxStore } from "@pux/shared";
import { TerminalImage } from "./terminal-image.js";
import { useColors, symbols, BLACK_CIRCLE, BLOCKQUOTE_BAR } from "../theme.js";

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
				args={args}
				result={result}
				status={status}
				toolCallId={toolCallId}
			/>
		);
	},
});

function DelegateRenderer({
	args,
	result,
	status,
	toolCallId,
}: {
	args: unknown;
	result: unknown;
	status: { type: string };
	toolCallId?: string;
}) {
	const colors = useColors();
	const agentName = (args as any)?.agent_id || (args as any)?.agent || "agent";
	const task = (args as any)?.task || (args as any)?.prompt || "";
	const isDone = status.type === "complete";
	const isRunning = status.type === "running";

	// Look up sub-agent details from Zustand store
	const agents = usePuxStore((s) => s.agents);
	const agentState = [...agents.values()].find(
		(a) => a.agentName === agentName && a.task === task
	);

	// Count sub-agent tool calls
	const subToolCount = agentState?.toolCalls.length ?? 0;
	const taskPreview = task.length > 40 ? task.slice(0, 37) + "..." : task;

	return (
		<Box paddingLeft={2} marginBottom={1}>
			<Text color={isDone ? colors.success : colors.running}>
				{BLACK_CIRCLE}{" "}
			</Text>
			<Text bold color={colors.brand}>
				{agentName}
			</Text>
			{taskPreview && <Text color="gray">({taskPreview})</Text>}
			<Text color="gray">
				{isDone ? " done" : isRunning ? " working..." : ""}
			</Text>
			{subToolCount > 0 && (
				<Text color="gray"> {symbols.dot} {subToolCount} tools</Text>
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
