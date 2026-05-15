import { useCallback, useEffect, useState } from "react";
import { Monitor, PowerIcon } from "lucide-react";
import { usePuxStore } from "@pux/shared";

export function VNCViewer() {
	const [sandboxId, setSandboxId] = useState<string | null>(null);
	const [loading, setLoading] = useState(true);
	const [starting, setStarting] = useState(false);
	const activeProject = usePuxStore((s) => s.activeProject);

	const detectSandbox = useCallback(() => {
		setLoading(true);
		fetch("/api/sandboxes")
			.then((r) => r.json())
			.then((data) => {
				const sandboxes = Array.isArray(data) ? data : [];
				if (sandboxes.length > 0) {
					setSandboxId(sandboxes[0].id || sandboxes[0]);
				} else {
					setSandboxId(null);
				}
			})
			.catch(() => setSandboxId(null))
			.finally(() => setLoading(false));
	}, []);

	useEffect(() => {
		detectSandbox();
	}, [detectSandbox]);

	const startSandbox = async () => {
		if (!activeProject) return;
		setStarting(true);
		try {
			const resp = await fetch("/api/sandbox", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ project_path: activeProject }),
			});
			if (resp.ok) {
				const data = await resp.json();
				setSandboxId(data.id);
			}
		} catch {
			// ignore
		} finally {
			setStarting(false);
		}
	};

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center">
				<span className="text-xs text-muted-foreground">Detecting sandbox...</span>
			</div>
		);
	}

	if (!sandboxId) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3">
				<Monitor className="size-8 text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">No sandbox running</span>
				{activeProject && (
					<button
						onClick={startSandbox}
						disabled={starting}
						className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
					>
						<PowerIcon className="size-3" />
						{starting ? "Starting..." : "Start sandbox"}
					</button>
				)}
			</div>
		);
	}

	return (
		<iframe
			src={`/api/sandbox/vnc/${sandboxId}/vnc.html?autoconnect=true&resize=scale`}
			className="h-full w-full border-0"
			title="Sandbox VNC"
		/>
	);
}
