import { usePuxStore } from "@/lib/pux-store";
import { BarChart3 } from "lucide-react";

export function MetricsFooter() {
	const usage = usePuxStore((s) => s.lastUsage);
	const metrics = usePuxStore((s) => s.contextMetrics);
	const compacting = usePuxStore((s) => s.compacting);

	return (
		<div className="flex items-center gap-4 border-t border-border px-3 py-1.5 text-[11px] text-dim">
			{usage && (
				<>
					<span>
						{usage.input.toLocaleString()} in / {usage.output.toLocaleString()} out
						{usage.cache > 0 && ` / ${usage.cache.toLocaleString()} cache`}
					</span>
					{usage.model && <span className="text-dim">{usage.model}</span>}
				</>
			)}
			{metrics && (
				<span className="flex items-center gap-1">
					<BarChart3 size={10} />
					ctx: {(metrics.contextUtil * 100).toFixed(0)}%
					<span className="text-dim">
						({(metrics.contextTokens / 1000).toFixed(1)}k / {(metrics.contextSize / 1000).toFixed(0)}k)
					</span>
				</span>
			)}
			{compacting && <span className="animate-pulse text-warn">Compressing...</span>}
		</div>
	);
}
