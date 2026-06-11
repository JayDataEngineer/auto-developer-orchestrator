"use client";

import { memo, useState } from "react";
import {
	AlertCircleIcon,
	CheckIcon,
	ChevronDownIcon,
	LoaderIcon,
	XCircleIcon,
} from "lucide-react";
import {
	type ToolCallMessagePartStatus,
	type ToolCallMessagePartComponent,
} from "@assistant-ui/react";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { useCollapsibleRoot } from "./use-collapsible";
import { usePuxStore } from "@/lib/pux-store";
import { ShieldCheck, CheckCircle } from "lucide-react";
import { DelegateRenderer, isDelegateTool, toolIcon, toolLabel, toolArgPreview } from "@/components/assistant-ui/delegate-tool-ui";

export type ToolFallbackRootProps = Omit<
	React.ComponentPropsWithRef<typeof Collapsible>,
	"open" | "onOpenChange"
> & {
	open?: boolean;
	onOpenChange?: (open: boolean) => void;
	defaultOpen?: boolean;
};

function ToolFallbackRoot({
	className,
	open: controlledOpen,
	onOpenChange: controlledOnOpenChange,
	defaultOpen = false,
	children,
	...props
}: ToolFallbackRootProps) {
	const { collapsibleRef, isOpen, handleOpenChange, animationStyle } =
		useCollapsibleRoot(defaultOpen, controlledOpen, controlledOnOpenChange);

	return (
		<Collapsible
			ref={collapsibleRef}
			data-slot="tool-fallback-root"
			open={isOpen}
			onOpenChange={handleOpenChange}
			className={cn(
				"aui-tool-fallback-root group/tool-fallback-root w-full rounded-lg border py-3",
				className,
			)}
			style={animationStyle}
			{...props}
		>
			{children}
		</Collapsible>
	);
}

type ToolStatus = ToolCallMessagePartStatus["type"];

const statusIconMap: Record<ToolStatus, React.ElementType> = {
	running: LoaderIcon,
	complete: CheckIcon,
	incomplete: XCircleIcon,
	"requires-action": AlertCircleIcon,
};

function ToolFallbackTrigger({
	toolName,
	status,
	className,
	...props
}: React.ComponentPropsWithRef<typeof CollapsibleTrigger> & {
	toolName: string;
	status?: ToolCallMessagePartStatus;
}) {
	const statusType = status?.type ?? "complete";
	const isRunning = statusType === "running";
	const isCancelled =
		status?.type === "incomplete" && status.reason === "cancelled";

	const Icon = statusIconMap[statusType];
	const label = isCancelled ? "Cancelled tool" : "Used tool";

	return (
		<CollapsibleTrigger
			data-slot="tool-fallback-trigger"
			className={cn(
				"aui-tool-fallback-trigger group/trigger flex w-full items-center gap-2 px-4 text-sm transition-colors",
				className,
			)}
			{...props}
		>
			<Icon
				data-slot="tool-fallback-trigger-icon"
				className={cn(
					"aui-tool-fallback-trigger-icon size-4 shrink-0",
					isCancelled && "text-muted-foreground",
					isRunning && "animate-spin",
				)}
			/>
			<span
				data-slot="tool-fallback-trigger-label"
				className={cn(
					"aui-tool-fallback-trigger-label-wrapper relative inline-block grow text-start leading-none",
					isCancelled && "text-muted-foreground line-through",
				)}
			>
				<span>
					{label}: <b>{toolName}</b>
				</span>
				{isRunning && (
					<span
						aria-hidden
						data-slot="tool-fallback-trigger-shimmer"
						className="aui-tool-fallback-trigger-shimmer shimmer pointer-events-none absolute inset-0 motion-reduce:animate-none"
					>
						{label}: <b>{toolName}</b>
					</span>
				)}
			</span>
			<ChevronDownIcon
				data-slot="tool-fallback-trigger-chevron"
				className={cn(
					"aui-tool-fallback-trigger-chevron size-4 shrink-0",
					"transition-transform duration-(--animation-duration) ease-out",
					"group-data-[state=closed]/trigger:-rotate-90",
					"group-data-[state=open]/trigger:rotate-0",
				)}
			/>
		</CollapsibleTrigger>
	);
}

function ToolFallbackContent({
	className,
	children,
	...props
}: React.ComponentPropsWithRef<typeof CollapsibleContent>) {
	return (
		<CollapsibleContent
			data-slot="tool-fallback-content"
			className={cn(
				"aui-tool-fallback-content relative overflow-hidden text-sm outline-none",
				"group/collapsible-content ease-out",
				"data-[state=closed]:animate-collapsible-up",
				"data-[state=open]:animate-collapsible-down",
				"data-[state=closed]:fill-mode-forwards",
				"data-[state=closed]:pointer-events-none",
				"data-[state=open]:duration-(--animation-duration)",
				"data-[state=closed]:duration-(--animation-duration)",
				className,
			)}
			{...props}
		>
			<div className="mt-3 flex flex-col gap-2 border-t pt-2">{children}</div>
		</CollapsibleContent>
	);
}

function ToolFallbackArgs({
	argsText,
	className,
	...props
}: React.ComponentProps<"div"> & {
	argsText?: string;
}) {
	if (!argsText) return null;

	return (
		<div
			data-slot="tool-fallback-args"
			className={cn("aui-tool-fallback-args px-4", className)}
			{...props}
		>
			<pre className="aui-tool-fallback-args-value whitespace-pre-wrap">
				{argsText}
			</pre>
		</div>
	);
}

// ── Image extraction from tool results ──

const BASE64_RE = /^[A-Za-z0-9+/=]+$/;

function extractImageSrc(value: unknown): string | null {
	if (typeof value === "string") {
		const trimmed = value.trim();
		if (trimmed.startsWith("data:image/")) return trimmed;
		if (trimmed.length > 200 && BASE64_RE.test(trimmed)) {
			return `data:image/png;base64,${trimmed}`;
		}
	}
	return null;
}

function extractScreenshotFromResult(
	result: unknown,
): { src: string; meta?: Record<string, unknown> } | null {
	if (result == null) return null;

	// String result — try parsing as JSON first (tool results arrive as JSON strings)
	if (typeof result === "string") {
		try {
			const parsed = JSON.parse(result);
			return extractScreenshotFromResult(parsed);
		} catch {
			// Not valid JSON — check if it's a raw base64 image
			const src = extractImageSrc(result);
			return src ? { src } : null;
		}
	}

	// Object result — look for screenshot/image fields
	if (typeof result === "object" && !Array.isArray(result)) {
		const obj = result as Record<string, unknown>;

		// Common patterns: screenshot, image, image_b64
		for (const key of ["screenshot", "image", "image_b64"]) {
			if (typeof obj[key] === "string") {
				const src = extractImageSrc(obj[key]);
				if (src) {
					// Build metadata from remaining fields (excluding the image data)
					const meta: Record<string, unknown> = {};
					for (const [k, v] of Object.entries(obj)) {
						if (k !== key && typeof v !== "object") meta[k] = v;
					}
					return { src, meta: Object.keys(meta).length > 0 ? meta : undefined };
				}
			}
		}
	}

	return null;
}

function ToolFallbackResult({
	result,
	className,
	...props
}: React.ComponentProps<"div"> & {
	result?: unknown;
}) {
	if (result === undefined) return null;

	// Check if result contains an image we can render
	const imageInfo = extractScreenshotFromResult(result);

	return (
		<div
			data-slot="tool-fallback-result"
			className={cn(
				"aui-tool-fallback-result border-t border-dashed px-4 pt-2",
				className,
			)}
			{...props}
		>
			{imageInfo ? (
				<>
					{imageInfo.meta && (
						<div className="mb-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
							{Object.entries(imageInfo.meta).map(([k, v]) => (
								<span key={k}>
									<span className="font-medium text-foreground">{k}</span>:{" "}
									{String(v).length > 80
										? String(v).slice(0, 77) + "..."
										: String(v)}
								</span>
							))}
						</div>
					)}
					<img
						src={imageInfo.src}
						alt="Tool result screenshot"
						className="max-h-64 rounded-md border border-border"
					/>
				</>
			) : (
				<>
					<p className="aui-tool-fallback-result-header font-semibold">Result:</p>
					<pre className="aui-tool-fallback-result-content whitespace-pre-wrap">
						{typeof result === "string"
							? result
							: JSON.stringify(result, null, 2)}
					</pre>
				</>
			)}
		</div>
	);
}

function ToolFallbackError({
	status,
	className,
	...props
}: React.ComponentProps<"div"> & {
	status?: ToolCallMessagePartStatus;
}) {
	if (status?.type !== "incomplete") return null;

	const error = status.error;
	const errorText = error
		? typeof error === "string"
			? error
			: JSON.stringify(error)
		: null;

	if (!errorText) return null;

	const isCancelled = status.reason === "cancelled";
	const headerText = isCancelled ? "Cancelled reason:" : "Error:";

	return (
		<div
			data-slot="tool-fallback-error"
			className={cn("aui-tool-fallback-error px-4", className)}
			{...props}
		>
			<p className="aui-tool-fallback-error-header font-semibold text-muted-foreground">
				{headerText}
			</p>
			<p className="aui-tool-fallback-error-reason text-muted-foreground">
				{errorText}
			</p>
		</div>
	);
}

const ToolFallbackImpl: ToolCallMessagePartComponent = ({
	toolName,
	argsText,
	args,
	result,
	status,
	interrupt,
}) => {
	const isCancelled =
		status?.type === "incomplete" && status.reason === "cancelled";
	const isRunning = status?.type === "running";
	const isComplete = status?.type === "complete";
	const hasResult = result !== undefined && result !== null;

	// Detect tool execution errors: result string starting with "Error:" or
	// containing "<tool_use_error>" (backend wraps errors this way).
	const resultStr = typeof result === "string" ? result : "";
	const hasError = status?.type === "incomplete" ||
		/^Error:/m.test(resultStr) ||
		resultStr.includes("<tool_use_error>");

	// Extract clean error message from result
	const errorText = hasError && resultStr
		? resultStr.replace(/<\/?tool_use_error>/g, "").trim()
		: null;

	// Check if this is a screenshot tool before early returns
	const isScreenshotTool = ["screenshot", "observe", "browser_screenshot",
		"desktop_screenshot", "computer_screenshot", "take_screenshot",
		"web_screenshot", "desktop_observe"].includes(toolName);

	// Delegate tools get the specialized collapsible card UI
	if (isDelegateTool(toolName)) {
		return <DelegateRenderer args={args ?? {}} result={result} status={status} />;
	}

	// Handle approval interrupts inline — permission hooks from any tool
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const answered = isComplete && pending === null;

	// Extract image from result
	const imageInfo = hasResult && !isCancelled ? extractScreenshotFromResult(result) : null;

	const [expanded, setExpanded] = useState(false);

	if (interrupt?.type === "human") {
		const payload = interrupt.payload as Record<string, unknown> | undefined;
		const hint = payload?.hint as string | undefined;

		if (hint === "approval") {
			if (answered) {
				return (
					<div className="my-1 px-2 py-1">
						<div className="flex items-center gap-2 text-xs text-muted-foreground">
							<CheckCircle size={12} className="text-muted-foreground" />
							<span>Approved: <b>{toolName}</b></span>
						</div>
					</div>
				);
			}

			const title = (payload?.title as string) || `Allow "${toolName}"?`;
			const description = payload?.description as string | undefined;

			return (
				<div className="my-2 rounded-lg border border-yellow-500/30 bg-yellow-500/5 py-3">
					<div className="flex items-center gap-2 px-4 text-sm">
						<ShieldCheck size={14} className="text-yellow-500" />
						<span className="text-xs font-semibold uppercase tracking-wider text-dim">
							Approval Required
						</span>
					</div>
					{title && (
						<div className="mt-2 px-4 text-sm font-medium">
							{title}
						</div>
					)}
					{description && (
						<div className="mt-1 px-4 text-sm text-muted-foreground">
							{description}
						</div>
					)}
					<div className="flex gap-2 border-t border-border px-4 py-3">
						<button
							onClick={() => respond("approve", "")}
							className="rounded-md bg-green-600 px-4 py-1.5 text-sm text-white hover:bg-green-700"
						>
							Approve
						</button>
						<button
							onClick={() => respond("reject", "")}
							className="rounded-md bg-red-600 px-4 py-1.5 text-sm text-white hover:bg-red-700"
						>
							Reject
						</button>
					</div>
				</div>
			);
		}
	}

	// Check for image in result
	// Screenshot tools: image lives under the tool row as expandable result.
	// Other tools with images (orchestrator sharing): render inline.
	const showImageInline = imageInfo && !isScreenshotTool;
	const showImageUnderTool = imageInfo && isScreenshotTool;

	// Compact tool row — same format as sub-agent tool rows
	const preview = toolArgPreview(toolName, args);
	const Icon = toolIcon(toolName);
	const label = toolLabel(toolName);
	const color = hasError
		? "text-red-500"
		: isRunning
			? "text-blue-500"
			: "text-green-500";

	return (
		<div className="group/tool-row">
			<div
				className={cn(
					"flex items-center gap-2 px-2 py-1 text-xs cursor-pointer",
					"hover:bg-accent/30 transition-colors",
					hasResult && "select-none",
				)}
				onClick={() => hasResult && setExpanded(!expanded)}
			>
				<Icon size={12} className={cn("shrink-0", color)} />
				<span className={cn("font-medium", hasError ? "text-red-500" : "text-muted-foreground")}>
					{label}
				</span>
				{preview && !hasError && (
					<span className="truncate text-dim max-w-[200px]">
						{preview}
					</span>
				)}
				{hasError && errorText && (
					<span className="truncate text-red-400/80 max-w-[300px]">
						{errorText.slice(0, 120)}
					</span>
				)}
				{isCancelled && (
					<span className="text-dim line-through">cancelled</span>
				)}
				{hasResult && !hasError && (
					<ChevronDownIcon
						size={10}
						className={cn(
							"shrink-0 text-muted-foreground transition-transform duration-150",
							expanded ? "rotate-0" : "-rotate-90",
						)}
					/>
				)}
				{isRunning && !hasResult && (
					<span className="text-dim">...</span>
				)}
			</div>
			{/* Image inline — orchestrator sharing an image with the user */}
			{showImageInline && (
				<div className="px-2 pb-1 pl-6">
					{imageInfo.meta && (
						<div className="mb-1 flex flex-wrap gap-x-3 gap-y-1 text-[10px] text-muted-foreground">
							{Object.entries(imageInfo.meta).map(([k, v]) => (
								<span key={k}>
									<span className="font-medium text-foreground">{k}</span>: {String(v).length > 60 ? String(v).slice(0, 57) + "..." : String(v)}
								</span>
							))}
						</div>
					)}
					<img
						src={imageInfo.src}
						alt="Tool result"
						className="max-h-64 rounded-md border border-border"
					/>
				</div>
			)}
			{/* Image under screenshot tool row — expandable, auto-opens on first result */}
			{showImageUnderTool && expanded && (
				<div className="px-2 pb-1 pl-6">
					{imageInfo.meta && (
						<div className="mb-1 flex flex-wrap gap-x-3 gap-y-1 text-[10px] text-muted-foreground">
							{Object.entries(imageInfo.meta).map(([k, v]) => (
								<span key={k}>
									<span className="font-medium text-foreground">{k}</span>: {String(v).length > 60 ? String(v).slice(0, 57) + "..." : String(v)}
								</span>
							))}
						</div>
					)}
					<img
						src={imageInfo.src}
						alt="Screenshot"
						className="max-h-64 rounded-md border border-border"
					/>
				</div>
			)}
			{/* Tool error — always visible, red styling */}
			{hasError && errorText && (
				<div className="px-2 pb-1 pl-6">
					<pre className="whitespace-pre-wrap text-[11px] leading-relaxed rounded-md p-2 max-h-48 overflow-y-auto bg-red-500/5 text-red-400 border border-red-500/20">
						{errorText}
					</pre>
				</div>
			)}
			{/* Expandable text result (non-image, non-screenshot, non-error) */}
			{hasResult && expanded && !imageInfo && !hasError && (
				<div className="px-2 pb-1 pl-6">
					<pre className="whitespace-pre-wrap text-[11px] leading-relaxed rounded-md p-2 max-h-48 overflow-y-auto bg-muted/50 text-muted-foreground">
						{typeof result === "string" ? result : JSON.stringify(result, null, 2)}
					</pre>
				</div>
			)}
		</div>
	);
};

const ToolFallback = memo(
	ToolFallbackImpl,
) as unknown as ToolCallMessagePartComponent & {
	Root: typeof ToolFallbackRoot;
	Trigger: typeof ToolFallbackTrigger;
	Content: typeof ToolFallbackContent;
	Args: typeof ToolFallbackArgs;
	Result: typeof ToolFallbackResult;
	Error: typeof ToolFallbackError;
};

ToolFallback.displayName = "ToolFallback";
ToolFallback.Root = ToolFallbackRoot;
ToolFallback.Trigger = ToolFallbackTrigger;
ToolFallback.Content = ToolFallbackContent;
ToolFallback.Args = ToolFallbackArgs;
ToolFallback.Result = ToolFallbackResult;
ToolFallback.Error = ToolFallbackError;

export {
	ToolFallback,
	ToolFallbackRoot,
	ToolFallbackTrigger,
	ToolFallbackContent,
	ToolFallbackArgs,
	ToolFallbackResult,
	ToolFallbackError,
};
