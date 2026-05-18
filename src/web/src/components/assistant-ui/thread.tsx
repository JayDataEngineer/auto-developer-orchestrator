"use client";

import { MarkdownTextContent } from "@/components/assistant-ui/markdown-text";
import {
	ComposerAddAttachment,
	ComposerAttachments,
	UserMessageAttachments,
} from "@/components/assistant-ui/attachment";
import {
	ReasoningContent,
	ReasoningRoot,
	ReasoningText,
	ReasoningTrigger,
} from "@/components/assistant-ui/reasoning";
import {
	ToolGroupContent,
	ToolGroupRoot,
	ToolGroupTrigger,
} from "@/components/assistant-ui/tool-group";
import { ToolFallback } from "@/components/assistant-ui/tool-fallback";
import { TooltipIconButton } from "@/components/assistant-ui/tooltip-icon-button";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectGroup,
	SelectItem,
	SelectLabel,
	SelectSeparator,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { usePuxStore } from "@/lib/pux-store";
import { Spinner } from "@/components/ui/spinner";
import { AddProviderDialog } from "@/components/add-provider-dialog";
import { cn } from "@/lib/utils";
import {
	ActionBarPrimitive,
	AuiIf,
	BranchPickerPrimitive,
	ComposerPrimitive,
	ErrorPrimitive,
	getMcpAppFromToolPart,
	MessagePrimitive,
	ThreadPrimitive,
	useAuiState,
} from "@assistant-ui/react";
import {
	ArrowDownIcon,
	ArrowUpIcon,
	CheckIcon,
	ChevronLeftIcon,
	ChevronRightIcon,
	CopyIcon,
	CpuIcon,
	DownloadIcon,
	HardDriveIcon,
	PencilIcon,
	PlusIcon,
	RefreshCwIcon,
	SquareIcon,
} from "lucide-react";
import { useEffect, useMemo, useState, type FC } from "react";

export const Thread: FC = () => {
	return (
		<ThreadPrimitive.Root
			className="aui-root aui-thread-root @container flex h-full flex-col bg-background"
			style={{
				["--thread-max-width" as string]: "44rem",
				["--composer-radius" as string]: "24px",
				["--composer-padding" as string]: "10px",
			} as React.CSSProperties}
		>
			<ThreadPrimitive.Viewport
				turnAnchor="top"
				data-slot="aui_thread-viewport"
				className="relative flex flex-1 flex-col overflow-x-auto overflow-y-scroll scroll-smooth"
			>
				<div className="mx-auto flex w-full max-w-[var(--thread-max-width)] flex-1 flex-col px-4 pt-4">
					<AuiIf condition={(s) => s.thread.isEmpty}>
						<ThreadWelcome />
					</AuiIf>

					<div
						data-slot="aui_message-group"
						className="mb-10 flex flex-col gap-y-8 empty:hidden"
					>
						<ThreadPrimitive.Messages>
							{() => <ThreadMessage />}
						</ThreadPrimitive.Messages>
						<AuiIf condition={(s) => s.thread.isRunning}>
							<div className="flex items-center gap-2 px-2 text-xs text-muted-foreground">
								<Spinner className="size-3.5" />
								Thinking...
							</div>
						</AuiIf>
					</div>

					<ThreadPrimitive.ViewportFooter className="aui-thread-viewport-footer sticky bottom-0 mt-auto flex flex-col gap-4 overflow-visible rounded-t-3xl bg-background pb-4 md:pb-6">
						<ThreadScrollToBottom />
						<Composer />
					</ThreadPrimitive.ViewportFooter>
				</div>
			</ThreadPrimitive.Viewport>
		</ThreadPrimitive.Root>
	);
};

const ThreadMessage: FC = () => {
	const role = useAuiState((s) => s.message.role);
	const isEditing = useAuiState((s) => s.message.composer.isEditing);

	if (isEditing) return <EditComposer />;
	if (role === "user") return <UserMessage />;
	return <AssistantMessage />;
};

const ThreadScrollToBottom: FC = () => {
	return (
		<ThreadPrimitive.ScrollToBottom asChild>
			<TooltipIconButton
				tooltip="Scroll to bottom"
				variant="outline"
				className="aui-thread-scroll-to-bottom absolute -top-12 z-10 self-center rounded-full p-4 disabled:invisible dark:border-border dark:bg-background dark:hover:bg-accent"
			>
				<ArrowDownIcon />
			</TooltipIconButton>
		</ThreadPrimitive.ScrollToBottom>
	);
};

const ThreadWelcome: FC = () => {
	return (
		<div className="aui-thread-welcome-root my-auto flex grow flex-col">
			<div className="aui-thread-welcome-center flex w-full grow flex-col items-center justify-center">
				<div className="aui-thread-welcome-message flex size-full flex-col justify-center px-4">
					<h1 className="aui-thread-welcome-message-inner fade-in slide-in-from-bottom-1 animate-in fill-mode-both font-semibold text-2xl duration-200">
						Pux
					</h1>
					<p className="aui-thread-welcome-message-inner fade-in slide-in-from-bottom-1 animate-in fill-mode-both text-muted-foreground text-xl delay-75 duration-200">
						Your AI-powered development orchestrator
					</p>
				</div>
			</div>
		</div>
	);
};

const Composer: FC = () => {
	return (
		<ComposerPrimitive.Root className="aui-composer-root relative flex w-full flex-col">
			<ComposerPrimitive.AttachmentDropzone asChild>
				<div
					data-slot="aui_composer-shell"
					className="flex w-full flex-col gap-2 rounded-3xl border border-border bg-muted p-2.5 transition-shadow focus-within:border-ring/75 focus-within:ring-2 focus-within:ring-ring/20 data-[dragging=true]:border-ring data-[dragging=true]:border-dashed data-[dragging=true]:bg-accent/50"
				>
					<ComposerAttachments />
					<ComposerPrimitive.Input
						placeholder="Send a message..."
						className="aui-composer-input max-h-32 min-h-10 w-full resize-none bg-transparent px-1.75 py-1 text-sm outline-none placeholder:text-muted-foreground/80"
						rows={1}
						autoFocus
						aria-label="Message input"
					/>
					<ComposerAction />
				</div>
			</ComposerPrimitive.AttachmentDropzone>
		</ComposerPrimitive.Root>
	);
};

const ComposerAction: FC = () => {
	const modelList = usePuxStore((s) => s.modelList);
	const activeModel = usePuxStore((s) => s.activeModel);
	const setModel = usePuxStore((s) => s.setModel);
	const defaultLogic = usePuxStore((s) => s.defaultLogic);
	const defaultWorker = usePuxStore((s) => s.defaultWorker);
	const setDefaults = usePuxStore((s) => s.setDefaults);
	const loadDefaults = usePuxStore((s) => s.loadDefaults);
	const [showAddProvider, setShowAddProvider] = useState(false);

	const grouped = useMemo(() => {
		const map = new Map<string, typeof modelList>();
		for (const m of modelList) {
			const provider = m.provider || "local";
			if (!map.has(provider)) map.set(provider, []);
			map.get(provider)!.push(m);
		}
		return map;
	}, [modelList]);

	const currentName = modelList.find((m) => m.id === activeModel)?.name || activeModel || "Default";
	const logicName = modelList.find((m) => m.id === defaultLogic)?.name || defaultLogic || "none";
	const workerName = modelList.find((m) => m.id === defaultWorker)?.name || defaultWorker || "none";

	// Load defaults on mount
	useEffect(() => { loadDefaults(); }, []);

	return (
		<>
			<div className="aui-composer-action-wrapper relative flex items-center justify-between">
			<div className="flex items-center gap-1">
				<ComposerAddAttachment />
				<Select value={activeModel || undefined} onValueChange={(val) => {
					if (val === "__clear_logic") { setDefaults("", defaultWorker); return; }
					if (val === "__clear_worker") { setDefaults(defaultLogic, ""); return; }
					setModel(val);
				}}>
					<SelectTrigger
						className="h-7 w-auto max-w-48 gap-1 border-none bg-transparent px-2 text-xs text-muted-foreground shadow-none hover:bg-accent/50 focus:ring-0"
						aria-label="Select model"
					>
						<SelectValue placeholder={currentName} />
					</SelectTrigger>
					<SelectContent className="max-h-80 min-w-[220px]">
						{/* Defaults info at top */}
						{defaultLogic || defaultWorker ? (
							<>
								<div className="space-y-0.5 px-2 py-1.5">
									{defaultLogic && (
										<div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
											<CpuIcon className="size-3 shrink-0" />
											<span className="truncate">Logic: {logicName}</span>
											<div
												className="ml-auto cursor-pointer hover:text-foreground"
												onMouseDown={(e) => e.preventDefault()}
												onClick={() => setDefaults("", defaultWorker)}
											>
												✕
											</div>
										</div>
									)}
									{defaultWorker && (
										<div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
											<HardDriveIcon className="size-3 shrink-0" />
											<span className="truncate">Worker: {workerName}</span>
											<div
												className="ml-auto cursor-pointer hover:text-foreground"
												onMouseDown={(e) => e.preventDefault()}
												onClick={() => setDefaults(defaultLogic, "")}
											>
												✕
											</div>
										</div>
									)}
								</div>
								<SelectSeparator />
							</>
						) : null}
						{/* Models grouped by provider */}
						{[...grouped.entries()].map(([provider, models]) => (
							<SelectGroup key={provider}>
								<SelectLabel className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
									{provider}
								</SelectLabel>
								{models.map((m) => (
									<SelectItem key={m.id} value={m.id} className="text-sm">
										{m.name}
									</SelectItem>
								))}
							</SelectGroup>
						))}
						<SelectSeparator />
						{/* Set as default actions */}
						{activeModel && (
							<>
								<div
									className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-xs text-muted-foreground outline-none hover:bg-accent hover:text-accent-foreground"
									onMouseDown={(e) => e.preventDefault()}
									onClick={() => setDefaults(activeModel, defaultWorker)}
								>
									<CpuIcon className="size-3.5" />
									Set as Logic Default
								</div>
								<div
									className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-xs text-muted-foreground outline-none hover:bg-accent hover:text-accent-foreground"
									onMouseDown={(e) => e.preventDefault()}
									onClick={() => setDefaults(defaultLogic, activeModel)}
								>
									<HardDriveIcon className="size-3.5" />
									Set as Worker Default
								</div>
								<SelectSeparator />
							</>
						)}
						{/* Add provider */}
						<div
							className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm text-muted-foreground outline-none hover:bg-accent hover:text-accent-foreground"
							onMouseDown={(e) => e.preventDefault()}
							onClick={() => setShowAddProvider(true)}
						>
							<PlusIcon className="size-3.5" />
							Add provider...
						</div>
					</SelectContent>
				</Select>
			</div>
			<div className="flex items-center gap-1">
			<AuiIf condition={(s) => !s.thread.isRunning}>
				<ComposerPrimitive.Send asChild>
					<TooltipIconButton
						tooltip="Send message"
						side="bottom"
						type="button"
						variant="default"
						size="icon"
						className="aui-composer-send size-8 rounded-full"
						aria-label="Send message"
					>
						<ArrowUpIcon className="aui-composer-send-icon size-4" />
					</TooltipIconButton>
				</ComposerPrimitive.Send>
			</AuiIf>
			<AuiIf condition={(s) => s.thread.isRunning}>
				<ComposerPrimitive.Cancel asChild>
					<Button
						type="button"
						variant="default"
						size="icon"
						className="aui-composer-cancel size-8 rounded-full"
						aria-label="Stop generating"
					>
						<SquareIcon className="aui-composer-cancel-icon size-3 fill-current" />
					</Button>
				</ComposerPrimitive.Cancel>
			</AuiIf>
			</div>
		</div>
		<AddProviderDialog open={showAddProvider} onOpenChange={setShowAddProvider} />
	</>
	);
};

const MessageError: FC = () => {
	return (
		<MessagePrimitive.Error>
			<ErrorPrimitive.Root className="aui-message-error-root mt-2 rounded-md border border-destructive bg-destructive/10 p-3 text-destructive text-sm dark:bg-destructive/5 dark:text-red-200">
				<ErrorPrimitive.Message className="aui-message-error-message line-clamp-2" />
			</ErrorPrimitive.Root>
		</MessagePrimitive.Error>
	);
};

const AssistantMessage: FC = () => {
	const ACTION_BAR_PT = "pt-1.5";
	const ACTION_BAR_HEIGHT = `-mb-7.5 min-h-7.5 ${ACTION_BAR_PT}`;

	return (
		<MessagePrimitive.Root
			data-slot="aui_assistant-message-root"
			data-role="assistant"
			className="fade-in slide-in-from-bottom-1 relative animate-in duration-150 [contain-intrinsic-size:auto_300px] [content-visibility:auto]"
		>
			<div
				data-slot="aui_assistant-message-content"
				className="wrap-break-word px-2 text-foreground leading-relaxed"
			>
				<MessagePrimitive.GroupedParts
					groupBy={(part) => {
						if (part.type === "reasoning")
							return ["group-chainOfThought", "group-reasoning"];
						if (part.type === "tool-call") {
							if (getMcpAppFromToolPart(part)) return null;
							return ["group-chainOfThought", "group-tool"];
						}
						return null;
					}}
				>
					{({ part, children }) => {
						switch (part.type) {
							case "group-chainOfThought":
								return (
									<div data-slot="aui_chain-of-thought">{children}</div>
								);
							case "group-reasoning": {
								const running = part.status.type === "running";
								return (
									<ReasoningRoot defaultOpen={running}>
										<ReasoningTrigger active={running} />
										<ReasoningContent aria-busy={running}>
											<ReasoningText>{children}</ReasoningText>
										</ReasoningContent>
									</ReasoningRoot>
								);
							}
							case "group-tool":
								return (
									<ToolGroupRoot>
										<ToolGroupTrigger
											count={part.indices.length}
											active={part.status.type === "running"}
										/>
										<ToolGroupContent>{children}</ToolGroupContent>
									</ToolGroupRoot>
								);
							case "text":
								return <MarkdownTextContent text={part.text} />;
							case "reasoning":
								return <>{part.text}</>;
							case "tool-call":
								return part.toolUI ?? <ToolFallback {...part} />;
							default:
								return null;
						}
					}}
				</MessagePrimitive.GroupedParts>
				<MessageError />
			</div>

			<div
				data-slot="aui_assistant-message-footer"
				className={cn("ms-2 flex items-center", ACTION_BAR_HEIGHT)}
			>
				<BranchPicker />
				<AssistantActionBar />
			</div>
		</MessagePrimitive.Root>
	);
};

const AssistantActionBar: FC = () => {
	return (
		<ActionBarPrimitive.Root
			hideWhenRunning
			autohide="not-last"
			className="aui-assistant-action-bar-root col-start-3 row-start-2 -ms-1 flex gap-1 text-muted-foreground"
		>
			<ActionBarPrimitive.Copy asChild>
				<TooltipIconButton tooltip="Copy">
					<AuiIf condition={(s) => s.message.isCopied}>
						<CheckIcon />
					</AuiIf>
					<AuiIf condition={(s) => !s.message.isCopied}>
						<CopyIcon />
					</AuiIf>
				</TooltipIconButton>
			</ActionBarPrimitive.Copy>
			<ActionBarPrimitive.ExportMarkdown asChild>
				<TooltipIconButton tooltip="Export as Markdown">
					<DownloadIcon />
				</TooltipIconButton>
			</ActionBarPrimitive.ExportMarkdown>
			<ActionBarPrimitive.Reload asChild>
				<TooltipIconButton tooltip="Refresh">
					<RefreshCwIcon />
				</TooltipIconButton>
			</ActionBarPrimitive.Reload>
		</ActionBarPrimitive.Root>
	);
};

const UserMessage: FC = () => {
	return (
		<MessagePrimitive.Root
			data-slot="aui_user-message-root"
			className="fade-in slide-in-from-bottom-1 grid animate-in auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] content-start gap-y-2 px-2 duration-150 [contain-intrinsic-size:auto_60px] [content-visibility:auto] [&:where(>*)]:col-start-2"
			data-role="user"
		>
			<UserMessageAttachments />
			<div className="aui-user-message-content-wrapper relative col-start-2 min-w-0">
				<div className="aui-user-message-content wrap-break-word peer rounded-2xl bg-muted px-4 py-2.5 text-foreground empty:hidden">
					<MessagePrimitive.Parts />
				</div>
				<div className="aui-user-action-bar-wrapper absolute start-0 top-1/2 -translate-x-full -translate-y-1/2 pe-2 peer-empty:hidden rtl:translate-x-full">
					<UserActionBar />
				</div>
			</div>

			<BranchPicker
				data-slot="aui_user-branch-picker"
				className="col-span-full col-start-1 row-start-3 -me-1 justify-end"
			/>
		</MessagePrimitive.Root>
	);
};

const UserActionBar: FC = () => {
	return (
		<ActionBarPrimitive.Root
			hideWhenRunning
			autohide="not-last"
			className="aui-user-action-bar-root flex flex-col items-end"
		>
			<ActionBarPrimitive.Edit asChild>
				<TooltipIconButton
					tooltip="Edit"
					className="aui-user-action-edit p-4"
				>
					<PencilIcon />
				</TooltipIconButton>
			</ActionBarPrimitive.Edit>
		</ActionBarPrimitive.Root>
	);
};

const EditComposer: FC = () => {
	return (
		<MessagePrimitive.Root
			data-slot="aui_edit-composer-wrapper"
			className="flex flex-col px-2"
		>
			<ComposerPrimitive.Root className="aui-edit-composer-root ms-auto flex w-full max-w-[85%] flex-col rounded-2xl bg-muted">
				<ComposerPrimitive.Input
					className="aui-edit-composer-input min-h-14 w-full resize-none bg-transparent p-4 text-foreground text-sm outline-none"
					autoFocus
				/>
				<div className="aui-edit-composer-footer mx-3 mb-3 flex items-center gap-2 self-end">
					<ComposerPrimitive.Cancel asChild>
						<Button variant="ghost" size="sm">
							Cancel
						</Button>
					</ComposerPrimitive.Cancel>
					<ComposerPrimitive.Send asChild>
						<Button size="sm">Update</Button>
					</ComposerPrimitive.Send>
				</div>
			</ComposerPrimitive.Root>
		</MessagePrimitive.Root>
	);
};

const BranchPicker: FC<BranchPickerPrimitive.Root.Props> = ({
	className,
	...rest
}) => {
	return (
		<BranchPickerPrimitive.Root
			hideWhenSingleBranch
			className={cn(
				"aui-branch-picker-root -ms-2 me-2 inline-flex items-center text-muted-foreground text-xs",
				className,
			)}
			{...rest}
		>
			<BranchPickerPrimitive.Previous asChild>
				<TooltipIconButton tooltip="Previous">
					<ChevronLeftIcon />
				</TooltipIconButton>
			</BranchPickerPrimitive.Previous>
			<span className="aui-branch-picker-state font-medium">
				<BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
			</span>
			<BranchPickerPrimitive.Next asChild>
				<TooltipIconButton tooltip="Next">
					<ChevronRightIcon />
				</TooltipIconButton>
			</BranchPickerPrimitive.Next>
		</BranchPickerPrimitive.Root>
	);
};
