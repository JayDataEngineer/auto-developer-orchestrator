import { useEffect, useState } from "react";
import { Monitor } from "lucide-react";

export function VNCViewer() {
	const [sandboxId, setSandboxId] = useState<string | null>(null);
	const [loading, setLoading] = useState(true);

	useEffect(() => {
		fetch("/api/sandboxes")
			.then((r) => r.json())
			.then((data) => {
				const sandboxes = Array.isArray(data) ? data : [];
				if (sandboxes.length > 0) {
					setSandboxId(sandboxes[0].id || sandboxes[0]);
				}
			})
			.catch(() => {})
			.finally(() => setLoading(false));
	}, []);

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center">
				<span className="text-xs text-muted-foreground">Detecting sandbox...</span>
			</div>
		);
	}

	if (!sandboxId) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-2">
				<Monitor className="size-8 text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">No sandbox running</span>
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
