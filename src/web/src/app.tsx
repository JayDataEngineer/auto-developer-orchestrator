import { useState, useEffect } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
	ThreadPrimitive,
	ComposerPrimitive,
	MessagePrimitive,
} from "@assistant-ui/react";
import type {
	ReasoningMessagePartProps,
	TextMessagePartProps,
	ToolCallMessagePartProps,
} from "@assistant-ui/react";
import { Group, Panel, Separator } from "react-resizable-panels";
import {
	Send,
	Square,
	Zap,
	Monitor,
	Calendar,
	ChevronRight,
	Wrench,
	Loader2,
	CheckCircle2,
	XCircle,
	Bot,
	User,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { usePuxStore } from "@/lib/pux-store";
import { puxChatAdapter } from "@/lib/pux-chat-adapter";
import { QuestionDialog } from "./components/question-dialog";
import { ApprovalDialog } from "./components/approval-dialog";
import { PlanDialog } from "./components/plan-dialog";
import { SandboxPanel } from "./components/workbench/sandbox-panel";
import { MetricsFooter } from "./components/workbench/metrics-footer";

// ── Runtime Provider (Unsloth pattern) ──

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const runtime = useLocalRuntime(puxChatAdapter);
	return (
		<AssistantRuntimeProvider runtime={runtime}>
			{children}
		</AssistantRuntimeProvider>
	);
}

// ── App ──

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);

	useEffect(() => {
		loadModels();
	}, [loadModels]);

	return (
		<PuxRuntimeProvider>
			<AppLayout />
		</PuxRuntimeProvider>
	);
}

// ── Layout ──

function AppLayout() {
	const project = usePuxStore((s) => s.activeProject);
	const setProject = usePuxStore((s) => s.setProject);
	const lastError = usePuxStore((s) => s.lastError);
	const clearError = usePuxStore((s) => s.clearError);

	return (
		<div className="flex h-screen overflow-hidden bg-bg">
			<Sidebar project={project} onSelectProject={setProject} />

			<Group orientation="horizontal" className="flex-1">
				<Panel defaultSize={45} minSize={30}>
					<div className="flex h-full flex-col">
						<ChatPanel />
						<MetricsFooter />
					</div>
				</Panel>

				<Separator className="w-px bg-border hover:bg-dim transition-colors" />

				<Panel defaultSize={55} minSize={25}>
					<Workbench />
				</Panel>
			</Group>

			<QuestionDialog />
			<ApprovalDialog />
			<PlanDialog />

			{lastError && (
				<div className="fixed bottom-4 right-4 z-50 max-w-sm rounded-lg border border-error/30 bg-surface px-4 py-3 text-sm text-error shadow-xl">
					<div className="flex items-start gap-2">
						<span className="flex-1">{lastError}</span>
						<button onClick={clearError} className="text-dim hover:text-text">
							&times;
						</button>
					</div>
				</div>
			)}
		</div>
	);
}

// ── Chat Panel ──

function ChatPanel() {
	return (
		<ThreadPrimitive.Root className="flex flex-1 flex-col overflow-hidden">
			<ThreadPrimitive.Viewport className="flex-1 overflow-y-auto">
				<ThreadPrimitive.Empty>
					<WelcomeScreen />
				</ThreadPrimitive.Empty>
				<ThreadPrimitive.Messages
					components={{
						UserMessage: UserMessageView,
						AssistantMessage: AssistantMessageView,
					}}
				/>
			</ThreadPrimitive.Viewport>

			<div className="border-t border-border px-4 pb-4 pt-3">
				<div className="mx-auto max-w-3xl">
					<ComposerPrimitive.Root
						className={cn(
							"flex items-end gap-2 rounded-2xl border bg-surface px-4 py-2.5",
							"border-border transition-colors focus-within:border-accent/50",
						)}
					>
						<ComposerPrimitive.Input
							placeholder="Send a message..."
							className="max-h-[200px] min-h-[20px] flex-1 resize-none bg-transparent text-sm leading-relaxed text-text outline-none placeholder:text-dim"
							rows={1}
						/>
						<ComposerPrimitive.Send className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-accent text-white transition-all disabled:opacity-30 hover:opacity-90">
							<Send size={14} />
						</ComposerPrimitive.Send>
						<ComposerPrimitive.Cancel className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-error/80 text-white transition-all hover:bg-error">
							<Square size={14} />
						</ComposerPrimitive.Cancel>
					</ComposerPrimitive.Root>
				</div>
			</div>
		</ThreadPrimitive.Root>
	);
}

// ── Welcome Screen ──

function WelcomeScreen() {
	return (
		<div className="flex h-full flex-col items-center justify-center gap-4 px-8">
			<div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-accent/10">
				<Zap size={24} className="text-accent" />
			</div>
			<div className="text-center">
				<h2 className="text-lg font-semibold tracking-tight text-text">
					Pux
				</h2>
				<p className="mt-1 text-sm text-dim">
					Your AI-powered development orchestrator
				</p>
			</div>
		</div>
	);
}

// ── Message Views ──

function UserMessageView() {
	return (
		<div className="flex gap-3 px-4 py-2">
			<div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-accent/10">
				<User size={12} className="text-accent" />
			</div>
			<div className="min-w-0 flex-1">
				<div className="mb-1 text-[10px] font-semibold uppercase tracking-widest text-dim">
					You
				</div>
				<div className="rounded-xl rounded-tl-sm border border-border bg-surface px-3.5 py-2.5 text-sm leading-relaxed text-text whitespace-pre-wrap">
					<MessagePrimitive.Parts
						components={{
							Text: UserTextPart,
						}}
					/>
				</div>
			</div>
		</div>
	);
}

function AssistantMessageView() {
	return (
		<div className="flex gap-3 px-4 py-2">
			<div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-border">
				<Bot size={12} className="text-dim" />
			</div>
			<div className="min-w-0 flex-1 space-y-2">
				<div className="mb-1 text-[10px] font-semibold uppercase tracking-widest text-dim">
					Pux
				</div>
				<MessagePrimitive.Parts
					components={{
						Text: AssistantTextPart,
						Reasoning: ReasoningView,
						tools: {
							Fallback: ToolFallbackView,
						},
					}}
				/>
			</div>
		</div>
	);
}

// ── Text Part Components ──

function UserTextPart({ text }: TextMessagePartProps) {
	return <>{text}</>;
}

function AssistantTextPart({ text, status }: TextMessagePartProps) {
	if (!text && status.type === "running") {
		return (
			<span className="inline-flex items-center gap-1">
				<span className="h-1.5 w-1.5 animate-pulse rounded-full bg-accent" />
				<span className="h-1.5 w-1.5 animate-pulse rounded-full bg-accent [animation-delay:150ms]" />
				<span className="h-1.5 w-1.5 animate-pulse rounded-full bg-accent [animation-delay:300ms]" />
			</span>
		);
	}
	return (
		<div className="text-sm leading-relaxed text-text whitespace-pre-wrap">
			{text}
		</div>
	);
}

// ── Reasoning (thinking) ──

function ReasoningView({ text, status }: ReasoningMessagePartProps) {
	const [open, setOpen] = useState(false);
	if (!text) return null;
	const isRunning = status.type === "running";

	return (
		<div className="my-1">
			<button
				onClick={() => setOpen(!open)}
				className={cn(
					"inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-[11px]",
					"text-dim transition-colors hover:border-dim hover:text-text",
					open ? "border-dim bg-surface" : "border-border",
				)}
			>
				<ChevronRight
					size={10}
					className={cn("transition-transform", open && "rotate-90")}
				/>
				Thinking
				{isRunning && (
					<span className="ml-0.5 inline-flex gap-0.5">
						<span className="h-1 w-1 animate-pulse rounded-full bg-accent" />
						<span className="h-1 w-1 animate-pulse rounded-full bg-accent [animation-delay:150ms]" />
						<span className="h-1 w-1 animate-pulse rounded-full bg-accent [animation-delay:300ms]" />
					</span>
				)}
			</button>
			{open && (
				<div className="mt-1.5 max-h-[300px] overflow-y-auto rounded-md border border-border bg-surface/50 p-3 text-xs leading-relaxed text-dim whitespace-pre-wrap">
					{text}
				</div>
			)}
		</div>
	);
}

// ── Tool Fallback ──

function truncate(s: unknown, max = 200): string {
	const str = typeof s === "string" ? s : JSON.stringify(s, null, 2) || "";
	return str.length > max ? str.slice(0, max) + "..." : str;
}

function StatusIcon({ status }: { status: ToolCallMessagePartProps["status"] }) {
	if (status.type === "running")
		return <Loader2 size={11} className="animate-spin text-warn" />;
	if (status.type === "complete")
		return <CheckCircle2 size={11} className="text-success" />;
	return <XCircle size={11} className="text-error" />;
}

function ToolFallbackView({
	toolName,
	args,
	result,
	isError,
	status,
}: ToolCallMessagePartProps) {
	const [open, setOpen] = useState(false);
	const hasResult = result !== undefined || isError;

	return (
		<div className="my-1 rounded-lg border border-border bg-surface">
			<button
				onClick={() => setOpen(!open)}
				className="flex w-full items-center gap-2 px-3 py-2 text-left"
			>
				<Wrench size={11} className="shrink-0 text-dim" />
				<span className="text-xs font-medium text-text">{toolName}</span>
				<span className="ml-auto">
					<StatusIcon status={status} />
				</span>
			</button>

			{/* Args preview */}
			{args && (
				<div className="border-t border-border/50 px-3 py-1.5">
					<pre className="overflow-hidden text-[11px] leading-relaxed text-dim whitespace-pre-wrap">
						{truncate(args, 150)}
					</pre>
				</div>
			)}

			{/* Result (collapsible) */}
			{open && hasResult && (
				<div className="max-h-[200px] overflow-y-auto border-t border-border/50 px-3 py-2">
					{isError ? (
						<pre className="text-[11px] leading-relaxed text-error whitespace-pre-wrap">
							{truncate(result, 2000)}
						</pre>
					) : (
						<pre className="text-[11px] leading-relaxed text-text whitespace-pre-wrap">
							{truncate(result, 2000)}
						</pre>
					)}
				</div>
			)}
		</div>
	);
}

// ── Sidebar ──

function Sidebar({
	project,
	onSelectProject,
}: {
	project: string;
	onSelectProject: (p: string) => void;
}) {
	return (
		<div className="flex h-full w-[220px] shrink-0 flex-col border-r border-border bg-surface">
			<div className="flex h-10 items-center gap-2.5 px-4">
				<div className="flex h-6 w-6 items-center justify-center rounded-md bg-accent/10">
					<Zap size={12} className="text-accent" />
				</div>
				<span className="text-sm font-bold tracking-tight text-text">Pux</span>
			</div>

			<div className="border-t border-border px-3 py-2">
				<input
					value={project}
					onChange={(e) => onSelectProject(e.target.value)}
					placeholder="Project name..."
					className="w-full rounded-md border border-border bg-bg px-2.5 py-1.5 text-xs text-text outline-none transition-colors focus:border-accent/50"
				/>
			</div>

			<div className="flex-1 overflow-y-auto">
				<div className="px-4 py-2 text-[10px] font-semibold uppercase tracking-widest text-dim">
					Recent
				</div>
				<div className="px-4 text-xs text-dim">Chat history loads here</div>
			</div>

			<div className="border-t border-border px-4 py-2 text-[10px] text-dim">
				Contract 2.0 Interface
			</div>
		</div>
	);
}

// ── Workbench ──

type WorkbenchTab = "sandbox" | "schedule";

function Workbench() {
	const [tab, setTab] = useState<WorkbenchTab>("sandbox");

	return (
		<div className="flex h-full flex-col">
			<div className="flex h-9 items-center gap-1 border-b border-border px-3">
				<TabBtn
					active={tab === "sandbox"}
					onClick={() => setTab("sandbox")}
					icon={<Monitor size={12} />}
					label="Sandbox"
				/>
				<TabBtn
					active={tab === "schedule"}
					onClick={() => setTab("schedule")}
					icon={<Calendar size={12} />}
					label="Schedule"
				/>
			</div>

			<div className="flex-1 overflow-hidden">
				{tab === "sandbox" && <SandboxPanel />}
				{tab === "schedule" && <SchedulePlaceholder />}
			</div>
		</div>
	);
}

function TabBtn({
	active,
	onClick,
	icon,
	label,
}: {
	active: boolean;
	onClick: () => void;
	icon: React.ReactNode;
	label: string;
}) {
	return (
		<button
			onClick={onClick}
			className={cn(
				"flex items-center gap-1.5 rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors",
				active
					? "bg-border text-text"
					: "text-dim hover:text-text",
			)}
		>
			{icon}
			{label}
		</button>
	);
}

function SchedulePlaceholder() {
	return (
		<div className="flex h-full flex-col items-center justify-center gap-2 text-dim">
			<Calendar size={24} />
			<span className="text-xs">Schedule view coming soon</span>
		</div>
	);
}
