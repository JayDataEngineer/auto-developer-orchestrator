import { useEffect, useState, useCallback } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
import { Panel, Group, Separator } from "react-resizable-panels";
import { usePuxStore, type WorkbenchTab, type Conversation } from "@/lib/pux-store";
import { puxChatAdapter } from "@/lib/pux-chat-adapter";
import { Thread } from "@/components/assistant-ui/thread";
import { VNCViewer } from "@/components/workbench/vnc-viewer";
import { EditorPanel } from "@/components/workbench/editor-panel";
import { SchedulerPanel } from "@/components/workbench/scheduler-panel";
import {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarHeader,
	SidebarInset,
	SidebarMenu,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarProvider,
	SidebarRail,
	SidebarTrigger,
} from "@/components/ui/sidebar";
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
	ChevronDown,
	FolderOpen,
	Folder,
} from "lucide-react";

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const runtime = useLocalRuntime(puxChatAdapter);
	return (
		<AssistantRuntimeProvider runtime={runtime}>
			{children}
		</AssistantRuntimeProvider>
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

function ProjectGroup({
	projectKey,
	project,
	conversations,
	isActive,
	onSelect,
}: {
	projectKey: string;
	project: { name: string; path: string; description?: string; hasManifest?: boolean } | undefined;
	conversations: Conversation[];
	isActive: boolean;
	onSelect: (project: string) => void;
}) {
	const [expanded, setExpanded] = useState(true);
	const displayName = projectKey.split("/").pop() || projectKey;

	return (
		<SidebarGroup>
			<button
				onClick={() => {
					onSelect(projectKey);
					setExpanded(!expanded);
				}}
				className={cn(
					"flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-xs transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
					isActive && "bg-sidebar-accent font-medium text-sidebar-accent-foreground",
				)}
			>
				{expanded ? (
					<ChevronDown size={12} className="shrink-0" />
				) : (
					<ChevronRight size={12} className="shrink-0" />
				)}
				{isActive ? (
					<FolderOpen size={13} className="shrink-0 text-yellow-500" />
				) : (
					<Folder size={13} className="shrink-0 text-yellow-500" />
				)}
				<span className="truncate">{displayName}</span>
				{project?.hasManifest && (
					<span className="ml-auto shrink-0 rounded bg-sidebar-primary/20 px-1 text-[9px] text-sidebar-primary">org</span>
				)}
			</button>
			{expanded && conversations.length > 0 && (
				<SidebarGroupContent>
					<div className="flex flex-col gap-0.5 px-1 pt-0.5">
						{conversations.map((c) => (
							<button
								key={`${c.project}-${c.agentId}`}
								onClick={() => onSelect(c.project)}
								className={cn(
									"flex w-full flex-col items-start gap-0 rounded-md px-2 py-1 text-left text-[12px] transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
									isActive && "text-sidebar-accent-foreground",
								)}
							>
								<span className="w-full truncate">
									{c.title || c.lastMessage || "Untitled"}
								</span>
								<span className="text-[10px] text-muted-foreground">
									{relativeTime(c.lastAt)}
									{c.messageCount > 0 && ` · ${c.messageCount} msgs`}
								</span>
							</button>
						))}
					</div>
				</SidebarGroupContent>
			)}
		</SidebarGroup>
	);
}

function AppSidebar() {
	const conversations = usePuxStore((s) => s.conversations);
	const projects = usePuxStore((s) => s.projects);
	const activeProject = usePuxStore((s) => s.activeProject);
	const setProject = usePuxStore((s) => s.setProject);

	// Group conversations by project
	const convsByProject = new Map<string, Conversation[]>();
	for (const c of conversations) {
		const existing = convsByProject.get(c.project) || [];
		existing.push(c);
		convsByProject.set(c.project, existing);
	}

	// All project keys: known projects first, then any projects only in conversations
	const knownNames = new Set(projects.map((p) => p.name));
	const allProjectKeys = [
		...projects.map((p) => p.name),
		...Array.from(convsByProject.keys()).filter((k) => !knownNames.has(k)),
	];

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
				{allProjectKeys.length === 0 ? (
					<SidebarGroup>
						<SidebarGroupContent>
							<div className="px-2 py-3 text-center text-xs text-muted-foreground">
								No projects yet
							</div>
						</SidebarGroupContent>
					</SidebarGroup>
				) : (
					allProjectKeys.map((projectKey) => (
						<ProjectGroup
							key={projectKey}
							projectKey={projectKey}
							project={projects.find((p) => p.name === projectKey)}
							conversations={convsByProject.get(projectKey) || []}
							isActive={activeProject === projectKey}
							onSelect={setProject}
						/>
					))
				)}
			</SidebarContent>
			<SidebarFooter>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton size="sm" tooltip="Settings">
							<span className="text-xs text-muted-foreground">v0.1</span>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarFooter>
			<SidebarRail />
		</Sidebar>
	);
}

function Workbench() {
	const storeTab = usePuxStore((s) => s.activeWorkbenchTab);
	const setStoreTab = usePuxStore((s) => s.setWorkbenchTab);
	const tab = storeTab;

	const tabs: { id: WorkbenchTab; icon: React.ReactNode; label: string }[] = [
		{ id: "vnc", icon: <Monitor size={14} />, label: "Sandbox" },
		{ id: "editor", icon: <Code2 size={14} />, label: "Editor" },
		{ id: "scheduler", icon: <Calendar size={14} />, label: "Scheduler" },
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
							tab === t.id
								? "bg-accent text-accent-foreground"
								: "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground",
						)}
						aria-label={t.label}
					>
						{t.icon}
					</button>
				))}
				<span className="ml-2 text-sm font-semibold text-foreground">
					{tabs.find((t) => t.id === tab)?.label}
				</span>
			</div>
			<div className="flex-1 overflow-hidden">
				{tab === "vnc" && <VNCViewer />}
				{tab === "editor" && <EditorPanel />}
				{tab === "scheduler" && <SchedulerPanel />}
			</div>
		</div>
	);
}

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);
	const loadConversations = usePuxStore((s) => s.loadConversations);
	const loadProjects = usePuxStore((s) => s.loadProjects);
	const [workbenchVisible, setWorkbenchVisible] = useState(true);

	const toggleWorkbench = useCallback(() => {
		setWorkbenchVisible((prev) => !prev);
	}, []);

	useEffect(() => {
		loadModels();
		loadProjects();
		loadConversations();
	}, [loadModels, loadConversations, loadProjects]);

	return (
		<PuxRuntimeProvider>
			<SidebarProvider className="h-svh overflow-hidden" defaultOpen={true}>
				<AppSidebar />
				<SidebarInset className="flex h-svh flex-col overflow-hidden">
					<header className="flex h-10 shrink-0 items-center gap-2 border-b border-border px-4">
						<SidebarTrigger />
						<Button
							variant="ghost"
							size="icon"
							className="ml-auto h-7 w-7"
							onClick={toggleWorkbench}
							aria-label={workbenchVisible ? "Close workbench" : "Open workbench"}
						>
							{workbenchVisible ? (
								<PanelRightClose className="size-4" />
							) : (
								<PanelRightOpen className="size-4" />
							)}
						</Button>
					</header>
					{workbenchVisible ? (
						<Group orientation="horizontal" className="flex-1 overflow-hidden">
							<Panel defaultSize={55} minSize={30}>
								<Thread />
							</Panel>
							<Separator className="w-px bg-border hover:bg-ring/50 active:bg-ring transition-colors cursor-col-resize" />
							<Panel defaultSize={45} minSize={15}>
								<Workbench />
							</Panel>
						</Group>
					) : (
						<Thread />
					)}
				</SidebarInset>
			</SidebarProvider>
		</PuxRuntimeProvider>
	);
}
