"use client";

import { useState } from "react";
import { makeAssistantToolUI } from "@assistant-ui/react";
import { usePuxStore } from "@/lib/pux-store";
import { cn } from "@/lib/utils";
import { MessageCircleQuestion, CheckCircle, CornerDownLeft } from "lucide-react";

function AskUserRenderer({
	args,
	status,
}: {
	args: Record<string, any>;
	result?: any;
	artifact?: any;
	status?: { type: string };
}) {
	const question = (args.question as string) || "";
	const options = args.options as string[] | undefined;
	const allowFreeText = args.allowFreeText !== false;
	const respond = usePuxStore((s) => s.respondToDecision);
	const pending = usePuxStore((s) => s.pendingDecision);
	const [input, setInput] = useState("");

	const isComplete = status?.type === "complete";
	const answered = isComplete && pending === null;

	const handleSubmit = (value: string) => {
		const decisionId = args.decisionId as string || pending?.decisionId || "";
		respond("answer", value);
		setInput("");
	};

	if (answered) {
		return (
			<div className="my-2 rounded-lg border border-border py-3">
				<div className="flex items-center gap-2 px-4 text-sm">
					<CheckCircle size={14} className="text-muted-foreground" />
					<span className="text-muted-foreground">
						Asked: <b>ask_user</b>
					</span>
				</div>
				{question && (
					<div className="mt-2 border-t border-border px-4 pt-2 text-sm text-muted-foreground">
						{question}
					</div>
				)}
			</div>
		);
	}

	return (
		<div className="my-2 rounded-lg border border-accent/30 bg-accent/5 py-3">
			<div className="flex items-center gap-2 px-4 text-sm">
				<MessageCircleQuestion size={14} className="text-accent" />
				<span className="text-xs font-semibold uppercase tracking-wider text-dim">
					Question
				</span>
			</div>
			{question && (
				<div className="mt-2 px-4 text-sm whitespace-pre-wrap">
					{question}
				</div>
			)}

			{(options?.length ?? 0) > 0 && (
				<div className="mt-2 px-4 space-y-1.5">
					{options!.map((opt) => (
						<button
							key={opt}
							onClick={() => handleSubmit(opt)}
							className="block w-full rounded-md border border-border bg-card px-3 py-2 text-left text-sm transition-colors hover:border-accent"
						>
							{opt}
						</button>
					))}
				</div>
			)}

			{allowFreeText && (
				<div className="mt-2 border-t border-border px-4 pt-2 space-y-2">
					<div className="relative">
						<input
							value={input}
							onChange={(e) => setInput(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter" && input.trim()) {
									e.preventDefault();
									handleSubmit(input.trim());
								}
							}}
							placeholder="Type your answer..."
							className="w-full rounded-lg border border-border bg-card px-3 py-2 pr-8 text-sm outline-none focus:border-accent"
						/>
						<CornerDownLeft
							size={14}
							className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground/40"
						/>
					</div>
					<button
						onClick={() => input.trim() && handleSubmit(input.trim())}
						disabled={!input.trim()}
						className="rounded-md bg-accent px-4 py-1.5 text-sm text-white disabled:opacity-30"
					>
						Submit
					</button>
				</div>
			)}
		</div>
	);
}

export const AskUserToolUI = makeAssistantToolUI({
	toolName: "ask_user",
	render: AskUserRenderer,
});
