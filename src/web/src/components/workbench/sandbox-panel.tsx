import { useEffect, useState, useRef } from "react";
import { Monitor, Globe } from "lucide-react";
import { cn } from "@/lib/utils";

export function SandboxPanel() {
	const [screenshot, setScreenshot] = useState<string | null>(null);
	const [currentUrl, setCurrentUrl] = useState("");
	const [sandboxId, setSandboxId] = useState("");
	const [active, setActive] = useState(false);
	const intervalRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined);

	useEffect(() => {
		// Resolve sandbox ID
		fetch("/api/sandboxes")
			.then((r) => r.json())
			.then((data) => {
				const sandboxes = Array.isArray(data) ? data : [];
				if (sandboxes.length > 0) {
					const id = sandboxes[0].id || sandboxes[0];
					setSandboxId(id);
				}
			})
			.catch(() => {});

		return () => {
			if (intervalRef.current) clearInterval(intervalRef.current);
		};
	}, []);

	useEffect(() => {
		if (!sandboxId) return;

		const poll = async () => {
			try {
				const resp = await fetch(`/api/sandbox/${sandboxId}/screenshot`);
				if (!resp.ok) {
					setActive(false);
					return;
				}
				const data = await resp.json();
				if (data.screenshot) {
					setScreenshot(data.screenshot);
					setCurrentUrl(data.url || "");
					setActive(true);
				}
			} catch {
				setActive(false);
			}
		};

		poll();
		intervalRef.current = setInterval(poll, 2000);
		return () => {
			if (intervalRef.current) clearInterval(intervalRef.current);
		};
	}, [sandboxId]);

	return (
		<div className="flex h-full flex-col">
			<div className="flex h-7 items-center gap-2 border-b border-border px-3">
				<Monitor size={12} className="text-dim" />
				<span className="text-[11px] font-bold uppercase tracking-wider text-dim">
					Sandbox
				</span>
				<span className={cn("ml-auto text-[11px]", active ? "text-success" : "text-dim")}>
					{active ? "Live" : "Inactive"}
				</span>
			</div>

			{currentUrl && (
				<div className="flex items-center gap-1.5 border-b border-border px-3 py-1">
					<Globe size={10} className="text-dim" />
					<span className="truncate text-[11px] text-dim">{currentUrl}</span>
				</div>
			)}

			<div className="flex flex-1 items-center justify-center bg-bg">
				{screenshot ? (
					<img
						src={`data:image/png;base64,${screenshot}`}
						alt="Sandbox"
						className="max-h-full max-w-full object-contain"
					/>
				) : (
					<span className="text-xs text-dim">No sandbox running</span>
				)}
			</div>
		</div>
	);
}
