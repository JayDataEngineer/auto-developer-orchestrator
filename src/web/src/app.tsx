import { useEffect, useState, useCallback } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
import { Panel, Group, Separator } from "react-resizable-panels";
import { usePuxStore } from "@/lib/pux-store";
import { puxChatAdapter } from "@/lib/pux-chat-adapter";
import { Thread } from "@/components/assistant-ui/thread";
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
import { PanelRightOpen, PanelRightClose, Zap } from "lucide-react";

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const runtime = useLocalRuntime(puxChatAdapter);
	return (
		<AssistantRuntimeProvider runtime={runtime}>
			{children}
		</AssistantRuntimeProvider>
	);
}

function AppSidebar() {
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
					<SidebarGroupContent />
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

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);
	const [workbenchVisible, setWorkbenchVisible] = useState(true);

	const toggleWorkbench = useCallback(() => {
		setWorkbenchVisible((prev) => !prev);
	}, []);

	useEffect(() => {
		loadModels();
	}, [loadModels]);

	return (
		<PuxRuntimeProvider>
			<SidebarProvider>
				<AppSidebar />
				<SidebarInset>
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
						<Group orientation="horizontal" className="flex-1">
							<Panel defaultSize={55} minSize={30}>
								<Thread />
							</Panel>
							<Separator className="w-px bg-border hover:bg-ring/50 active:bg-ring transition-colors cursor-col-resize" />
							<Panel defaultSize={45} minSize={20} maxSize={65} collapsible>
								<div className="flex h-full flex-col bg-background">
									<div className="flex h-9 items-center border-b border-border px-3">
										<span className="text-sm font-semibold text-foreground">Workbench</span>
									</div>
									<div className="flex-1" />
								</div>
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
