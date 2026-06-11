"use client";

import { useState } from "react";
import { makeAssistantToolUI } from "@assistant-ui/react";
import { usePuxStore } from "@/lib/pux-store";
import { cn } from "@/lib/utils";
import {
	Bot,
	CheckCircle,
	ChevronDownIcon,
	Loader2,
	XCircle,
	// Tool-type icons
	Terminal,
	FileText,
	Globe,
	Search,
	FileCode,
	Eye,
	Headphones,
	MemoryStick,
	GitBranch,
	Wrench,
	Brain,
	MessageSquare,
} from "lucide-react";
import type { ToolCallRecord } from "@/lib/pux-store";
import type { SubAgentRecord } from "@pux/shared";

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

export function toolLabel(name: string): string {
	return TOOL_LABELS[name] ?? name;
}

// ── Tool-type icons ──

const TOOL_ICONS: Record<string, React.ElementType> = {
	bash: Terminal,
	file_read: FileText,
	file_write: FileText,
	file_edit: FileText,
	browse_to: Globe,
	snapshot_a11y: Eye,
	click_element: Globe,
	type_text: Globe,
	scroll_page: Globe,
	web_search: Search,
	web_scrape: FileCode,
	analyze_image: Eye,
	transcribe_audio: Headphones,
	memory: MemoryStick,
	delegate_to: GitBranch,
	delegate_async: GitBranch,
};

export function toolIcon(name: string): React.ElementType {
	return TOOL_ICONS[name] ?? Wrench;
}

export function toolArgPreview(name: string, args?: unknown): string {
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

// ── Truncate tool result for preview ──

function truncateResult(result: unknown, maxLen = 200): string {
	if (result === undefined || result === null) return "";
	const str = typeof result === "string" ? result : JSON.stringify(result, null, 2);
	if (str.length <= maxLen) return str;
	return str.slice(0, maxLen) + "...";
}

function resultAsString(result: unknown): string {
	if (result === undefined || result === null) return "";
	return typeof result === "string" ? result : JSON.stringify(result, null, 2);
}

// ── Sub-agent tool row with expandable result ──

function SubAgentToolRow({ tool }: { tool: ToolCallRecord }) {
	const [expanded, setExpanded] = useState(false);
	const endedAt = tool.endedAt;
	const duration = endedAt ? endedAt - tool.timestamp : null;
	const preview = toolArgPreview(tool.toolName, tool.args);
	const hasError = tool.isError;
	const hasResult = tool.result !== undefined && tool.result !== null;
	const Icon = toolIcon(tool.toolName);

	const color = hasError
		? "text-red-500"
		: endedAt
			? "text-green-500"
			: "text-blue-500 animate-pulse";

	return (
		<div className="group/tool-row">
			<div
				className={cn(
					"flex items-center gap-2 px-4 py-1.5 text-xs cursor-pointer",
					"hover:bg-accent/30 transition-colors",
					hasResult && "select-none",
				)}
				onClick={() => hasResult && setExpanded(!expanded)}
			>
				<Icon size={12} className={cn("shrink-0", color)} />
				<span className="font-medium text-muted-foreground">
					{toolLabel(tool.toolName)}
				</span>
				{preview && (
					<span className="truncate text-dim max-w-[200px]">
						{preview}
					</span>
				)}
				{duration !== null && (
					<span className="ml-auto text-dim tabular-nums">
						{formatDuration(duration)}
					</span>
				)}
				{hasResult && (
					<ChevronDownIcon
						size={10}
						className={cn(
							"shrink-0 text-muted-foreground transition-transform duration-150",
							expanded ? "rotate-0" : "-rotate-90",
						)}
					/>
				)}
			</div>
			{/* Expandable result */}
			{hasResult && expanded && (
				<div className="px-4 pb-2 pl-8">
					<pre
						className={cn(
							"whitespace-pre-wrap text-[11px] leading-relaxed rounded-md p-2 max-h-48 overflow-y-auto",
							hasError
								? "bg-red-500/5 text-red-400 border border-red-500/20"
								: "bg-muted/50 text-muted-foreground",
						)}
					>
						{resultAsString(tool.result)}
					</pre>
				</div>
			)}
		</div>
	);
}

// ── Briefing section (what the CTO told the sub-agent) ──

function BriefingSection({ instructions, task }: { instructions?: string; task: string }) {
	const [expanded, setExpanded] = useState(false);
	const briefing = instructions || task;
	if (!briefing) return null;

	return (
		<div className="border-t border-border">
			<button
				onClick={() => setExpanded(!expanded)}
				className="flex items-center gap-2 px-4 py-2 text-xs w-full hover:bg-accent/30 transition-colors"
			>
				<FileText size={12} className="shrink-0 text-muted-foreground" />
				<span className="font-medium text-muted-foreground">
					Briefing
				</span>
				<span className="text-dim truncate max-w-[200px]">
					{task.slice(0, 60)}{task.length > 60 ? "..." : ""}
				</span>
				<ChevronDownIcon
					size={10}
					className={cn(
						"ml-1 shrink-0 text-muted-foreground transition-transform duration-150",
						expanded ? "rotate-0" : "-rotate-90",
					)}
				/>
			</button>
			{expanded && (
				<div className="px-4 pb-2 pl-8">
					<pre className="whitespace-pre-wrap text-[11px] leading-relaxed text-foreground bg-muted/50 rounded-md p-2 max-h-64 overflow-y-auto">
						{briefing}
					</pre>
				</div>
			)}
		</div>
	);
}

// ── Thinking section ──

function ThinkingSection({ text, isRunning }: { text: string; isRunning: boolean }) {
	const [expanded, setExpanded] = useState(false);
	if (!text) return null;

	return (
		<div className="border-t border-border">
			<button
				onClick={() => setExpanded(!expanded)}
				className="flex items-center gap-2 px-4 py-2 text-xs w-full hover:bg-accent/30 transition-colors"
			>
				<Brain size={12} className={cn("shrink-0", isRunning ? "text-blue-500" : "text-muted-foreground")} />
				<span className="font-medium text-muted-foreground">
					Thinking
				</span>
				{isRunning && (
					<span className="text-dim">...</span>
				)}
				<ChevronDownIcon
					size={10}
					className={cn(
						"ml-1 shrink-0 text-muted-foreground transition-transform duration-150",
						expanded ? "rotate-0" : "-rotate-90",
					)}
				/>
			</button>
			{expanded && (
				<div className="px-4 pb-2 pl-8">
					<pre className="whitespace-pre-wrap text-[11px] leading-relaxed text-foreground bg-muted/50 rounded-md p-2 max-h-48 overflow-y-auto">
						{text}
					</pre>
				</div>
			)}
		</div>
	);
}

function TextSection({ text }: { text: string }) {
	const [expanded, setExpanded] = useState(false);
	if (!text) return null;

	return (
		<div className="border-t border-border">
			<button
				onClick={() => setExpanded(!expanded)}
				className="flex items-center gap-2 px-4 py-2 text-xs w-full hover:bg-accent/30 transition-colors"
			>
				<MessageSquare size={12} className="shrink-0 text-muted-foreground" />
				<span className="font-medium text-muted-foreground">
					Output
				</span>
				<span className="text-dim">({text.length} chars)</span>
				<ChevronDownIcon
					size={10}
					className={cn(
						"ml-1 shrink-0 text-muted-foreground transition-transform duration-150",
						expanded ? "rotate-0" : "-rotate-90",
					)}
				/>
			</button>
			{expanded && (
				<div className="px-4 pb-2 pl-8">
					<pre className="whitespace-pre-wrap text-[11px] leading-relaxed text-foreground bg-muted/50 rounded-md p-2 max-h-64 overflow-y-auto">
						{text}
					</pre>
				</div>
			)}
		</div>
	);
}

// ── Main delegate renderer ──
// Exported so ToolFallback can use it directly (bypasses makeAssistantToolUI
// registration which has a timing/matching issue in assistant-ui v0.14.5).

export function DelegateRenderer({
	args,
	result,
	status,
}: {
	args: Record<string, any>;
	result?: any;
	status?: { type: string };
}) {
	const agentName = (args.role as string) || (args.agent_id as string) || (args.agent as string) || (args.instructions as string) || "agent";
	const task = (args.task as string) || (args.prompt as string) || "";
	const isRunning = status?.type === "running";
	const isComplete = status?.type === "complete";
	const isError = status?.type === "error";

	const agents = usePuxStore((s) => s.agents);
	// Match by injected agentId (exact, handles concurrent same-role agents),
	// then fall back to agentName + task prefix matching.
	const agentState = (() => {
		// Best: match by agentId injected during subagent_start
		const injectedId = args.__agentId as string | undefined;
		if (injectedId) {
			const byId = agents.get(injectedId);
			if (byId) return byId;
		}
		const candidates = [...agents.values()].filter(
			(a) => a.agentName === agentName,
		);
		if (candidates.length === 0) return undefined;
		if (candidates.length === 1) return candidates[0];
		// Multiple agents with same name — try task prefix match
		const byTask = candidates.find(
			(a) => task.startsWith(a.task) || a.task.startsWith(task),
		);
		return byTask ?? candidates.find((a) => a.status === "running") ?? candidates[0];
	})();

	// Persisted sub-agent trace from tool_calls JSON (available after page reload)
	const persistedSubAgent = args.__subAgent as SubAgentRecord | undefined;

	// Priority: live Zustand state → persisted subAgent from DB
	const toolCalls: ToolCallRecord[] = agentState?.toolCalls
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

	// Tools default to closed — user clicks to expand.
	const [expanded, setExpanded] = useState(false);

	// Determine display status: live state takes priority, then persisted subAgent
	const displayStatus = isRunning ? "working..."
		: isError ? "failed"
		: persistedSubAgent?.status === "error" ? "failed"
		: (agentState || persistedSubAgent) ? "done" : "done";

	const elapsed = agentState
		? (agentState.endedAt ?? Date.now()) - agentState.startedAt
		: 0;

	return (
		<div className="my-2 w-full rounded-lg border border-border">
			{/* Header — click to fold/unfold */}
			<button
				type="button"
				onClick={() => setExpanded((v) => !v)}
				className="flex items-center gap-2 px-4 py-3 text-sm w-full cursor-pointer hover:bg-accent/30 transition-colors select-none text-left"
			>
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
					{displayStatus}
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
				<ChevronDownIcon
					className={cn(
						"size-3.5 shrink-0 text-muted-foreground transition-transform duration-150",
						expanded ? "rotate-0" : "-rotate-90",
					)}
				/>
			</button>

			{/* Expandable execution trace */}
			{expanded && (
				<div className="border-t border-border">
					{/* Briefing — what the CTO told this agent */}
					{(args.instructions || task) && (
						<BriefingSection instructions={args.instructions as string} task={task} />
					)}

					{/* Thinking section */}
					{thinkingText && (
						<ThinkingSection text={thinkingText} isRunning={isRunning} />
					)}

					{/* Tool call list */}
					{subToolCount > 0 && (
						<div className={cn("py-1", !thinkingText && (!args.instructions && !task) && "border-t border-border")}>
							{toolCalls.map((tool, i) => (
								<SubAgentToolRow key={`${tool.toolName}-${tool.timestamp}-${i}`} tool={tool} />
							))}
						</div>
					)}

					{/* Sub-agent text output — live state or persisted fallback */}
					{agentText && <TextSection text={agentText} />}
					{!agentText && persistedSubAgent?.result && <TextSection text={persistedSubAgent.result} />}
				</div>
			)}
		</div>
	);
}

// Keep makeAssistantToolUI registration as fallback (may work in future versions)
export const DelegateToolUI = makeAssistantToolUI({
	toolName: "delegate_to",
	render: DelegateRenderer,
});

export const DelegateAsyncToolUI = makeAssistantToolUI({
	toolName: "delegate_async",
	render: DelegateRenderer,
});

// Check if a tool name is a delegation tool (used by ToolFallback)
export function isDelegateTool(toolName: string): boolean {
	return toolName === "delegate_to" || toolName === "delegate_async";
}
