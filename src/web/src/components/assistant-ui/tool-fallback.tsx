"use client";

import { memo } from "react";
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

function ToolFallbackResult({
	result,
	className,
	...props
}: React.ComponentProps<"div"> & {
	result?: unknown;
}) {
	if (result === undefined) return null;

	return (
		<div
			data-slot="tool-fallback-result"
			className={cn(
				"aui-tool-fallback-result border-t border-dashed px-4 pt-2",
				className,
			)}
			{...props}
		>
			<p className="aui-tool-fallback-result-header font-semibold">Result:</p>
			<pre className="aui-tool-fallback-result-content whitespace-pre-wrap">
				{typeof result === "string" ? result : JSON.stringify(result, null, 2)}
			</pre>
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
	result,
	status,
	interrupt,
}) => {
	const isCancelled =
		status?.type === "incomplete" && status.reason === "cancelled";

	// Handle approval interrupts inline — permission hooks from any tool
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const isComplete = status?.type === "complete";
	const answered = isComplete && pending === null;

	if (interrupt?.type === "human") {
		const payload = interrupt.payload as Record<string, unknown> | undefined;
		const hint = payload?.hint as string | undefined;

		if (hint === "approval") {
			if (answered) {
				return (
					<div className="my-2 rounded-lg border border-border py-3">
						<div className="flex items-center gap-2 px-4 text-sm">
							<CheckCircle size={14} className="text-muted-foreground" />
							<span className="text-muted-foreground">
								Approved: <b>{toolName}</b>
							</span>
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

	return (
		<ToolFallbackRoot
			className={cn(isCancelled && "border-muted-foreground/30 bg-muted/30")}
		>
			<ToolFallbackTrigger toolName={toolName} status={status} />
			<ToolFallbackContent>
				<ToolFallbackError status={status} />
				<ToolFallbackArgs
					argsText={argsText}
					className={cn(isCancelled && "opacity-60")}
				/>
				{!isCancelled && <ToolFallbackResult result={result} />}
			</ToolFallbackContent>
		</ToolFallbackRoot>
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
