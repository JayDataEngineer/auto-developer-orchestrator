import { useState } from "react";
import { MessageCircleQuestion } from "lucide-react";
import { usePuxStore } from "@/lib/pux-store";

export function DecisionQuestion() {
	const pending = usePuxStore((s) => s.pendingDecision);
	const respond = usePuxStore((s) => s.respondToDecision);
	const [input, setInput] = useState("");

	if (!pending) return null;

	const handleSubmit = (value: string) => {
		respond("answer", value);
		setInput("");
	};

	return (
		<div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
			<div className="w-[440px] max-w-[90vw] rounded-xl border border-border bg-surface shadow-2xl">
				<div className="flex items-center gap-2 border-b border-border px-4 py-3">
					<MessageCircleQuestion size={14} className="text-accent" />
					<span className="text-xs font-semibold uppercase tracking-wider text-dim">
						Question
					</span>
				</div>
				<div className="px-4 py-3 text-sm whitespace-pre-wrap text-text">
					{pending.title}
				</div>

				{(pending.options?.length ?? 0) > 0 && (
					<div className="px-4 pb-2 space-y-1.5">
						{pending.options!.map((opt) => (
							<button
								key={opt}
								onClick={() => handleSubmit(opt)}
								className="block w-full rounded-md border border-border bg-bg px-3 py-2 text-left text-sm text-text transition-colors hover:border-accent"
							>
								{opt}
							</button>
						))}
					</div>
				)}

				{pending.allowFreeText !== false && (
					<div className="border-t border-border px-4 py-3 space-y-2">
						<input
							value={input}
							onChange={(e) => setInput(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter" && input.trim()) handleSubmit(input.trim());
							}}
							placeholder="Type your answer..."
							className="w-full rounded-lg border border-border bg-bg px-3 py-2 text-sm text-text outline-none focus:border-accent"
						/>
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
		</div>
	);
}
