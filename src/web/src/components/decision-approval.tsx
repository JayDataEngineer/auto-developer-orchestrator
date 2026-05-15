import { ShieldCheck } from "lucide-react";
import { usePuxStore } from "@/lib/pux-store";

export function DecisionApproval() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);

	if (!pending) return null;

	return (
		<div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
			<div className="w-[440px] max-w-[90vw] rounded-xl border border-border bg-surface shadow-2xl">
				<div className="flex items-center gap-2 border-b border-border px-4 py-3">
					<ShieldCheck size={14} className="text-yellow-500" />
					<span className="text-xs font-semibold uppercase tracking-wider text-dim">
						Approval Required
					</span>
				</div>
				{pending.title && (
					<div className="px-4 py-3 text-sm font-medium text-text">
						{pending.title}
					</div>
				)}
				{pending.description && (
					<div className="px-4 pb-3 text-sm text-muted-foreground">
						{pending.description}
					</div>
				)}
				<div className="flex gap-2 border-t border-border px-4 py-3">
					<button
						onClick={() => respond("approve", "")}
						className="rounded-md bg-green-600 px-4 py-1.5 text-sm text-white hover:bg-green-700"
					>
						Approve
					</button>
					<button
						onClick={() => respond("reject", "")}
						className="rounded-md bg-red-600 px-4 py-1.5 text-sm text-white hover:bg-red-700"
					>
						Reject
					</button>
				</div>
			</div>
		</div>
	);
}
