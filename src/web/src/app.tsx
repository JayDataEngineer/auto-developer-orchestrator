import { useEffect, useState, useCallback } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
import { Panel, Group, Separator } from "react-resizable-panels";
import { usePuxStore } from "@/lib/pux-store";
import { puxChatAdapter } from "@/lib/pux-chat-adapter";
import { Thread } from "@/components/assistant-ui/thread";
import { VNCViewer } from "@/components/workbench/vnc-viewer";
import { EditorPanel } from "@/components/workbench/editor-panel";
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
	MessageSquare,
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

function AppSidebar() {
	const conversations = usePuxStore((s) => s.conversations);
	const activeProject = usePuxStore((s) => s.activeProject);
	const setProject = usePuxStore((s) => s.setProject);

	return (
		<Sidebar collapsible="icon">
			<SidebarHeader>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton size="lg" tooltip="Pux">
							<div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
								<Zap className="size-4" />
							</div>
							<div className="flex flex-col gap-0.5 leading-none">
								<span className="font-semibold">Pux</span>
							</div>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarHeader>
			<SidebarContent>
				<SidebarGroup>
					<SidebarGroupLabel>History</SidebarGroupLabel>
					<SidebarGroupContent>
						<SidebarMenu>
							{conversations.length === 0 && (
								<div className="px-2 py-4 text-center text-xs text-muted-foreground">
									No conversations yet
								</div>
							)}
							{conversations.map((c) => (
								<SidebarMenuItem key={`${c.project}-${c.agentId}`}>
									<SidebarMenuButton
										tooltip={c.title || c.lastMessage}
										isActive={activeProject === c.project}
										onClick={() => setProject(c.project)}
										className="flex-col items-start gap-0.5"
									>
										<span className="truncate text-sm">
											{c.title || c.lastMessage || "Untitled"}
										</span>
										<span className="text-[11px] text-muted-foreground">
											{relativeTime(c.lastAt)}
											{c.messageCount > 0 && ` · ${c.messageCount} msgs`}
										</span>
									</SidebarMenuButton>
								</SidebarMenuItem>
							))}
						</SidebarMenu>
					</SidebarGroupContent>
				</SidebarGroup>
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

type WorkbenchTab = "vnc" | "editor";

function Workbench() {
	const [tab, setTab] = useState<WorkbenchTab>("vnc");

	const tabButton = (id: WorkbenchTab, icon: React.ReactNode, label: string) => (
		<button
			onClick={() => setTab(id)}
			className={cn(
				"inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors",
				tab === id
					? "bg-accent text-accent-foreground"
					: "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground",
			)}
			aria-label={label}
		>
			{icon}
		</button>
	);

	return (
		<div className="flex h-full flex-col bg-background">
			<div className="flex h-9 items-center gap-0.5 border-b border-border px-1">
				{tabButton("vnc", <Monitor size={14} />, "Sandbox")}
				{tabButton("editor", <Code2 size={14} />, "Editor")}
				<span className="ml-2 text-sm font-semibold text-foreground">
					{tab === "vnc" ? "Sandbox" : "Editor"}
				</span>
			</div>
			<div className="flex-1 overflow-hidden">
				{tab === "vnc" ? <VNCViewer /> : <EditorPanel />}
			</div>
		</div>
	);
}

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);
	const loadConversations = usePuxStore((s) => s.loadConversations);
	const [workbenchVisible, setWorkbenchVisible] = useState(true);

	const toggleWorkbench = useCallback(() => {
		setWorkbenchVisible((prev) => !prev);
	}, []);

	useEffect(() => {
		loadModels();
		loadConversations();
	}, [loadModels, loadConversations]);

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
