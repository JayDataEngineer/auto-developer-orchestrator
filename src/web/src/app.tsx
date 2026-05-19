import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
	SimpleImageAttachmentAdapter,
} from "@assistant-ui/react";
import {
	usePuxStore,
	type WorkbenchTab,
	type Conversation,
	type RunningAgentInfo,
} from "@/lib/pux-store";
import { relativeTime } from "@pux/shared";
import { puxChatAdapter } from "@/lib/pux-chat-adapter";
import { createPuxHistoryAdapter, storedMessagesToThreadLikes } from "@/lib/pux-history-adapter";
import { getFetch, apiUrl } from "@pux/shared";
import { Thread } from "@/components/assistant-ui/thread";
import { VNCViewer } from "@/components/workbench/vnc-viewer";
import { EditorPanel } from "@/components/workbench/editor-panel";
import { SchedulerPanel } from "@/components/workbench/scheduler-panel";
import { WorkersPanel } from "@/components/workbench/workers-panel";
import { SettingsPanel } from "@/components/workbench/settings-panel";
import { TerminalDrawer } from "@/components/workbench/terminal-drawer";
import { AddProjectDialog } from "@/components/add-project-dialog";
import { WidgetToolUIs } from "@/components/assistant-ui/widget-tool-ui";
import { AskUserToolUI } from "@/components/assistant-ui/ask-user-tool-ui";
import { PlanReviewToolUI } from "@/components/assistant-ui/plan-review-tool-ui";
import { DelegateToolUI, DelegateAsyncToolUI } from "@/components/assistant-ui/delegate-tool-ui";
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
	Tabs,
	TabsList,
	TabsTrigger,
	TabsContent,
} from "@/components/ui/tabs";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Button } from "@/components/ui/button";
import { Panel, Group, Separator, usePanelRef } from "react-resizable-panels";
import {
	PanelRight,
	PanelLeftOpen,
	PanelLeftClose,
	Zap,
	Monitor,
	Code2,
	Calendar,
	ChevronRight,
	TerminalIcon,
	Trash2,
	Plus,
	Users,
	FolderOpen,
	XIcon,
	Settings,
} from "lucide-react";

// ── Runtime Provider ──
// NOT re-keyed — uses runtime.thread.reset() to switch conversations.
// Placed inside SidebarInset so sidebar state survives conversation switches.

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const historyAdapter = useMemo(() => createPuxHistoryAdapter(), []);
	const runtime = useLocalRuntime(puxChatAdapter, {
		adapters: {
			history: historyAdapter,
			attachments: new SimpleImageAttachmentAdapter(),
		},
	});

	// When the active conversation changes, reset the thread and reload history.
	// This avoids the AuiProvider crash caused by key-based remounting.
	const conversationKey = usePuxStore((s) => s.conversationKey);
	const prevKeyRef = useRef(conversationKey);
	useEffect(() => {
		if (conversationKey === prevKeyRef.current) return;
		prevKeyRef.current = conversationKey;

		(async () => {
			const store = usePuxStore.getState();
			const project = store.activeProject;
			if (!project) {
				runtime.thread.reset();
				return;
			}
			try {
				const params = new URLSearchParams({ project, limit: "200" });
				if (store.activeAgentId) params.set("agentId", store.activeAgentId);
				const fetch = getFetch();
				const resp = await fetch(apiUrl(`/api/pux/history?${params}`));
				if (!resp.ok) {
					runtime.thread.reset();
					return;
				}
				const data = await resp.json();
				if (!Array.isArray(data) || data.length === 0) {
					runtime.thread.reset();
					return;
				}
				const messages = storedMessagesToThreadLikes(data);
				runtime.thread.reset(messages);
			} catch {
				runtime.thread.reset();
			}
		})();
	}, [conversationKey, runtime]);

	// Listen for pux:send-message events from workbench panels
	useEffect(() => {
		const handler = (e: Event) => {
			const text = (e as CustomEvent).detail?.text;
			if (text) {
				runtime.thread.composer.setText(text);
				runtime.thread.composer.send();
			}
		};
		window.addEventListener("pux:send-message", handler);
		return () => window.removeEventListener("pux:send-message", handler);
	}, [runtime]);

	return (
		<AssistantRuntimeProvider runtime={runtime}>
			{WidgetToolUIs.map((UI, i) => (
				<UI key={i} />
			))}
			<AskUserToolUI />
			<PlanReviewToolUI />
			<DelegateToolUI />
			<DelegateAsyncToolUI />
			{children}
		</AssistantRuntimeProvider>
	);
}

// ── Helpers ──

function useAgentStatusPolling(intervalMs = 3000) {
	const update = usePuxStore((s) => s.updateRunningAgents);
	const loadConversations = usePuxStore((s) => s.loadConversations);
	useEffect(() => {
		update();
		const id = setInterval(() => {
			update();
			loadConversations();
		}, intervalMs);
		return () => clearInterval(id);
	}, [update, loadConversations, intervalMs]);
}

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
	const removeProject = usePuxStore((s) => s.removeProject);
	const runningAgents = usePuxStore((s) => s.runningAgents);
	const viewedConversations = usePuxStore((s) => s.viewedConversations);

	return (
		<Collapsible defaultOpen={isActive} className="group/collapsible">
			<SidebarMenuItem>
				<CollapsibleTrigger asChild>
					<SidebarMenuButton
						isActive={isActive}
						tooltip={displayName}
					>
						<ChevronRight className="transition-transform group-data-[state=open]/collapsible:rotate-90 group-data-[collapsible=icon]:hidden" />
						<span className="flex-1 truncate">{displayName}</span>
						{project?.hasManifest && (
							<span className="rounded bg-sidebar-primary/20 px-1 text-[9px] text-sidebar-primary group-data-[collapsible=icon]:hidden">
								org
							</span>
						)}
					</SidebarMenuButton>
				</CollapsibleTrigger>
				<button
					onClick={(e) => {
						e.stopPropagation();
						removeProject(projectKey);
					}}
					className="absolute right-1 top-1/2 -translate-y-1/2 rounded p-1 opacity-0 hover:bg-destructive/10 hover:text-destructive group-hover/collapsible:opacity-100 group-data-[collapsible=icon]:hidden"
					title="Remove from sidebar"
				>
					<XIcon className="size-3" />
				</button>
				{conversations.length > 0 && (
					<CollapsibleContent className="group-data-[collapsible=icon]:hidden">
						<SidebarMenuSub>
							{conversations.map((c) => {
								const convKey = `${c.project}:${c.agentId}`;
								const isRunning = runningAgents.has(convKey);
								const isUnviewed = !viewedConversations.has(convKey) && c.messageCount > 0;
								return (
									<SidebarMenuSubItem key={`${c.project}-${c.agentId}`} className="group/sub">
										<SidebarMenuSubButton
											onClick={() =>
												onSelectConversation(c.project, c.agentId)
											}
										>
											<div className="flex min-w-0 flex-1 items-center gap-1.5">
												{isUnviewed && !isRunning && (
													<span className="inline-flex h-2 w-2 shrink-0 rounded-full bg-white" />
												)}
												{isRunning && (
													<span className="relative flex h-2 w-2 shrink-0">
														<span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-blue-400 opacity-75" />
														<span className="relative inline-flex h-2 w-2 rounded-full bg-blue-500" />
													</span>
												)}
												<div className="flex min-w-0 flex-1 flex-col">
													<span className="truncate text-[12px]">
														{c.title || c.lastMessage || "Untitled"}
													</span>
													<span className="text-[10px] text-muted-foreground">
														{relativeTime(c.lastAt, { now: "now" })}
														{c.messageCount > 0 &&
															` · ${c.messageCount} msgs`}
													</span>
												</div>
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
								);
							})}
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
	const [showAddProject, setShowAddProject] = useState(false);

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
					<SidebarMenuItem>
						<SidebarMenuButton
							onClick={() => usePuxStore.getState().startNewChat()}
							tooltip="New Chat"
						>
							<Plus className="size-4" />
							<span className="group-data-[collapsible=icon]:hidden">New Chat</span>
						</SidebarMenuButton>
					</SidebarMenuItem>
					<SidebarMenuItem>
						<SidebarMenuButton
							onClick={() => setShowAddProject(true)}
							tooltip="Open Folder"
						>
							<FolderOpen className="size-4" />
							<span className="group-data-[collapsible=icon]:hidden">Open Folder</span>
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
			<AddProjectDialog open={showAddProject} onOpenChange={setShowAddProject} />
		</Sidebar>
	);
}

// ── Workbench content with tab bar ──

function Workbench() {
	const storeTab = usePuxStore((s) => s.activeWorkbenchTab);
	const setStoreTab = usePuxStore((s) => s.setWorkbenchTab);

	return (
		<Tabs
			value={storeTab}
			onValueChange={(v) => setStoreTab(v as WorkbenchTab)}
			className="flex h-full flex-col bg-sidebar"
		>
			<TabsList className="h-9 shrink-0 w-full justify-start rounded-none border-b border-border bg-transparent px-2">
				<TabsTrigger value="vnc" className="gap-1.5 text-xs">
					<Monitor className="size-4" />
					Sandbox
				</TabsTrigger>
				<TabsTrigger value="editor" className="gap-1.5 text-xs">
					<Code2 className="size-4" />
					Editor
				</TabsTrigger>
				<TabsTrigger value="scheduler" className="gap-1.5 text-xs">
					<Calendar className="size-4" />
					Scheduler
				</TabsTrigger>
				<TabsTrigger value="workers" className="gap-1.5 text-xs">
					<Users className="size-4" />
					Agents
				</TabsTrigger>
				<TabsTrigger value="settings" className="gap-1.5 text-xs">
					<Settings className="size-4" />
					Settings
				</TabsTrigger>
			</TabsList>
			<TabsContent value="vnc" className="flex-1 overflow-hidden mt-0">
				<VNCViewer />
			</TabsContent>
			<TabsContent value="editor" className="flex-1 overflow-hidden mt-0">
				<EditorPanel />
			</TabsContent>
			<TabsContent value="scheduler" className="flex-1 overflow-hidden mt-0">
				<SchedulerPanel />
			</TabsContent>
			<TabsContent value="workers" className="flex-1 overflow-hidden mt-0">
				<WorkersPanel />
			</TabsContent>
			<TabsContent value="settings" className="flex-1 overflow-hidden mt-0">
				<SettingsPanel />
			</TabsContent>
		</Tabs>
	);
}

// ── App ──

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);
	const loadConversations = usePuxStore((s) => s.loadConversations);
	const loadProjects = usePuxStore((s) => s.loadProjects);
	const activeProject = usePuxStore((s) => s.activeProject);
	const activeProjectPath = usePuxStore((s) => s.activeProjectPath);
	const theme = usePuxStore((s) => s.theme);
	const [workbenchVisible, setWorkbenchVisible] = useState(true);
	const workbenchPanelRef = usePanelRef();

	// Apply theme on mount
	useEffect(() => {
		document.documentElement.setAttribute("data-pux-theme", theme);
	}, [theme]);

	// Poll for running agent status
	useAgentStatusPolling();
	const [showTerminal, setShowTerminal] = useState(false);

	const toggleWorkbench = useCallback(() => {
		const panel = workbenchPanelRef.current;
		if (!panel) return;
		if (panel.isCollapsed()) {
			panel.resize("35%");
		} else {
			panel.collapse();
		}
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
		<SidebarProvider
			className="relative h-svh overflow-hidden"
			defaultOpen={true}
		>
			<AppSidebar />
			<Group orientation="horizontal" className="h-svh">
				<Panel defaultSize={65} minSize={30}>
					<SidebarInset className="flex h-full flex-col overflow-hidden">
						{/* Navbar */}
						<header className="flex h-10 shrink-0 items-center gap-2 border-b border-sidebar-border bg-sidebar px-4">
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
								<PanelRight className="size-4" />
							</Button>
						</header>
						<PuxRuntimeProvider>
							{showTerminal ? (
								<Group orientation="vertical" className="flex-1">
									<Panel defaultSize={70} minSize={30}>
										<Thread />
									</Panel>
									<Separator className="h-1 bg-border hover:bg-ring/50 transition-colors" />
									<Panel defaultSize={30} minSize={15} collapsible>
										<TerminalDrawer
											cwd={activeProjectPath || activeProject}
											onClose={() => setShowTerminal(false)}
										/>
									</Panel>
								</Group>
							) : (
								<div className="flex-1 overflow-hidden">
									<Thread />
								</div>
							)}
						</PuxRuntimeProvider>
					</SidebarInset>
				</Panel>

				<Separator className="w-px bg-border hover:bg-ring/50 transition-colors cursor-col-resize" />
				<Panel
					panelRef={workbenchPanelRef}
					defaultSize={35}
					minSize={20}
					collapsible
					collapsedSize={0}
					onResize={(size) => setWorkbenchVisible(size.asPercentage > 0)}
				>
					<div className="flex h-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground">
						<Workbench />
					</div>
				</Panel>
			</Group>
		</SidebarProvider>
	);
}
