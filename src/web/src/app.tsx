import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
import {
	usePuxStore,
	type WorkbenchTab,
	type Conversation,
} from "@/lib/pux-store";
import { puxChatAdapter } from "@/lib/pux-chat-adapter";
import { createPuxHistoryAdapter } from "@/lib/pux-history-adapter";
import { Thread } from "@/components/assistant-ui/thread";
import { VNCViewer } from "@/components/workbench/vnc-viewer";
import { EditorPanel } from "@/components/workbench/editor-panel";
import { SchedulerPanel } from "@/components/workbench/scheduler-panel";
import { TerminalDrawer } from "@/components/workbench/terminal-drawer";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarHeader,
	SidebarInset,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarMenuSub,
	SidebarMenuSubButton,
	SidebarMenuSubItem,
	SidebarProvider,
	SidebarRail,
	SidebarTrigger,
	useSidebar,
} from "@/components/ui/sidebar";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
	PanelRightOpen,
	PanelRightClose,
	PanelLeftOpen,
	PanelLeftClose,
	Zap,
	Monitor,
	Code2,
	Calendar,
	ChevronRight,
	FolderOpen,
	Folder,
	MessageSquare,
	TerminalIcon,
	Trash2,
} from "lucide-react";

// ── Runtime Provider ──
// Re-keyed on conversationKey to reload history when switching conversations.
// Placed inside SidebarInset so sidebar state survives re-key.

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const historyAdapter = useMemo(() => createPuxHistoryAdapter(), []);
	const runtime = useLocalRuntime(puxChatAdapter, {
		adapters: { history: historyAdapter },
	});
	return (
		<AssistantRuntimeProvider runtime={runtime}>
			{children}
		</AssistantRuntimeProvider>
	);
}

// ── Helpers ──

function SidebarToggle() {
	const { state } = useSidebar();
	return (
		<SidebarTrigger>
			{state === "expanded" ? (
				<PanelLeftClose className="size-4" />
			) : (
				<PanelLeftOpen className="size-4" />
			)}
		</SidebarTrigger>
	);
}

function relativeTime(iso: string): string {
	if (!iso) return "";
	const diff = Date.now() - new Date(iso).getTime();
	const mins = Math.floor(diff / 60000);
	if (mins < 1) return "now";
	if (mins < 60) return `${mins}m`;
	const hrs = Math.floor(mins / 60);
	if (hrs < 24) return `${hrs}h`;
	const days = Math.floor(hrs / 24);
	return `${days}d`;
}

// ── Project Group (collapsible) ──

function ProjectGroup({
	projectKey,
	project,
	conversations,
	isActive,
	onSelectConversation,
}: {
	projectKey: string;
	project:
		| { name: string; path: string; description?: string; hasManifest?: boolean }
		| undefined;
	conversations: Conversation[];
	isActive: boolean;
	onSelectConversation: (project: string, agentId: string) => void;
}) {
	const displayName = projectKey.split("/").pop() || projectKey;
	const deleteConversation = usePuxStore((s) => s.deleteConversation);

	return (
		<Collapsible defaultOpen={isActive} className="group/collapsible">
			<SidebarMenuItem>
				<CollapsibleTrigger asChild>
					<SidebarMenuButton
						isActive={isActive}
						tooltip={displayName}
					>
						<ChevronRight className="transition-transform group-data-[state=open]/collapsible:rotate-90 group-data-[collapsible=icon]:hidden" />
						{isActive ? (
							<FolderOpen className="text-yellow-500" />
						) : (
							<Folder className="text-yellow-500" />
						)}
						<span>{displayName}</span>
						{project?.hasManifest && (
							<span className="ml-auto rounded bg-sidebar-primary/20 px-1 text-[9px] text-sidebar-primary group-data-[collapsible=icon]:hidden">
								org
							</span>
						)}
					</SidebarMenuButton>
				</CollapsibleTrigger>
				{conversations.length > 0 && (
					<CollapsibleContent className="group-data-[collapsible=icon]:hidden">
						<SidebarMenuSub>
							{conversations.map((c) => (
								<SidebarMenuSubItem key={`${c.project}-${c.agentId}`} className="group/sub">
									<SidebarMenuSubButton
										onClick={() =>
											onSelectConversation(c.project, c.agentId)
										}
									>
										<MessageSquare className="size-3" />
										<div className="flex min-w-0 flex-1 flex-col">
											<span className="truncate text-[12px]">
												{c.title || c.lastMessage || "Untitled"}
											</span>
											<span className="text-[10px] text-muted-foreground">
												{relativeTime(c.lastAt)}
												{c.messageCount > 0 &&
													` · ${c.messageCount} msgs`}
											</span>
										</div>
										<button
											onClick={(e) => {
												e.stopPropagation();
												deleteConversation(c.project, c.agentId);
											}}
											className="ml-1 shrink-0 rounded p-0.5 opacity-0 hover:bg-destructive/10 hover:text-destructive group-hover/sub:opacity-100"
											title="Delete chat"
										>
											<Trash2 className="size-3" />
										</button>
									</SidebarMenuSubButton>
								</SidebarMenuSubItem>
							))}
						</SidebarMenuSub>
					</CollapsibleContent>
				)}
			</SidebarMenuItem>
		</Collapsible>
	);
}

// ── Sidebar ──

function AppSidebar() {
	const conversations = usePuxStore((s) => s.conversations);
	const projects = usePuxStore((s) => s.projects);
	const activeProject = usePuxStore((s) => s.activeProject);
	const setConversation = usePuxStore((s) => s.setConversation);

	// Group conversations by project
	const convsByProject = useMemo(() => {
		const map = new Map<string, Conversation[]>();
		for (const c of conversations) {
			const existing = map.get(c.project) || [];
			existing.push(c);
			map.set(c.project, existing);
		}
		return map;
	}, [conversations]);

	// All project keys: known projects first, then any projects only in conversations
	const allProjectKeys = useMemo(() => {
		const knownNames = new Set(projects.map((p) => p.name));
		return [
			...projects.map((p) => p.name),
			...Array.from(convsByProject.keys()).filter((k) => !knownNames.has(k)),
		];
	}, [projects, convsByProject]);

	return (
		<Sidebar collapsible="icon">
			<SidebarHeader>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton size="lg" tooltip="Pux">
							<div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
								<Zap className="size-4" />
							</div>
							<div className="flex flex-col gap-0.5 leading-none group-data-[collapsible=icon]:hidden">
								<span className="font-semibold">Pux</span>
							</div>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarHeader>
			<SidebarContent className="group-data-[collapsible=icon]:hidden">
				<SidebarGroup>
					<SidebarMenu>
						{allProjectKeys.length === 0 ? (
							<div className="px-2 py-3 text-center text-xs text-muted-foreground">
								No projects yet
							</div>
						) : (
							allProjectKeys.map((projectKey) => (
								<ProjectGroup
									key={projectKey}
									projectKey={projectKey}
									project={projects.find((p) => p.name === projectKey)}
									conversations={
										convsByProject.get(projectKey) || []
									}
									isActive={activeProject === projectKey}
									onSelectConversation={setConversation}
								/>
							))
						)}
					</SidebarMenu>
				</SidebarGroup>
			</SidebarContent>
			<SidebarFooter className="group-data-[collapsible=icon]:hidden">
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton size="sm" tooltip="Settings">
							<span className="text-xs text-muted-foreground">
								v0.1
							</span>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarFooter>
			<SidebarRail />
		</Sidebar>
	);
}

// ── Workbench content with tab bar ──

function Workbench() {
	const storeTab = usePuxStore((s) => s.activeWorkbenchTab);
	const setStoreTab = usePuxStore((s) => s.setWorkbenchTab);

	const tabs: { id: WorkbenchTab; icon: React.ReactNode; label: string }[] = [
		{ id: "vnc", icon: <Monitor className="size-4" />, label: "Sandbox" },
		{ id: "editor", icon: <Code2 className="size-4" />, label: "Editor" },
		{ id: "scheduler", icon: <Calendar className="size-4" />, label: "Scheduler" },
	];

	return (
		<div className="flex h-full flex-col bg-sidebar">
			{/* Tab bar inside the workbench */}
			<div className="flex h-9 shrink-0 items-center gap-0.5 border-b border-border px-2">
				{tabs.map((t) => (
					<button
						key={t.id}
						onClick={() => setStoreTab(t.id)}
						className={cn(
							"inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors",
							storeTab === t.id
								? "bg-accent text-accent-foreground"
								: "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground",
						)}
					>
						{t.icon}
						{t.label}
					</button>
				))}
			</div>
			{/* Tab content */}
			<div className="flex-1 overflow-hidden">
				{storeTab === "vnc" && <VNCViewer />}
				{storeTab === "editor" && <EditorPanel />}
				{storeTab === "scheduler" && <SchedulerPanel />}
			</div>
		</div>
	);
}

// ── App ──

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);
	const loadConversations = usePuxStore((s) => s.loadConversations);
	const loadProjects = usePuxStore((s) => s.loadProjects);
	const conversationKey = usePuxStore((s) => s.conversationKey);
	const activeProject = usePuxStore((s) => s.activeProject);
	const [workbenchVisible, setWorkbenchVisible] = useState(true);
	const [showTerminal, setShowTerminal] = useState(false);
	const [workbenchWidth, setWorkbenchWidth] = useState(800);
	const workbenchWidthRef = useRef(800);
	const workbenchProviderRef = useRef<HTMLDivElement>(null);

	const toggleWorkbench = useCallback(() => {
		setWorkbenchVisible((prev) => !prev);
	}, []);

	// Ctrl+` to toggle terminal
	useEffect(() => {
		const handler = (e: KeyboardEvent) => {
			if (e.key === "`" && (e.ctrlKey || e.metaKey)) {
				e.preventDefault();
				setShowTerminal((prev) => !prev);
			}
		};
		window.addEventListener("keydown", handler);
		return () => window.removeEventListener("keydown", handler);
	}, []);

	useEffect(() => {
		loadModels();
		loadProjects();
		loadConversations();
	}, [loadModels, loadConversations, loadProjects]);

	// Drag-resize handle for workbench sidebar
	const handleResizeStart = useCallback((e: React.MouseEvent) => {
		e.preventDefault();
		const startX = e.clientX;
		const startWidth = workbenchWidthRef.current;

		const handleMove = (moveEvent: MouseEvent) => {
			const delta = startX - moveEvent.clientX;
			const newWidth = Math.max(
				250,
				Math.min(window.innerWidth * 0.75, startWidth + delta),
			);
			workbenchWidthRef.current = newWidth;
			// Direct DOM updates — avoids re-renders during drag
			const el = workbenchProviderRef.current;
			if (el) el.style.width = `${newWidth}px`;
			const inset = el?.previousElementSibling as HTMLElement | null;
			if (inset) inset.style.marginRight = `${newWidth}px`;
		};

		const handleUp = () => {
			setWorkbenchWidth(workbenchWidthRef.current);
			document.removeEventListener("mousemove", handleMove);
			document.removeEventListener("mouseup", handleUp);
			document.body.style.cursor = "";
			document.body.style.userSelect = "";
		};

		document.body.style.cursor = "col-resize";
		document.body.style.userSelect = "none";
		document.addEventListener("mousemove", handleMove);
		document.addEventListener("mouseup", handleUp);
	}, []);

	return (
		<SidebarProvider
			className="relative h-svh overflow-hidden"
			defaultOpen={true}
		>
			<AppSidebar />
			<SidebarInset
				className="flex h-svh flex-col overflow-hidden transition-[margin-right] duration-200 ease-linear"
				style={
					workbenchVisible
						? { marginRight: `${workbenchWidth}px` } as React.CSSProperties
						: undefined
				}
			>
				{/* Navbar */}
				<header className="flex h-10 shrink-0 items-center gap-2 border-b border-border px-4">
					<SidebarToggle />
					<Button
						variant="ghost"
						size="icon"
						className="ml-1 h-7 w-7"
						onClick={() => setShowTerminal((v) => !v)}
						aria-label="Toggle terminal"
					>
						<TerminalIcon className="size-4" />
					</Button>
					{/* Workbench tab indicator */}
					{workbenchVisible && (
						<>
							<div className="mx-2 h-5 w-px bg-border" />
							<span className="text-xs font-medium text-muted-foreground">
								Workbench
							</span>
						</>
					)}
					<Button
						variant="ghost"
						size="icon"
						className="ml-auto h-7 w-7"
						onClick={toggleWorkbench}
						aria-label={
							workbenchVisible
								? "Close workbench"
								: "Open workbench"
						}
					>
						{workbenchVisible ? (
							<PanelRightClose className="size-4" />
						) : (
							<PanelRightOpen className="size-4" />
						)}
					</Button>
				</header>
				<PuxRuntimeProvider key={conversationKey}>
					<div className="flex h-full flex-col">
						<div className="flex-1 overflow-hidden">
							<Thread />
						</div>
						{showTerminal && (
							<TerminalDrawer
								cwd={activeProject}
								onClose={() => setShowTerminal(false)}
							/>
						)}
					</div>
				</PuxRuntimeProvider>
			</SidebarInset>
			{/* Workbench — fixed right panel with slide animation */}
			<div
				ref={workbenchProviderRef}
				className={cn(
					"fixed inset-y-0 right-0 z-10 flex flex-col border-l border-border bg-sidebar text-sidebar-foreground transition-transform duration-200 ease-linear",
					!workbenchVisible && "translate-x-full",
				)}
				style={{ width: `${workbenchWidth}px` }}
			>
				{/* Drag-resize handle */}
				<div
					className="absolute inset-y-0 left-0 z-30 w-1.5 cursor-col-resize hover:bg-ring/50 active:bg-ring transition-colors"
					onMouseDown={handleResizeStart}
				/>
				<Workbench />
			</div>
		</SidebarProvider>
	);
}
