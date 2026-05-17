"use client";

import { useState } from "react";
import { makeAssistantToolUI } from "@assistant-ui/react";
import { usePuxStore } from "@/lib/pux-store";
import { ClipboardList, CheckCircle } from "lucide-react";

function PlanReviewRenderer({
	args,
	status,
}: {
	args: Record<string, any>;
	result?: any;
	artifact?: any;
	status?: { type: string };
}) {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const [refineMode, setRefineMode] = useState(false);
	const [feedback, setFeedback] = useState("");

	const isComplete = status?.type === "complete";
	const answered = isComplete && pending === null;

	const title = (args.title as string) || pending?.title || "";
	const description = (args.description as string) || pending?.description || "";

	const handleRefine = () => {
		if (feedback.trim()) {
			respond("refine", feedback.trim());
			setFeedback("");
			setRefineMode(false);
		}
	};

	if (answered) {
		return (
			<div className="my-2 rounded-lg border border-border py-3">
				<div className="flex items-center gap-2 px-4 text-sm">
					<CheckCircle size={14} className="text-muted-foreground" />
					<span className="text-muted-foreground">
						Plan reviewed: <b>{title || "create_plan"}</b>
					</span>
				</div>
			</div>
		);
	}

	return (
		<div className="my-2 rounded-lg border border-accent/30 bg-accent/5 py-3">
			<div className="flex items-center gap-2 px-4 text-sm">
				<ClipboardList size={14} className="text-accent" />
				<span className="text-xs font-semibold uppercase tracking-wider text-dim">
					Plan Review
				</span>
			</div>
			{title && (
				<div className="mt-2 px-4 text-sm font-medium">
					{title}
				</div>
			)}
			{description && (
				<div className="mt-1 max-h-[300px] overflow-y-auto px-4 text-sm text-muted-foreground whitespace-pre-wrap">
					{description}
				</div>
			)}

			{refineMode ? (
				<div className="border-t border-border px-4 py-3 space-y-2">
					<textarea
						value={feedback}
						onChange={(e) => setFeedback(e.target.value)}
						placeholder="What should be changed..."
						rows={3}
						className="w-full rounded-lg border border-border bg-card px-3 py-2 text-sm outline-none focus:border-accent resize-none"
					/>
					<div className="flex gap-2">
						<button
							onClick={handleRefine}
							disabled={!feedback.trim()}
							className="rounded-md bg-yellow-600 px-4 py-1.5 text-sm text-white disabled:opacity-30 hover:bg-yellow-700"
						>
							Send Feedback
						</button>
						<button
							onClick={() => { setRefineMode(false); setFeedback(""); }}
							className="rounded-md border border-border px-4 py-1.5 text-sm"
						>
							Cancel
						</button>
					</div>
				</div>
			) : (
				<div className="flex gap-2 border-t border-border px-4 py-3">
					<button
						onClick={() => respond("approve", "")}
						className="rounded-md bg-green-600 px-4 py-1.5 text-sm text-white hover:bg-green-700"
					>
						Approve
					</button>
					<button
						onClick={() => setRefineMode(true)}
						className="rounded-md border border-yellow-600 text-yellow-600 px-4 py-1.5 text-sm hover:bg-yellow-600/10"
					>
						Refine
					</button>
					<button
						onClick={() => respond("cancel", "")}
						className="rounded-md border border-border px-4 py-1.5 text-sm"
					>
						Cancel
					</button>
				</div>
			)}
		</div>
	);
}

export const PlanReviewToolUI = makeAssistantToolUI({
	toolName: "create_plan",
	render: PlanReviewRenderer,
});
