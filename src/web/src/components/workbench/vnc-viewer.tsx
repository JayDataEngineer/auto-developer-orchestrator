import { useCallback, useEffect, useState } from "react";
import { Monitor, PowerIcon } from "lucide-react";
import { usePuxStore } from "@pux/shared";

interface SandboxInfo {
	id: string;
	project_path: string;
	status: string;
	mode: string;
}

export function VNCViewer() {
	const [sandbox, setSandbox] = useState<SandboxInfo | null>(null);
	const [loading, setLoading] = useState(true);
	const [starting, setStarting] = useState(false);
	const activeProject = usePuxStore((s) => s.activeProject);
	const activeProjectPath = usePuxStore((s) => s.activeProjectPath);

	const detectSandbox = useCallback(() => {
		if (!activeProject) {
			setSandbox(null);
			setLoading(false);
			return;
		}
		setLoading(true);
		fetch("/api/sandbox/")
			.then((r) => r.json())
			.then((data: SandboxInfo[]) => {
				// Prefer exact id match, then exact project_path match
				const byId = data.find((sb) => sb.id === activeProject);
				const byPath = data.find((sb) => sb.project_path === activeProjectPath);
				const byBasename = data.find(
					(sb) => sb.project_path && sb.project_path.split("/").pop() === activeProject,
				);
				setSandbox(byId || byPath || byBasename || null);
			})
			.catch(() => setSandbox(null))
			.finally(() => setLoading(false));
	}, [activeProject]);

	// Re-detect when project changes
	useEffect(() => {
		detectSandbox();
	}, [detectSandbox]);

	const startSandbox = async () => {
		if (!activeProject) return;
		setStarting(true);
		try {
			const resp = await fetch("/api/sandbox/", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ project_path: activeProjectPath || activeProject }),
			});
			if (resp.ok) {
				const data = await resp.json();
				setSandbox(data);
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

	if (!sandbox) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3">
				<Monitor className="size-8 text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">No sandbox for this project</span>
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
			src={`/api/sandbox/vnc/${sandbox.id}/vnc.html?autoconnect=true&resize=scale`}
			className="h-full w-full border-0"
			title="Sandbox VNC"
		/>
	);
}
