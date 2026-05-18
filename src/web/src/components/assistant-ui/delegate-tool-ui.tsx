"use client";

import { makeAssistantToolUI } from "@assistant-ui/react";
import { usePuxStore } from "@/lib/pux-store";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { useCollapsibleRoot } from "./use-collapsible";
import {
	Bot,
	CheckCircle,
	ChevronDownIcon,
	Loader2,
	XCircle,
} from "lucide-react";
import type { ToolCallRecord } from "@/lib/pux-store";

// ── Human-readable tool labels ──

const TOOL_LABELS: Record<string, string> = {
	browse_to: "Navigated to URL",
	snapshot_a11y: "Captured accessibility tree",
	click_element: "Clicked element",
	type_text: "Typed text",
	scroll_page: "Scrolled page",
	web_search: "Searched the web",
	web_scrape: "Scraped page content",
	analyze_image: "Analyzed image",
	transcribe_audio: "Transcribed audio",
	bash: "Ran command",
	file_read: "Read file",
	file_write: "Wrote file",
	file_edit: "Edited file",
	memory: "Updated memory",
	ask_user: "Asked for input",
	delegate_to: "Delegated to agent",
	delegate_async: "Started background task",
	collect_results: "Collected results",
};

function toolLabel(name: string): string {
	return TOOL_LABELS[name] ?? name;
}

function toolArgPreview(name: string, args?: unknown): string {
	if (!args || typeof args !== "object") return "";
	const a = args as Record<string, unknown>;
	switch (name) {
		case "browse_to":
		case "web_scrape":
			return String(a.url || a.address || "");
		case "web_search":
			return String(a.query || "");
		case "bash":
			return String(a.command || "").slice(0, 60);
		case "click_element":
			return String(a.selector || a.ref || "");
		case "type_text":
			return String(a.text || "").slice(0, 40);
		case "file_read":
		case "file_write":
		case "file_edit":
			return String(a.path || a.file_path || "");
		case "analyze_image":
			return String(a.prompt || a.imageSource || "").slice(0, 40);
		default:
			return "";
	}
}

function formatDuration(ms: number): string {
	if (ms < 1000) return `${ms}ms`;
	return `${(ms / 1000).toFixed(1)}s`;
}

// ── Sub-agent tool row ──

function SubAgentToolRow({ tool }: { tool: ToolCallRecord }) {
	const endedAt = tool.endedAt;
	const duration = endedAt ? endedAt - tool.timestamp : null;
	const preview = toolArgPreview(tool.toolName, tool.args);
	const hasError = tool.isError;

	return (
		<div className="flex items-center gap-2 px-4 py-1.5 text-xs">
			<span
				className={cn(
					"size-1.5 shrink-0 rounded-full",
					hasError ? "bg-red-500" : endedAt ? "bg-green-500" : "bg-blue-500 animate-pulse",
				)}
			/>
			<span className="font-medium text-muted-foreground">
				{toolLabel(tool.toolName)}
			</span>
			{preview && (
				<span className="truncate text-dim max-w-[260px]">
					{preview}
				</span>
			)}
			{duration !== null && (
				<span className="ml-auto text-dim tabular-nums">
					{formatDuration(duration)}
				</span>
			)}
		</div>
	);
}

// ── Main delegate renderer ──

function DelegateRenderer({
	args,
	status,
}: {
	args: Record<string, any>;
	result?: any;
	status?: { type: string };
}) {
	const agentName = (args.agent_id as string) || (args.agent as string) || "agent";
	const task = (args.task as string) || (args.prompt as string) || "";
	const isRunning = status?.type === "running";
	const isComplete = status?.type === "complete";
	const isError = status?.type === "error";

	const agents = usePuxStore((s) => s.agents);
	const agentState = [...agents.values()].find(
		(a) => a.agentName === agentName && a.task === task,
	);
	const toolCalls = agentState?.toolCalls ?? [];
	const subToolCount = toolCalls.length;

	// Auto-expand while running, collapse when done
	const { collapsibleRef, isOpen, handleOpenChange, animationStyle } =
		useCollapsibleRoot(isRunning);

	const statusLabel = isRunning
		? "working..."
		: isComplete
			? "done"
			: isError
				? "failed"
				: "";

	const elapsed = agentState
		? (agentState.endedAt ?? Date.now()) - agentState.startedAt
		: 0;

	return (
		<Collapsible
			ref={collapsibleRef}
			open={isOpen}
			onOpenChange={handleOpenChange}
			className="my-2 w-full rounded-lg border border-border"
			style={animationStyle}
		>
			{/* Header */}
			<div className="flex items-center gap-2 px-4 py-3 text-sm">
				{isRunning ? (
					<Loader2 size={14} className="animate-spin text-blue-500" />
				) : isError ? (
					<XCircle size={14} className="text-red-500" />
				) : (
					<CheckCircle size={14} className="text-green-500" />
				)}
				<Bot size={14} className="text-muted-foreground" />
				<span className="font-medium">{agentName}</span>
				{task && (
					<span className="text-muted-foreground truncate max-w-[300px]">
						{task}
					</span>
				)}
				<span className="text-xs text-muted-foreground ml-auto">
					{statusLabel}
				</span>
				{subToolCount > 0 && (
					<span className="text-xs text-dim">
						{"\u00B7"} {subToolCount} tool{subToolCount !== 1 ? "s" : ""}
					</span>
				)}
				{elapsed > 0 && !isRunning && (
					<span className="text-xs text-dim tabular-nums">
						{formatDuration(elapsed)}
					</span>
				)}
				{/* Expand/collapse chevron — only show when there are sub-tools */}
				{subToolCount > 0 && (
					<CollapsibleTrigger asChild>
						<button className="p-0.5 rounded hover:bg-accent/50 transition-colors">
							<ChevronDownIcon
								className={cn(
									"size-3.5 text-muted-foreground transition-transform duration-200 ease-out",
									"group-data-[state=closed]/trigger:-rotate-90",
								)}
							/>
						</button>
					</CollapsibleTrigger>
				)}
			</div>

			{/* Collapsible tool list */}
			{subToolCount > 0 && (
				<CollapsibleContent
					className={cn(
						"overflow-hidden text-sm outline-none",
						"data-[state=closed]:animate-collapsible-up",
						"data-[state=open]:animate-collapsible-down",
						"data-[state=closed]:fill-mode-forwards",
						"data-[state=closed]:pointer-events-none",
						"data-[state=open]:duration-200",
						"data-[state=closed]:duration-200",
					)}
				>
					<div className="border-t border-border py-2">
						{toolCalls.map((tool, i) => (
							<SubAgentToolRow key={`${tool.toolName}-${tool.timestamp}-${i}`} tool={tool} />
						))}
					</div>
				</CollapsibleContent>
			)}
		</Collapsible>
	);
}

export const DelegateToolUI = makeAssistantToolUI({
	toolName: "delegate_to",
	render: DelegateRenderer,
});

export const DelegateAsyncToolUI = makeAssistantToolUI({
	toolName: "delegate_async",
	render: DelegateRenderer,
});
