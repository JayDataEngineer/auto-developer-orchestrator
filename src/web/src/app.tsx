import { useEffect } from "react";
import {
	useLocalRuntime,
	AssistantRuntimeProvider,
} from "@assistant-ui/react";
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

export function App() {
	const loadModels = usePuxStore((s) => s.loadModels);

	useEffect(() => {
		loadModels();
	}, [loadModels]);

	return (
		<PuxRuntimeProvider>
			<Thread />
		</PuxRuntimeProvider>
	);
}
