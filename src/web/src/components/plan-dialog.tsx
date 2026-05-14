import { useState } from "react";
import { ClipboardList } from "lucide-react";
import { usePuxStore } from "@/lib/pux-store";

export function PlanDialog() {
	const pending = usePuxStore((s) => s.pendingPlan);
	const respond = usePuxStore((s) => s.respondToPlan);
	const [feedback, setFeedback] = useState("");
	const [showRefine, setShowRefine] = useState(false);

	if (!pending) return null;

	const handleAction = (action: string) => {
		respond(action, action === "refine" ? feedback : undefined);
		setFeedback("");
		setShowRefine(false);
	};

	return (
		<div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
			<div className="max-h-[80vh] w-[520px] max-w-[90vw] overflow-y-auto rounded-xl border border-border bg-surface shadow-2xl">
				<div className="flex items-center gap-2 border-b border-border px-4 py-3">
					<ClipboardList size={14} className="text-accent" />
					<span className="text-xs font-semibold uppercase tracking-wider text-dim">
						Plan: {pending.name}
					</span>
				</div>
				<div className="max-h-[400px] overflow-y-auto px-4 py-3 text-sm whitespace-pre-wrap text-text">
					{pending.content}
				</div>

				{showRefine && (
					<div className="border-t border-border px-4 py-3">
						<textarea
							value={feedback}
							onChange={(e) => setFeedback(e.target.value)}
							placeholder="What should be changed?"
							rows={3}
							className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm text-text outline-none focus:border-accent resize-none"
						/>
					</div>
				)}

				<div className="flex justify-end gap-2 border-t border-border px-4 py-3">
					<button
						onClick={() => handleAction("cancel")}
						className="rounded-md border border-error/50 px-4 py-1.5 text-sm text-error transition-colors hover:border-error"
					>
						Cancel
					</button>
					{!showRefine ? (
						<button
							onClick={() => setShowRefine(true)}
							className="rounded-md border border-border px-4 py-1.5 text-sm text-text transition-colors hover:border-dim"
						>
							Refine
						</button>
					) : (
						<button
							onClick={() => feedback.trim() && handleAction("refine")}
							disabled={!feedback.trim()}
							className="rounded-md border border-warn px-4 py-1.5 text-sm text-warn disabled:opacity-30"
						>
							Send Feedback
						</button>
					)}
					<button
						onClick={() => handleAction("approve")}
						className="rounded-md bg-accent px-4 py-1.5 text-sm text-white transition-opacity hover:opacity-90"
					>
						Approve
					</button>
				</div>
			</div>
		</div>
	);
}
