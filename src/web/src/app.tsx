import { useEffect, useState, useCallback, useMemo, useRef, useDeferredValue, memo } from "react";
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
import { cn } from "@/lib/utils";
import { webChatAdapter } from "@/lib/pux-chat-adapter";
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
	SidebarMenuAction,
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
import { Sheet, SheetContent } from "@/components/ui/sheet";
import { Panel, Group, Separator, usePanelRef } from "react-resizable-panels";
import {
	PanelRight,
	PanelLeftOpen,
	PanelLeftClose,
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
	WifiOff,
	AlertTriangle,
	Pencil,
	Menu,
	Search,
} from "lucide-react";
import { useIsMobile } from "@/hooks/use-mobile";

// ── Runtime Provider ──
// NOT re-keyed — uses runtime.thread.reset() to switch conversations.
// Placed inside SidebarInset so sidebar state survives conversation switches.

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const historyAdapter = useMemo(() => createPuxHistoryAdapter(), []);
	const runtime = useLocalRuntime(webChatAdapter, {
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

function useBackendHealth(intervalMs = 10000) {
	const [online, setOnline] = useState(true);
	useEffect(() => {
		let alive = true;
		const check = async () => {
			try {
				const fetch = getFetch();
				const resp = await fetch(apiUrl("/api/pux/models"), {
					method: "GET",
					signal: AbortSignal.timeout(3000),
				});
				if (alive) setOnline(resp.ok);
			} catch {
				if (alive) setOnline(false);
			}
		};
		check();
		const id = setInterval(check, intervalMs);
		return () => {
			alive = false;
			clearInterval(id);
		};
	}, [intervalMs]);
	return online;
}

function BackendOfflineBanner() {
	const online = useBackendHealth();
	if (online) return null;
	return (
		<div className="flex h-7 shrink-0 items-center gap-2 bg-red-600/90 px-4 text-xs font-medium text-white">
			<WifiOff className="size-3.5" />
			<span>Backend offline — start with </span>
			<code className="rounded bg-white/20 px-1.5 py-0.5 text-[11px]">task dev</code>
		</div>
	);
}

function ExtensionFailureToast() {
	const [toast, setToast] = useState<{ name: string; error: string } | null>(null);

	useEffect(() => {
		let alive = true;
		(async () => {
			try {
				const resp = await getFetch("/api/extensions");
				if (!alive) return;
				const results: { name: string; success: boolean; error?: string }[] = await resp.json();
				const failed = results.find((r) => !r.success);
				if (failed) {
					setToast({ name: failed.name, error: failed.error || "unknown error" });
					setTimeout(() => { if (alive) setToast(null); }, 10000);
				}
			} catch {
				// backend not up yet
			}
		})();
		return () => { alive = false; };
	}, []);

	if (!toast) return null;

	return (
		<div className="flex h-7 shrink-0 items-center gap-2 bg-amber-600/90 px-4 text-xs font-medium text-white">
			<AlertTriangle className="size-3.5 shrink-0" />
			<span className="truncate">
				Extension <span className="font-semibold">{toast.name}</span> failed: {toast.error}
			</span>
			<button onClick={() => setToast(null)} className="ml-auto shrink-0 rounded p-0.5 hover:bg-white/20">
				<XIcon className="size-3" />
			</button>
		</div>
	);
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

// ── Conversation item with inline rename ──

function ConversationItem({
	conversation: c,
	isProcessing,
	isUnread,
	onSelect,
	onDelete,
	onRename,
}: {
	conversation: Conversation;
	isProcessing: boolean;
	isUnread: boolean;
	onSelect: () => void;
	onDelete: () => void;
	onRename: (title: string) => void;
}) {
	const [renaming, setRenaming] = useState(false);
	const [draft, setDraft] = useState("");
	const inputRef = useRef<HTMLInputElement>(null);

	const startRename = (e: React.MouseEvent) => {
		e.stopPropagation();
		setDraft(c.title || c.lastMessage || "");
		setRenaming(true);
		setTimeout(() => inputRef.current?.focus(), 0);
	};

	const submitRename = () => {
		const trimmed = draft.trim();
		if (trimmed) onRename(trimmed);
		setRenaming(false);
	};

	return (
		<SidebarMenuSubItem className="group/sub">
			{renaming ? (
				<div className="flex items-center gap-1 px-2 py-1">
					<input
						ref={inputRef}
						value={draft}
						onChange={(e) => setDraft(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Enter") submitRename();
							if (e.key === "Escape") setRenaming(false);
						}}
						onBlur={submitRename}
						className="h-5 w-full rounded bg-background px-1.5 text-[12px] outline-none ring-1 ring-ring"
					/>
				</div>
			) : (
				<SidebarMenuSubButton
					onClick={onSelect}
					className="!cursor-default"
				>
					<div className="flex min-w-0 flex-1 items-center gap-1.5">
						{isUnread && !isProcessing && (
							<span className="inline-flex h-2 w-2 shrink-0 rounded-full bg-white" />
						)}
						{isProcessing && (
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
						onClick={startRename}
						className="shrink-0 rounded p-0.5 opacity-0 hover:bg-muted group-hover/sub:opacity-100"
						title="Rename"
					>
						<Pencil className="size-3" />
					</button>
					<button
						onClick={(e) => {
							e.stopPropagation();
							onDelete();
						}}
						className="shrink-0 rounded p-0.5 opacity-0 hover:bg-destructive/10 hover:text-destructive group-hover/sub:opacity-100"
						title="Delete chat"
					>
						<Trash2 className="size-3" />
					</button>
				</SidebarMenuSubButton>
			)}
		</SidebarMenuSubItem>
	);
}

// ── Project Group (collapsible) ──

const ProjectGroup = memo(function ProjectGroup({
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
	const renameConversation = usePuxStore((s) => s.renameConversation);
	const removeProject = usePuxStore((s) => s.removeProject);
	const runningAgents = usePuxStore((s) => s.runningAgents);

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
				<SidebarMenuAction
					showOnHover
					onClick={(e) => {
						e.stopPropagation();
						removeProject(projectKey);
					}}
					title="Remove from sidebar"
				>
					<XIcon className="size-3" />
				</SidebarMenuAction>
				{conversations.length > 0 && (
					<CollapsibleContent className="group-data-[collapsible=icon]:hidden">
						<SidebarMenuSub>
							{conversations.map((c) => {
								const convKey = `${c.project}:${c.agentId}`;
								const status = c.status || "";
								const isProcessing = status === "processing" || runningAgents.has(convKey);
								const isUnread = status === "unread";
								return (
									<ConversationItem
										key={`${c.project}-${c.agentId}`}
										conversation={c}
										isProcessing={isProcessing}
										isUnread={isUnread}
										onSelect={() => onSelectConversation(c.project, c.agentId)}
										onDelete={() => deleteConversation(c.project, c.agentId)}
										onRename={(title) => renameConversation(c.project, c.agentId, title)}
									/>
								);
							})}
						</SidebarMenuSub>
					</CollapsibleContent>
				)}
			</SidebarMenuItem>
		</Collapsible>
	);
});

// ── Sidebar ──

function latestAt(convs: Conversation[] | undefined): number {
	if (!convs || convs.length === 0) return 0;
	return Math.max(...convs.map((c) => new Date(c.lastAt).getTime() || 0));
}

function AppSidebar() {
	const conversations = usePuxStore((s) => s.conversations);
	const projects = usePuxStore((s) => s.projects);
	const activeProject = usePuxStore((s) => s.activeProject);
	const setConversation = usePuxStore((s) => s.setConversation);
	const [showAddProject, setShowAddProject] = useState(false);
	const [search, setSearch] = useState("");
	const deferredSearch = useDeferredValue(search);

	// Group conversations by project, sorted newest-first within each group
	const convsByProject = useMemo(() => {
		const map = new Map<string, Conversation[]>();
		for (const c of conversations) {
			const existing = map.get(c.project) || [];
			existing.push(c);
			map.set(c.project, existing);
		}
		for (const [key, arr] of map) {
			arr.sort((a, b) => new Date(b.lastAt).getTime() - new Date(a.lastAt).getTime());
			map.set(key, arr);
		}
		return map;
	}, [conversations]);

	// All project keys sorted by most recent conversation (last used first)
	const allProjectKeys = useMemo(() => {
		const all = new Set([
			...projects.map((p) => p.name),
			...convsByProject.keys(),
		]);
		return Array.from(all).sort((a, b) => {
			const aLatest = latestAt(convsByProject.get(a));
			const bLatest = latestAt(convsByProject.get(b));
			return bLatest - aLatest;
		});
	}, [projects, convsByProject]);

	// Filter by search term (matches project names + conversation titles)
	const filteredKeys = useMemo(() => {
		if (!deferredSearch.trim()) return allProjectKeys;
		const q = deferredSearch.toLowerCase();
		return allProjectKeys.filter((key) => {
			if (key.toLowerCase().includes(q)) return true;
			const convs = convsByProject.get(key) || [];
			return convs.some((c) =>
				(c.title || c.lastMessage || "").toLowerCase().includes(q)
			);
		});
	}, [allProjectKeys, convsByProject, deferredSearch]);

	return (
		<Sidebar collapsible="icon">
			<SidebarHeader>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton size="lg" tooltip="Pux">
							<div className="flex aspect-square size-8 items-center justify-center rounded-lg">
								<svg viewBox="0 0 122.88 93.04" className="size-5" fill="#5BA0E8"><path d="M7.09,43.87h7.11v-1.65c0-2.2,0.44-4.3,1.24-6.22c0.83-1.99,2.04-3.79,3.55-5.29c1.5-1.5,3.3-2.72,5.29-3.55 c1.92-0.8,4.02-1.24,6.22-1.24h28.29l0-0.14V15.65c-0.46-0.17-0.9-0.38-1.32-0.62c-0.59-0.35-1.13-0.77-1.61-1.25 c-0.74-0.74-1.34-1.63-1.75-2.62c-0.39-0.95-0.61-2-0.61-3.08c0-1.09,0.22-2.13,0.61-3.08c0.41-0.99,1.01-1.88,1.75-2.62 c0.74-0.74,1.63-1.34,2.62-1.75c0.95-0.4,2-0.61,3.08-0.61s2.13,0.22,3.08,0.61c0.99,0.41,1.88,1.01,2.62,1.75 c0.74,0.74,1.34,1.63,1.75,2.62c0.39,0.95,0.61,2,0.61,3.08c0,1.09-0.22,2.13-0.61,3.08c-0.41,0.99-1.01,1.88-1.75,2.62l-0.04,0.04 c-0.47,0.46-1,0.87-1.57,1.21c-0.42,0.25-0.86,0.46-1.32,0.62v10.13l0,0.14h28.29c2.2,0,4.3,0.44,6.22,1.24 c2,0.83,3.79,2.04,5.29,3.55c1.5,1.5,2.72,3.3,3.55,5.29c0.8,1.92,1.24,4.02,1.24,6.22v1.65h6.86c0.95,0,1.87,0.19,2.71,0.54 c0.87,0.36,1.65,0.89,2.3,1.54l0.04,0.04c0.64,0.65,1.15,1.41,1.5,2.26c0.35,0.84,0.54,1.75,0.54,2.71v18.92 c0,0.95-0.19,1.87-0.54,2.71c-0.36,0.86-0.89,1.65-1.54,2.3l0,0c-1.28,1.28-3.06,2.08-5.01,2.08h-6.87 c-0.03,2.11-0.47,4.14-1.24,5.99c-0.83,2-2.04,3.79-3.55,5.29c-1.5,1.5-3.3,2.72-5.29,3.55c-1.92,0.8-4.02,1.24-6.22,1.24H30.5 c-2.2,0-4.3-0.44-6.22-1.24c-2-0.83-3.79-2.04-5.29-3.55c-1.5-1.5-2.72-3.3-3.55-5.29c-0.77-1.85-1.21-3.88-1.24-5.99H7.09 c-0.95,0-1.87-0.19-2.71-0.54c-0.87-0.36-1.65-0.89-2.3-1.54l-0.04-0.04c-0.64-0.65-1.15-1.41-1.5-2.26C0.19,71.75,0,70.83,0,69.88 V50.96c0-0.95,0.19-1.87,0.54-2.71c0.36-0.87,0.89-1.65,1.54-2.3l0,0c0.65-0.65,1.43-1.18,2.3-1.54 C5.22,44.06,6.13,43.87,7.09,43.87L7.09,43.87z M47,74.3c-0.14-0.11-0.26-0.23-0.37-0.37c-0.33-0.4-0.5-0.86-0.51-1.33 c-0.01-0.47,0.14-0.94,0.45-1.35c0.11-0.14,0.23-0.27,0.38-0.39c0.52-0.43,1.21-0.66,1.89-0.67c0.68-0.01,1.36,0.19,1.9,0.6 c1.86,1.43,3.7,2.47,5.52,3.16c1.8,0.68,3.58,1,5.34,0.98c1.77-0.02,3.56-0.39,5.39-1.08c1.85-0.7,3.71-1.75,5.6-3.1 c0.56-0.4,1.25-0.58,1.93-0.55c0.68,0.03,1.36,0.27,1.87,0.72c0.13,0.12,0.25,0.25,0.36,0.4c0.3,0.42,0.44,0.9,0.41,1.37 c-0.03,0.47-0.22,0.93-0.56,1.32c-0.12,0.13-0.26,0.26-0.42,0.37c-2.37,1.71-4.75,3.01-7.16,3.9c-2.42,0.89-4.87,1.36-7.35,1.39 c-2.49,0.03-4.95-0.39-7.4-1.28c-2.43-0.88-4.85-2.23-7.23-4.06L47,74.3L47,74.3z"/></svg>
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
				{/* Search */}
				<div className="px-2 pb-1">
					<div className="relative">
						<Search className="absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
						<input
							type="text"
							placeholder="Search..."
							value={search}
							onChange={(e) => setSearch(e.target.value)}
							className="h-7 w-full rounded-md bg-sidebar-accent/50 pl-7 pr-2 text-xs outline-none placeholder:text-muted-foreground/60 focus:ring-1 focus:ring-sidebar-ring"
						/>
					</div>
				</div>
				<SidebarGroup>
					<SidebarMenu>
						{filteredKeys.length === 0 ? (
							<div className="px-2 py-3 text-center text-xs text-muted-foreground">
								{allProjectKeys.length === 0 ? "No projects yet" : "No matches"}
							</div>
						) : (
							filteredKeys.map((projectKey) => (
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
	const isMobile = useIsMobile();

	return (
		<Tabs
			value={storeTab}
			onValueChange={(v) => setStoreTab(v as WorkbenchTab)}
			className="flex h-full flex-col bg-sidebar"
		>
			<TabsList className={cn(
				"shrink-0 w-full rounded-none border-b border-border bg-transparent px-2",
				isMobile ? "h-12" : "h-9",
			)}>
				<TabsTrigger value="vnc" className={cn("gap-1.5 grow shrink-0", isMobile ? "text-sm" : "text-xs")}>
					<Monitor className="size-4" />
					Sandbox
				</TabsTrigger>
				<TabsTrigger value="editor" className={cn("gap-1.5 grow shrink-0", isMobile ? "text-sm" : "text-xs")}>
					<Code2 className="size-4" />
					Editor
				</TabsTrigger>
				<TabsTrigger value="scheduler" className={cn("gap-1.5 grow shrink-0", isMobile ? "text-sm" : "text-xs")}>
					<Calendar className="size-4" />
					Scheduler
				</TabsTrigger>
				<TabsTrigger value="workers" className={cn("gap-1.5 grow shrink-0", isMobile ? "text-sm" : "text-xs")}>
					<Users className="size-4" />
					Agents
				</TabsTrigger>
				<TabsTrigger value="settings" className={cn("gap-1.5 grow shrink-0", isMobile ? "text-sm" : "text-xs")}>
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
	const fontScale = usePuxStore((s) => s.fontScale);
	const isMobile = useIsMobile();
	const [workbenchVisible, setWorkbenchVisible] = useState(false);
	const [mobileWorkbenchOpen, setMobileWorkbenchOpen] = useState(false);
	const workbenchPanelRef = usePanelRef();

	// Apply theme on mount
	useEffect(() => {
		document.documentElement.setAttribute("data-pux-theme", theme);
	}, [theme]);

	// Apply font scale
	useEffect(() => {
		document.documentElement.style.fontSize = `${fontScale * 100}%`;
	}, [fontScale]);

	// Poll for running agent status
	useAgentStatusPolling();
	const [showTerminal, setShowTerminal] = useState(false);

	const toggleWorkbench = useCallback(() => {
		if (isMobile) {
			setMobileWorkbenchOpen((v) => !v);
			return;
		}
		const panel = workbenchPanelRef.current;
		if (!panel) return;
		if (panel.isCollapsed()) {
			panel.resize("35%");
		} else {
			panel.collapse();
		}
	}, [isMobile]);

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
			defaultOpen={!isMobile}
		>
			<AppSidebar />

			{isMobile ? (
				/* ── Mobile layout: full-width chat + sheet workbench ── */
				<div className="flex h-svh flex-col">
					{/* Navbar */}
					<header className="flex h-12 shrink-0 items-center gap-2 border-b border-sidebar-border bg-sidebar px-3">
						<SidebarToggle />
						<Button
							variant="ghost"
							size="icon"
							className="ml-1 h-9 w-9"
							onClick={() => setShowTerminal((v) => !v)}
							aria-label="Toggle terminal"
						>
							<TerminalIcon className="size-5" />
						</Button>

						<Button
							variant="ghost"
							size="icon"
							className="ml-auto h-9 w-9"
							onClick={() => setMobileWorkbenchOpen(true)}
							aria-label="Open workbench"
						>
							<PanelRight className="size-5" />
						</Button>
					</header>
					<BackendOfflineBanner />
					<ExtensionFailureToast />
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

					{/* Workbench as bottom sheet on mobile */}
					<Sheet open={mobileWorkbenchOpen} onOpenChange={setMobileWorkbenchOpen}>
						<SheetContent
							side="bottom"
							className="h-[85vh] rounded-t-xl p-0 bg-sidebar text-sidebar-foreground"
						>
							<Workbench />
						</SheetContent>
					</Sheet>
				</div>
			) : (
				/* ── Desktop layout: resizable panels ── */
				<Group orientation="horizontal" className="h-svh" onLayoutChanged={() => usePuxStore.getState().bumpWorkbenchLayout()}>
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
							<BackendOfflineBanner />
							<ExtensionFailureToast />
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

					<Separator className="relative w-3 cursor-col-resize before:content-[''] before:absolute before:inset-y-0 before:left-1/2 before:-translate-x-1/2 before:w-px before:bg-border before:transition-colors hover:before:bg-ring/50" />
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
			)}
		</SidebarProvider>
	);
}
