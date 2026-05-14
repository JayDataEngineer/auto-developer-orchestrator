import { ShieldCheck } from "lucide-react";
import { usePuxStore } from "@/lib/pux-store";

export function ApprovalDialog() {
	const pending = usePuxStore((s) => s.pendingApproval);
	const respond = usePuxStore((s) => s.respondToApproval);

	if (!pending) return null;

	return (
		<div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
			<div className="w-[440px] max-w-[90vw] rounded-xl border border-border bg-surface shadow-2xl">
				<div className="flex items-center gap-2 border-b border-border px-4 py-3">
					<ShieldCheck size={14} className="text-warn" />
					<span className="text-xs font-semibold uppercase tracking-wider text-dim">
						Approval Required
					</span>
				</div>
				<div className="px-4 py-3 text-sm whitespace-pre-wrap text-text">
					{pending.title && (
						<div className="mb-2 font-semibold">{pending.title}</div>
					)}
					{pending.description}
				</div>
				<div className="flex justify-end gap-2 border-t border-border px-4 py-3">
					<button
						onClick={() => respond(false)}
						className="rounded-md border border-border px-4 py-1.5 text-sm text-text transition-colors hover:border-dim"
					>
						Reject
					</button>
					<button
						onClick={() => respond(true)}
						className="rounded-md bg-accent px-4 py-1.5 text-sm text-white transition-opacity hover:opacity-90"
					>
						Approve
					</button>
				</div>
			</div>
		</div>
	);
}
