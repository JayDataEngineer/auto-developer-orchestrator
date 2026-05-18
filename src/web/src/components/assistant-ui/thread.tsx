"use client";

import {
	ComposerAddAttachment,
	ComposerAttachments,
	UserMessageAttachments,
} from "@/components/assistant-ui/attachment";
import { MarkdownText } from "@/components/assistant-ui/markdown-text";
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
import { AddProviderDialog } from "@/components/add-provider-dialog";
import { cn } from "@/lib/utils";
import {
	ActionBarPrimitive,
	AuiIf,
	BranchPickerPrimitive,
	ComposerPrimitive,
	ErrorPrimitive,
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
	LoaderIcon,
	PencilIcon,
	PlusIcon,
	RefreshCwIcon,
	SquareIcon,
} from "lucide-react";
import { useEffect, useMemo, useState, type FC } from "react";

export const Thread: FC = () => {
	return (
		<ThreadPrimitive.Root
			className="flex h-full flex-col bg-background text-sm"
			style={{
				["--thread-max-width" as string]: "44rem",
			} as React.CSSProperties}
		>
			<ThreadPrimitive.Viewport
				turnAnchor="top"
				className="relative flex flex-1 flex-col overflow-x-auto overflow-y-scroll scroll-smooth px-4 pt-4"
			>
				<AuiIf condition={(s) => s.thread.isEmpty}>
					<ThreadWelcome />
				</AuiIf>

				<ThreadPrimitive.Messages>
					{() => <ThreadMessage />}
				</ThreadPrimitive.Messages>

				<ThreadPrimitive.ViewportFooter className="sticky bottom-0 mx-auto mt-auto flex w-full max-w-[var(--thread-max-width)] flex-col gap-4 overflow-visible rounded-t-3xl bg-background pb-4">
					<ThreadScrollToBottom />
					<Composer />
				</ThreadPrimitive.ViewportFooter>
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
				className="absolute -top-12 z-10 self-center rounded-full p-4 disabled:invisible"
			>
				<ArrowDownIcon />
			</TooltipIconButton>
		</ThreadPrimitive.ScrollToBottom>
	);
};

const ThreadWelcome: FC = () => {
	return (
		<div className="mx-auto my-auto flex w-full max-w-[var(--thread-max-width)] flex-grow flex-col">
			<div className="flex w-full flex-grow flex-col items-center justify-center">
				<div className="flex size-full flex-col justify-center px-8">
					<div className="text-2xl font-semibold">Pux</div>
					<div className="text-2xl text-muted-foreground/65">
						Your AI-powered development orchestrator
					</div>
				</div>
			</div>
		</div>
	);
};

const Composer: FC = () => {
	return (
		<ComposerPrimitive.Root className="relative flex w-full flex-col">
			<ComposerPrimitive.AttachmentDropzone className="flex w-full flex-col rounded-2xl border border-input bg-background px-1 pt-2 outline-none transition-shadow has-[textarea:focus-visible]:border-ring has-[textarea:focus-visible]:ring-2 has-[textarea:focus-visible]:ring-ring/20 data-[dragging=true]:border-ring data-[dragging=true]:border-dashed data-[dragging=true]:bg-accent/50">
				<ComposerAttachments />
				<ComposerPrimitive.Input
					placeholder="Send a message..."
					className="mb-1 max-h-32 min-h-14 w-full resize-none bg-transparent px-4 pt-2 pb-3 text-sm outline-none placeholder:text-muted-foreground focus-visible:ring-0"
					rows={1}
					autoFocus
					aria-label="Message input"
				/>
				<ComposerAction />
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

	useEffect(() => { loadDefaults(); }, []);

	return (
		<>
			<div className="relative mx-2 mb-2 flex items-center justify-between">
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
								className="size-8 rounded-full"
								aria-label="Send message"
							>
								<ArrowUpIcon className="size-4" />
							</TooltipIconButton>
						</ComposerPrimitive.Send>
					</AuiIf>
					<AuiIf condition={(s) => s.thread.isRunning}>
						<ComposerPrimitive.Cancel asChild>
							<Button
								type="button"
								variant="default"
								size="icon"
								className="size-8 rounded-full"
								aria-label="Stop generating"
							>
								<SquareIcon className="size-3 fill-current" />
							</Button>
						</ComposerPrimitive.Cancel>
					</AuiIf>
				</div>
			</div>
			<AddProviderDialog open={showAddProvider} onOpenChange={setShowAddProvider} />
		</>
	);
};

const AssistantMessage: FC = () => {
	return (
		<MessagePrimitive.Root
			className="group relative mx-auto w-full max-w-[var(--thread-max-width)] py-3 fade-in slide-in-from-bottom-1 animate-in duration-150"
			data-role="assistant"
		>
			<div className="break-words px-2 leading-relaxed text-foreground">
				<MessagePrimitive.Parts
					components={{
						Text: MarkdownText,
						tools: { Fallback: ToolFallback },
					}}
				/>
				<MessageError />
				<AuiIf condition={(s) => s.thread.isRunning && s.message.content.length === 0}>
					<div className="flex items-center gap-2 text-muted-foreground">
						<LoaderIcon className="size-4 animate-spin" />
						<span className="text-sm">Thinking...</span>
					</div>
				</AuiIf>
			</div>

			<div className="mt-1 ml-2 flex min-h-6 items-center opacity-0 transition-opacity group-hover:opacity-100">
				<BranchPicker />
				<AssistantActionBar />
			</div>
		</MessagePrimitive.Root>
	);
};

const MessageError: FC = () => {
	return (
		<MessagePrimitive.Error>
			<ErrorPrimitive.Root className="mt-2 rounded-md border border-destructive bg-destructive/10 p-3 text-destructive text-sm dark:bg-destructive/5 dark:text-red-200">
				<ErrorPrimitive.Message className="line-clamp-2" />
			</ErrorPrimitive.Root>
		</MessagePrimitive.Error>
	);
};

const AssistantActionBar: FC = () => {
	return (
		<ActionBarPrimitive.Root
			hideWhenRunning
			autohide
			className="-ml-1 flex gap-1 text-muted-foreground"
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
			className="mx-auto grid w-full max-w-[var(--thread-max-width)] auto-rows-auto grid-cols-[minmax(72px,1fr)_auto] content-start gap-y-2 px-2 py-3 fade-in slide-in-from-bottom-1 animate-in duration-150"
			data-role="user"
		>
			<UserMessageAttachments />

			<div className="relative col-start-2 min-w-0">
				<div className="rounded-2xl bg-muted px-4 py-2.5 break-words text-foreground">
					<MessagePrimitive.Parts />
				</div>
				<div className="absolute top-1/2 left-0 -translate-x-full -translate-y-1/2 pr-2">
					<UserActionBar />
				</div>
			</div>

			<BranchPicker className="col-span-full col-start-1 row-start-3 -mr-1 justify-end" />
		</MessagePrimitive.Root>
	);
};

const UserActionBar: FC = () => {
	return (
		<ActionBarPrimitive.Root
			hideWhenRunning
			autohide="not-last"
			className="flex flex-col items-end"
		>
			<ActionBarPrimitive.Edit asChild>
				<TooltipIconButton tooltip="Edit" className="p-4">
					<PencilIcon />
				</TooltipIconButton>
			</ActionBarPrimitive.Edit>
		</ActionBarPrimitive.Root>
	);
};

const EditComposer: FC = () => {
	return (
		<MessagePrimitive.Root className="mx-auto flex w-full max-w-[var(--thread-max-width)] flex-col px-2 py-3">
			<ComposerPrimitive.Root className="ml-auto flex w-full max-w-[85%] flex-col rounded-2xl bg-muted">
				<ComposerPrimitive.Input
					className="min-h-14 w-full resize-none bg-transparent p-4 text-foreground text-sm outline-none"
					autoFocus
				/>
				<div className="mx-3 mb-3 flex items-center gap-2 self-end">
					<ComposerPrimitive.Cancel asChild>
						<Button variant="ghost" size="sm">Cancel</Button>
					</ComposerPrimitive.Cancel>
					<ComposerPrimitive.Send asChild>
						<Button size="sm">Update</Button>
					</ComposerPrimitive.Send>
				</div>
			</ComposerPrimitive.Root>
		</MessagePrimitive.Root>
	);
};

const BranchPicker: FC<{ className?: string }> = ({
	className,
	...rest
}) => {
	return (
		<BranchPickerPrimitive.Root
			hideWhenSingleBranch
			className={cn("mr-2 -ml-2 inline-flex items-center text-xs text-muted-foreground", className)}
			{...rest}
		>
			<BranchPickerPrimitive.Previous asChild>
				<TooltipIconButton tooltip="Previous">
					<ChevronLeftIcon />
				</TooltipIconButton>
			</BranchPickerPrimitive.Previous>
			<span className="font-medium">
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
