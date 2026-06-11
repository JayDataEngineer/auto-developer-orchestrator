import { useCallback, useEffect, useRef, useState } from "react";
import { Monitor, PowerIcon } from "lucide-react";
import { usePuxStore } from "@pux/shared";

interface DesktopSession {
	is_active: boolean;
	mode: string;
	novnc_port: number;
}

interface SandboxInfo {
	id: string;
	project_path: string;
	status: string;
	mode: string;
	desktop_session: DesktopSession | null;
}

export function VNCViewer() {
	const [sandbox, setSandbox] = useState<SandboxInfo | null>(null);
	const [loading, setLoading] = useState(true);
	const [starting, setStarting] = useState(false);
	const [enabling, setEnabling] = useState(false);
	const [vncReady, setVncReady] = useState(false);
	const activeProject = usePuxStore((s) => s.activeProject);
	const activeProjectPath = usePuxStore((s) => s.activeProjectPath);
	const layoutVersion = usePuxStore((s) => s.workbenchLayoutVersion);
	const containerRef = useRef<HTMLDivElement>(null);

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
				const byId = data.find((sb) => sb.id === activeProject);
				const byPath = data.find((sb) => sb.project_path === activeProjectPath);
				const byBasename = data.find(
					(sb) => sb.project_path && sb.project_path.split("/").pop() === activeProject,
				);
				const match = byId || byPath || byBasename || null;

				// If we found a sandbox, fetch its full details (including desktop_session)
				if (match) {
					fetch(`/api/sandbox/${match.id}`)
						.then((r) => r.json())
						.then((full) => setSandbox(full))
						.catch(() => setSandbox(match))
						.finally(() => setLoading(false));
				} else {
					setSandbox(null);
					setLoading(false);
				}
			})
			.catch(() => {
				setSandbox(null);
				setLoading(false);
			});
	}, [activeProject, activeProjectPath]);

	// Initial detection on mount + project change
	useEffect(() => {
		detectSandbox();
	}, [detectSandbox]);

	// Auto-poll while sandbox isn't ready (no sandbox yet, or session not active)
	const isActive = sandbox?.desktop_session?.is_active ?? false;
	useEffect(() => {
		if (isActive) return;
		const interval = setInterval(detectSandbox, 3000);
		return () => clearInterval(interval);
	}, [isActive, detectSandbox]);

	// Poll vnc-health until the VNC server is actually accepting connections.
	// desktop_session.is_active just means the Go backend registered the session —
	// the actual x11vnc + websockify inside the container may still be starting.
	useEffect(() => {
		if (!isActive || !sandbox) {
			setVncReady(false);
			return;
		}
		let cancelled = false;
		const poll = () => {
			if (cancelled) return;
			fetch(`/api/sandbox/${sandbox.id}/vnc-health`)
				.then((r) => r.json())
				.then((data) => {
					if (cancelled) return;
					if (data.healthy) {
						setVncReady(true);
					} else {
						setTimeout(poll, 500);
					}
				})
				.catch(() => {
					if (!cancelled) setTimeout(poll, 500);
				});
		};
		poll();
		return () => { cancelled = true; };
	}, [isActive, sandbox]);

	// Auto-switch to VNC tab when VNC is actually ready
	const prevReady = useRef(false);
	useEffect(() => {
		if (vncReady && !prevReady.current) {
			usePuxStore.getState().setWorkbenchTab("vnc");
		}
		prevReady.current = vncReady;
	}, [vncReady]);

	// Resize the X11 framebuffer to match the container dimensions.
	// Uses a last-sent dimension guard to prevent feedback loops:
	// xrandr changes framebuffer → noVNC re-renders → container might
	// report same size → skip the API call, breaking the loop.
	const sandboxId = sandbox?.id;
	useEffect(() => {
		if (!sandboxId || !containerRef.current || !vncReady) return;

		let lastW = 0;
		let lastH = 0;

		const resizeToContainer = () => {
			const el = containerRef.current;
			if (!el) return;
			const w = Math.floor(el.clientWidth) - 4;
			const h = Math.floor(el.clientHeight) - 4;
			if (w < 100 || h < 100) return;
			// Skip if dimensions haven't changed — prevents infinite loop
			if (w === lastW && h === lastH) return;
			lastW = w;
			lastH = h;

			fetch(`/api/sandbox/${sandboxId}/x11/resolution`, {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ width: w, height: h }),
			}).catch(() => { /* sandbox may have been removed */ });
		};

		// Fire immediately for this layout change
		resizeToContainer();

		// ResizeObserver with debounce catches window resizes that don't
		// change panel percentages (so onLayoutChanged won't fire).
		let timer: ReturnType<typeof setTimeout>;
		const observer = new ResizeObserver(() => {
			clearTimeout(timer);
			timer = setTimeout(resizeToContainer, 200);
		});
		observer.observe(containerRef.current);

		return () => {
			clearTimeout(timer);
			observer.disconnect();
		};
	}, [sandboxId, layoutVersion, vncReady]);

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

	const enableDesktop = async () => {
		if (!sandbox) return;
		setEnabling(true);
		try {
			const resp = await fetch(`/api/sandbox/${sandbox.id}/desktop-mode`, {
				method: "POST",
			});
			if (resp.ok) {
				const session = await resp.json();
				setSandbox((prev) =>
					prev ? { ...prev, mode: "desktop", desktop_session: session } : prev,
				);
			}
		} catch {
			// ignore
		} finally {
			setEnabling(false);
		}
	};

	if (loading) {
		return (
			<div ref={containerRef} className="flex h-full items-center justify-center">
				<span className="text-xs text-muted-foreground">Detecting sandbox...</span>
			</div>
		);
	}

	if (!sandbox) {
		return (
			<div ref={containerRef} className="flex h-full flex-col items-center justify-center gap-3">
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

	// Sandbox exists but no desktop session — auto-polling will catch when it becomes ready
	if (!sandbox.desktop_session?.is_active) {
		const isReady = sandbox.status === "running";
		return (
			<div ref={containerRef} className="flex h-full flex-col items-center justify-center gap-3">
				<Monitor className="size-8 animate-pulse text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">
					{!isReady ? "Sandbox starting..." : sandbox.mode === "cli" ? "Sandbox ready" : "Connecting..."}
				</span>
				{isReady && sandbox.mode === "cli" && (
					<button
						onClick={enableDesktop}
						disabled={enabling}
						className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
					>
						<Monitor className="size-3" />
						{enabling ? "Enabling..." : "Enable desktop"}
					</button>
				)}
			</div>
		);
	}

	// Desktop session active but VNC server not yet accepting connections
	if (!vncReady) {
		return (
			<div ref={containerRef} className="flex h-full flex-col items-center justify-center gap-3">
				<Monitor className="size-8 animate-pulse text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">Starting VNC...</span>
			</div>
		);
	}

	const wsPath = `api/sandbox/vnc/${sandbox.id}/websockify`;

	return (
		<div ref={containerRef} className="h-full w-full">
			<iframe
				src={`/api/sandbox/vnc/${sandbox.id}/vnc.html?autoconnect=true&resize=remote&reconnect=true&reconnect_delay=2000&path=${encodeURIComponent(wsPath)}`}
				className="h-full w-full border-0"
				title="Sandbox VNC"
			/>
		</div>
	);
}
