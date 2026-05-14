import { useEffect, useState, useCallback, useMemo } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
import { Panel, Group, Separator } from "react-resizable-panels";
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
import { TerminalPanel } from "@/components/workbench/terminal-panel";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
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
	Zap,
	Monitor,
	Code2,
	Calendar,
	ChevronRight,
	FolderOpen,
	Folder,
	MessageSquare,
	TerminalIcon,
	XIcon,
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

	return (
		<Collapsible defaultOpen className="group/collapsible">
			<SidebarMenuItem>
				<CollapsibleTrigger asChild>
					<SidebarMenuButton
						isActive={isActive}
						tooltip={displayName}
					>
						<ChevronRight className="transition-transform group-data-[state=open]/collapsible:rotate-90" />
						{isActive ? (
							<FolderOpen className="text-yellow-500" />
						) : (
							<Folder className="text-yellow-500" />
						)}
						<span>{displayName}</span>
						{project?.hasManifest && (
							<span className="ml-auto rounded bg-sidebar-primary/20 px-1 text-[9px] text-sidebar-primary">
								org
							</span>
						)}
					</SidebarMenuButton>
				</CollapsibleTrigger>
				{conversations.length > 0 && (
					<CollapsibleContent>
						<SidebarMenuSub>
							{conversations.map((c) => (
								<SidebarMenuSubItem key={`${c.project}-${c.agentId}`}>
									<SidebarMenuSubButton
										onClick={() =>
											onSelectConversation(c.project, c.agentId)
										}
									>
										<MessageSquare className="size-3" />
										<div className="flex min-w-0 flex-col">
											<span className="truncate text-[12px]">
												{c.title || c.lastMessage || "Untitled"}
											</span>
											<span className="text-[10px] text-muted-foreground">
												{relativeTime(c.lastAt)}
												{c.messageCount > 0 &&
													` · ${c.messageCount} msgs`}
											</span>
										</div>
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
			<SidebarContent>
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
			<SidebarFooter>
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

// ── Workbench ──

function Workbench() {
	const storeTab = usePuxStore((s) => s.activeWorkbenchTab);
	const setStoreTab = usePuxStore((s) => s.setWorkbenchTab);

	const tabs: { id: WorkbenchTab; icon: React.ReactNode; label: string }[] = [
		{ id: "vnc", icon: <Monitor size={14} />, label: "Sandbox" },
		{ id: "editor", icon: <Code2 size={14} />, label: "Editor" },
		{
			id: "scheduler",
			icon: <Calendar size={14} />,
			label: "Scheduler",
		},
	];

	return (
		<div className="flex h-full flex-col bg-background">
			<div className="flex h-9 items-center gap-0.5 border-b border-border px-1">
				{tabs.map((t) => (
					<button
						key={t.id}
						onClick={() => setStoreTab(t.id)}
						className={cn(
							"inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors",
							storeTab === t.id
								? "bg-accent text-accent-foreground"
								: "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground",
						)}
						aria-label={t.label}
					>
						{t.icon}
					</button>
				))}
				<span className="ml-2 text-sm font-semibold text-foreground">
					{tabs.find((t) => t.id === storeTab)?.label}
				</span>
			</div>
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

	return (
		<SidebarProvider className="h-svh overflow-hidden" defaultOpen={true}>
			<AppSidebar />
			<SidebarInset className="flex h-svh flex-col overflow-hidden">
				<header className="flex h-10 shrink-0 items-center gap-2 border-b border-border px-4">
					<SidebarTrigger />
					<Button
						variant="ghost"
						size="icon"
						className="ml-1 h-7 w-7"
						onClick={() => setShowTerminal((v) => !v)}
						aria-label="Toggle terminal"
					>
						<TerminalIcon className="size-4" />
					</Button>
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
					{workbenchVisible ? (
						<Group
							orientation="horizontal"
							className="flex-1 overflow-hidden"
						>
							<Panel defaultSize={55} minSize={30}>
								<div className="flex h-full flex-col">
									<div className="flex-1 overflow-hidden">
										<Thread />
									</div>
									{showTerminal && (
										<div className="flex h-56 shrink-0 flex-col border-t border-border">
											<div className="flex h-7 items-center justify-between border-b border-border bg-muted/20 px-2">
												<span className="text-[11px] font-medium text-muted-foreground">Terminal</span>
												<button
													onClick={() => setShowTerminal(false)}
													className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
												>
													<XIcon size={12} />
												</button>
											</div>
											<div className="flex-1 overflow-hidden">
												<TerminalPanel cwd={activeProject} />
											</div>
										</div>
									)}
								</div>
							</Panel>
							<Separator className="w-px bg-border hover:bg-ring/50 active:bg-ring transition-colors cursor-col-resize" />
							<Panel defaultSize={45} minSize={15}>
								<Workbench />
							</Panel>
						</Group>
					) : (
						<div className="flex h-full flex-col">
							<div className="flex-1 overflow-hidden">
								<Thread />
							</div>
							{showTerminal && (
								<div className="flex h-56 shrink-0 flex-col border-t border-border">
									<div className="flex h-7 items-center justify-between border-b border-border bg-muted/20 px-2">
										<span className="text-[11px] font-medium text-muted-foreground">Terminal</span>
										<button
											onClick={() => setShowTerminal(false)}
											className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
										>
											<XIcon size={12} />
										</button>
									</div>
									<div className="flex-1 overflow-hidden">
										<TerminalPanel cwd={activeProject} />
									</div>
								</div>
							)}
						</div>
					)}
				</PuxRuntimeProvider>
			</SidebarInset>
		</SidebarProvider>
	);
}
