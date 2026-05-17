"use client";

import { makeAssistantToolUI, MessagePartPrimitive } from "@assistant-ui/react";
import { usePuxStore } from "@/lib/pux-store";
import { Bot, CheckCircle, Loader2, XCircle } from "lucide-react";

function DelegateRenderer({
	args,
	status,
}: {
	args: Record<string, any>;
	result?: any;
	status?: { type: string };
}) {
	const agentName = (args.agent_id as string) || (args.agent as string) || "agent";
	const task = (args.task as string) || (args.prompt as string) || "";
	const isRunning = status?.type === "running";
	const isComplete = status?.type === "complete";
	const isError = status?.type === "error";

	const agents = usePuxStore((s) => s.agents);
	const agentState = [...agents.values()].find(
		(a) => a.agentName === agentName && a.task === task
	);
	const subToolCount = agentState?.toolCalls.length ?? 0;

	return (
		<div className="my-2 rounded-lg border border-border">
			<div className="flex items-center gap-2 px-4 py-3 text-sm border-b border-border">
				{isRunning ? (
					<Loader2 size={14} className="animate-spin text-blue-500" />
				) : isError ? (
					<XCircle size={14} className="text-red-500" />
				) : (
					<CheckCircle size={14} className="text-green-500" />
				)}
				<Bot size={14} className="text-muted-foreground" />
				<span className="font-medium">{agentName}</span>
				{task && (
					<span className="text-muted-foreground truncate max-w-[400px]">
						{task}
					</span>
				)}
				<span className="text-xs text-muted-foreground ml-auto">
					{isRunning ? "working..." : isComplete ? "done" : isError ? "failed" : ""}
				</span>
				{subToolCount > 0 && (
					<span className="text-xs text-muted-foreground">
						· {subToolCount} tool{subToolCount !== 1 ? "s" : ""}
					</span>
				)}
			</div>
			{isComplete && (
				<div className="px-4 py-3">
					<MessagePartPrimitive.Messages />
				</div>
			)}
		</div>
	);
}

export const DelegateToolUI = makeAssistantToolUI({
	toolName: "delegate_to",
	render: DelegateRenderer,
});

export const DelegateAsyncToolUI = makeAssistantToolUI({
	toolName: "delegate_async",
	render: DelegateRenderer,
});
