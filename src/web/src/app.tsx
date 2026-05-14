import { useEffect } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
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
import { Zap } from "lucide-react";

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

function WorkbenchSidebar() {
	return (
		<Sidebar side="right" collapsible="icon">
			<SidebarHeader>
				<SidebarMenu>
					<SidebarMenuItem>
						<SidebarMenuButton size="lg" tooltip="Workbench">
							<span className="font-semibold">Workbench</span>
						</SidebarMenuButton>
					</SidebarMenuItem>
				</SidebarMenu>
			</SidebarHeader>
			<SidebarContent />
			<SidebarRail />
		</Sidebar>
	);
}

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);

	useEffect(() => {
		loadModels();
	}, [loadModels]);

	return (
		<PuxRuntimeProvider>
			<SidebarProvider>
				<AppSidebar />
				<SidebarInset>
					<header className="flex h-10 items-center gap-2 border-b border-border px-4">
						<SidebarTrigger />
					</header>
					<Thread />
				</SidebarInset>
				<WorkbenchSidebar />
			</SidebarProvider>
		</PuxRuntimeProvider>
	);
}
