import { useState } from "react";
import { ClipboardList } from "lucide-react";
import { usePuxStore } from "@/lib/pux-store";

export function DecisionPlanReview() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const [refineMode, setRefineMode] = useState(false);
	const [feedback, setFeedback] = useState("");

	if (!pending) return null;

	const handleRefine = () => {
		if (feedback.trim()) {
			respond("refine", feedback.trim());
			setFeedback("");
			setRefineMode(false);
		}
	};

	return (
		<div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
			<div className="w-[520px] max-w-[90vw] rounded-xl border border-border bg-surface shadow-2xl">
				<div className="flex items-center gap-2 border-b border-border px-4 py-3">
					<ClipboardList size={14} className="text-accent" />
					<span className="text-xs font-semibold uppercase tracking-wider text-dim">
						Plan Review
					</span>
				</div>
				{pending.title && (
					<div className="px-4 py-3 text-sm font-medium text-text">
						{pending.title}
					</div>
				)}
				{pending.description && (
					<div className="max-h-[400px] overflow-y-auto px-4 pb-3 text-sm text-muted-foreground whitespace-pre-wrap">
						{pending.description}
					</div>
				)}

				{refineMode && (
					<div className="border-t border-border px-4 py-3 space-y-2">
						<textarea
							value={feedback}
							onChange={(e) => setFeedback(e.target.value)}
							placeholder="What should be changed..."
							rows={3}
							className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm text-text outline-none focus:border-accent resize-none"
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
								className="rounded-md border border-border px-4 py-1.5 text-sm text-text"
							>
								Cancel
							</button>
						</div>
					</div>
				)}

				{!refineMode && (
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
							className="rounded-md border border-border px-4 py-1.5 text-sm text-text"
						>
							Cancel
						</button>
					</div>
				)}
			</div>
		</div>
	);
}
