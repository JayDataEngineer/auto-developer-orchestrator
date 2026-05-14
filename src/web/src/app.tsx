import { useEffect } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
import { Panel, Group, Separator } from "react-resizable-panels";
import { usePuxStore } from "@/lib/pux-store";
import { puxChatAdapter } from "@/lib/pux-chat-adapter";
import { Thread } from "@/components/assistant-ui/thread";

function PuxRuntimeProvider({ children }: { children: React.ReactNode }) {
	const runtime = useLocalRuntime(puxChatAdapter);
	return (
		<AssistantRuntimeProvider runtime={runtime}>
			{children}
		</AssistantRuntimeProvider>
	);
}

function ResizeHandle() {
	return (
		<Separator
			className="w-px bg-border hover:bg-ring/50 active:bg-ring transition-colors"
		/>
	);
}

function Sidebar() {
	return (
		<div className="flex h-full flex-col bg-background">
			<div className="flex h-10 items-center gap-2 border-b border-border px-3">
				<span className="text-sm font-semibold text-foreground">Pux</span>
			</div>
			<div className="flex-1" />
			<div className="border-t border-border px-3 py-2 text-[10px] text-muted-foreground">
				History
			</div>
		</div>
	);
}

function Workbench() {
	return (
		<div className="flex h-full flex-col bg-background">
			<div className="flex h-10 items-center gap-2 border-b border-border px-3">
				<span className="text-sm font-semibold text-foreground">Workbench</span>
			</div>
			<div className="flex-1" />
		</div>
	);
}

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);

	useEffect(() => {
		loadModels();
	}, [loadModels]);

	return (
		<PuxRuntimeProvider>
			<Group orientation="horizontal" className="h-screen">
				<Panel defaultSize={15} minSize={10} maxSize={25} collapsible>
					<Sidebar />
				</Panel>
				<ResizeHandle />
				<Panel defaultSize={45} minSize={30}>
					<Thread />
				</Panel>
				<ResizeHandle />
				<Panel defaultSize={40} minSize={20} collapsible>
					<Workbench />
				</Panel>
			</Group>
		</PuxRuntimeProvider>
	);
}
