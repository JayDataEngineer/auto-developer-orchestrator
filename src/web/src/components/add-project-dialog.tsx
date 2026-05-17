import { useState, useEffect, useCallback, useRef } from "react";
import {
	Sheet,
	SheetContent,
	SheetHeader,
	SheetTitle,
	SheetDescription,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { usePuxStore } from "@/lib/pux-store";
import {
	Folder,
	FolderOpen,
	File,
	ChevronRight,
	Loader2,
	AlertCircle,
	ArrowUp,
	Keyboard,
	Server,
} from "lucide-react";

interface BrowseEntry {
	name: string;
	isDir: boolean;
	size?: number;
}

interface BrowseResponse {
	path: string;
	parent: string;
	entries: BrowseEntry[];
}

interface AddProjectDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

async function browseFs(path: string): Promise<BrowseResponse> {
	const resp = await fetch(
		`/api/fs/browse?path=${encodeURIComponent(path)}`,
	);
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Failed to browse");
	}
	return resp.json();
}

async function sshConnect(user: string, host: string, port: string, password: string, keyData: string): Promise<{ sessionKey: string }> {
	const resp = await fetch("/api/pux/ssh/connect", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ user, host, port, password, keyData }),
	});
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Connection failed");
	}
	return resp.json();
}

async function sshBrowse(sessionKey: string, path: string): Promise<BrowseResponse> {
	const resp = await fetch("/api/pux/ssh/browse", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ sessionKey, path }),
	});
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Failed to browse remote");
	}
	return resp.json();
}

async function sshDisconnect(sessionKey: string): Promise<void> {
	await fetch("/api/pux/ssh/disconnect", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ sessionKey }),
	}).catch(() => {});
}

async function addProject(name: string, path: string): Promise<void> {
	const resp = await fetch("/api/projects/add", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ name, path }),
	});
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Failed to add project");
	}
}

type Source = "local" | "ssh" | "manual";

export function AddProjectDialog({
	open,
	onOpenChange,
}: AddProjectDialogProps) {
	const [source, setSource] = useState<Source>("local");
	const [currentPath, setCurrentPath] = useState("");
	const [entries, setEntries] = useState<BrowseEntry[]>([]);
	const [loading, setLoading] = useState(false);
	const [selectedPath, setSelectedPath] = useState("");
	const [projectName, setProjectName] = useState("");
	const [manualPath, setManualPath] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [adding, setAdding] = useState(false);
	const loadProjects = usePuxStore((s) => s.loadProjects);

	// SSH state
	const [sshHost, setSshHost] = useState("");
	const [sshPort, setSshPort] = useState("22");
	const [sshUser, setSshUser] = useState("");
	const [sshPassword, setSshPassword] = useState("");
	const [sshKeyData, setSshKeyData] = useState("");
	const [sshSessionKey, setSshSessionKey] = useState<string | null>(null);
	const [sshConnecting, setSshConnecting] = useState(false);
	const sshConnected = !!sshSessionKey;

	// Disconnect on close
	const prevOpen = useRef(open);
	useEffect(() => {
		if (prevOpen.current && !open && sshSessionKey) {
			sshDisconnect(sshSessionKey);
			setSshSessionKey(null);
		}
		if (open) {
			setSelectedPath("");
			setProjectName("");
			setError(null);
			setSource("local");
			setManualPath("");
		}
		prevOpen.current = open;
	}, [open, sshSessionKey]);

	const fetchDir = useCallback(async (path: string) => {
		setLoading(true);
		setError(null);
		try {
			const resp = source === "ssh" && sshSessionKey
				? await sshBrowse(sshSessionKey, path)
				: await browseFs(path);
			setCurrentPath(resp.path);
			setEntries(resp.entries);
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : "Failed to list directory");
		} finally {
			setLoading(false);
		}
	}, [source, sshSessionKey]);

	// Fetch on open or when SSH connects
	useEffect(() => {
		if (open && (source !== "ssh" || sshConnected)) {
			fetchDir("");
		}
	}, [open, source, sshConnected, fetchDir]);

	const navigateTo = (path: string) => {
		setSelectedPath("");
		fetchDir(path);
	};

	const selectFolder = (path: string) => {
		setSelectedPath(path);
		const name = path.split("/").pop() || path;
		setProjectName(name);
	};

	const handleSshConnect = async () => {
		if (!sshHost || !sshUser) return;
		setSshConnecting(true);
		setError(null);
		try {
			const { sessionKey } = await sshConnect(sshUser, sshHost, sshPort, sshPassword, sshKeyData);
			setSshSessionKey(sessionKey);
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : "Connection failed");
		} finally {
			setSshConnecting(false);
		}
	};

	// Breadcrumb segments from root
	const segments = currentPath
		.split("/")
		.filter(Boolean)
		.reduce<{ label: string; path: string }[]>((acc, seg, i) => {
			const path = "/" + currentPath.split("/").filter(Boolean).slice(0, i + 1).join("/");
			acc.push({ label: seg, path });
			return acc;
		}, []);

	const handleAdd = async () => {
		let path: string;
		if (source === "manual") {
			path = manualPath;
		} else {
			path = selectedPath;
		}
		// Prefix SSH paths
		if (source === "ssh" && sshHost) {
			const port = sshPort && sshPort !== "22" ? `:${sshPort}` : "";
			path = `ssh://${sshUser}@${sshHost}${port}${path}`;
		}
		if (!path || !projectName) return;
		setAdding(true);
		setError(null);
		try {
			await addProject(projectName, path);
			await loadProjects();
			onOpenChange(false);
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : "Failed to add project");
		} finally {
			setAdding(false);
		}
	};

	const handleManualBrowse = () => {
		if (manualPath.trim()) {
			fetchDir(manualPath.trim());
			setSource("local");
		}
	};

	const showBrowser = source !== "manual" && (source !== "ssh" || sshConnected);

	return (
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent side="left" className="sm:max-w-lg flex flex-col gap-0 p-0">
				<SheetHeader className="px-4 pt-4 pb-2 border-b">
					<SheetTitle className="flex items-center gap-2">
						<FolderOpen className="size-5" />
						Open Folder
					</SheetTitle>
					<SheetDescription>
						Browse and select a folder to add as a project
					</SheetDescription>
				</SheetHeader>

				{/* Source tabs */}
				<div className="flex border-b px-4 gap-1">
					<button
						type="button"
						className={`px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
							source === "local"
								? "border-primary text-foreground"
								: "border-transparent text-muted-foreground hover:text-foreground"
						}`}
						onClick={() => setSource("local")}
					>
						Local
					</button>
					<button
						type="button"
						className={`px-3 py-2 text-xs font-medium border-b-2 transition-colors flex items-center gap-1 ${
							source === "ssh"
								? "border-primary text-foreground"
								: "border-transparent text-muted-foreground hover:text-foreground"
						}`}
						onClick={() => setSource("ssh")}
					>
						<Server className="size-3" />
						SSH
					</button>
					<button
						type="button"
						className={`px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
							source === "manual"
								? "border-primary text-foreground"
								: "border-transparent text-muted-foreground hover:text-foreground"
						}`}
						onClick={() => setSource("manual")}
					>
						Manual
					</button>
				</div>

				{/* SSH connection form */}
				{source === "ssh" && !sshConnected && (
					<div className="flex-1 flex flex-col gap-4 px-4 py-6">
						<div className="grid grid-cols-[1fr_auto] gap-3">
							<label className="flex flex-col gap-1 text-sm">
								<span className="text-xs font-medium text-muted-foreground">Host</span>
								<Input
									placeholder="192.168.1.100"
									value={sshHost}
									onChange={(e) => setSshHost(e.target.value)}
								/>
							</label>
							<label className="flex flex-col gap-1 text-sm w-20">
								<span className="text-xs font-medium text-muted-foreground">Port</span>
								<Input
									value={sshPort}
									onChange={(e) => setSshPort(e.target.value)}
								/>
							</label>
						</div>
						<label className="flex flex-col gap-1 text-sm">
							<span className="text-xs font-medium text-muted-foreground">Username</span>
							<Input
								placeholder="ubuntu"
								value={sshUser}
								onChange={(e) => setSshUser(e.target.value)}
							/>
						</label>
						<label className="flex flex-col gap-1 text-sm">
							<span className="text-xs font-medium text-muted-foreground">Password</span>
							<Input
								type="password"
								placeholder="Password or leave blank for key auth"
								value={sshPassword}
								onChange={(e) => setSshPassword(e.target.value)}
								onKeyDown={(e) => { if (e.key === "Enter") handleSshConnect(); }}
							/>
						</label>
						<label className="flex flex-col gap-1 text-sm">
							<span className="text-xs font-medium text-muted-foreground">Private Key (optional)</span>
							<textarea
								className="flex min-h-[60px] w-full rounded-md border border-input bg-transparent px-3 py-2 text-xs shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring font-mono"
								placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
								value={sshKeyData}
								onChange={(e) => setSshKeyData(e.target.value)}
							/>
						</label>
						<Button
							onClick={handleSshConnect}
							disabled={sshConnecting || !sshHost || !sshUser}
							className="w-full gap-2"
						>
							{sshConnecting ? (
								<><Loader2 className="size-4 animate-spin" /> Connecting...</>
							) : (
								<><Server className="size-4" /> Connect</>
							)}
						</Button>
						{error && (
							<div className="flex items-center gap-2 text-sm text-destructive">
								<AlertCircle className="size-4 shrink-0" />
								{error}
							</div>
						)}
					</div>
				)}

				{/* Browser (local or SSH connected) */}
				{showBrowser ? (
					<>
						{/* Breadcrumb */}
						<div className="flex items-center gap-1 px-4 py-2 text-xs border-b bg-muted/30 overflow-x-auto">
							<button
								type="button"
								className="shrink-0 text-muted-foreground hover:text-foreground"
								onClick={() => navigateTo(source === "ssh" ? "/" : "/home")}
							>
								/
							</button>
							{segments.map((seg) => (
								<span key={seg.path} className="flex items-center gap-1 shrink-0">
									<ChevronRight className="size-3 text-muted-foreground" />
									<button
										type="button"
										className="text-muted-foreground hover:text-foreground truncate max-w-24"
										onClick={() => navigateTo(seg.path)}
									>
										{seg.label}
									</button>
								</span>
							))}
						</div>

						{/* Folder list */}
						<div className="flex-1 overflow-y-auto min-h-0">
							{currentPath !== "/" && (
								<button
									type="button"
									className="flex items-center gap-2 w-full px-4 py-1.5 text-sm hover:bg-muted/50 text-muted-foreground"
									onClick={() => navigateTo(currentPath.split("/").slice(0, -1).join("/") || "/")}
								>
									<ArrowUp className="size-4 shrink-0" />
									<span>..</span>
								</button>
							)}

							{loading ? (
								<div className="flex items-center justify-center py-12 text-muted-foreground">
									<Loader2 className="size-5 animate-spin mr-2" />
									Loading...
								</div>
							) : (
								entries.map((entry) => (
									<button
										key={entry.name}
										type="button"
										className={`flex items-center gap-2 w-full px-4 py-1.5 text-sm hover:bg-muted/50 ${
											selectedPath === `${currentPath}/${entry.name}` ? "bg-accent" : ""
										} ${!entry.isDir ? "text-muted-foreground" : ""}`}
										onClick={() => {
											if (entry.isDir) {
												const newPath = `${currentPath}/${entry.name}`;
												selectFolder(newPath);
												navigateTo(newPath);
											}
										}}
										title={entry.isDir ? `Select ${entry.name}` : undefined}
									>
										{entry.isDir ? (
											<Folder className="size-4 shrink-0 text-yellow-500" />
										) : (
											<File className="size-4 shrink-0" />
										)}
										<span className="truncate">{entry.name}</span>
									</button>
								))
							)}

							{!loading && entries.length === 0 && (
								<div className="px-4 py-8 text-sm text-muted-foreground text-center">
									Empty directory
								</div>
							)}
						</div>
					</>
				) : source === "manual" ? (
					/* Manual path input */
					<div className="flex-1 flex flex-col items-center justify-center gap-4 px-6">
						<Keyboard className="size-8 text-muted-foreground" />
						<p className="text-sm text-muted-foreground text-center">
							Enter the path to a project folder.
							<br />
							Useful for remote/SSH paths you know by heart.
						</p>
						<div className="flex w-full gap-2">
							<Input
								placeholder="/home/ubuntu/my-project"
								value={manualPath}
								onChange={(e) => setManualPath(e.target.value)}
								onKeyDown={(e) => {
									if (e.key === "Enter") handleManualBrowse();
								}}
							/>
							<Button
								variant="secondary"
								onClick={handleManualBrowse}
								disabled={!manualPath.trim()}
							>
								Browse
							</Button>
						</div>
					</div>
				) : null}

				{/* Footer */}
				<div className="border-t px-4 py-3 space-y-3">
					{error && source !== "ssh" && (
						<div className="flex items-center gap-2 text-sm text-destructive">
							<AlertCircle className="size-4 shrink-0" />
							{error}
						</div>
					)}

					{/* Selected path + name */}
					{selectedPath && showBrowser && (
						<div className="space-y-2">
							<div className="flex items-center gap-2 text-sm text-muted-foreground bg-muted/30 rounded px-3 py-1.5">
								{source === "ssh" ? <Server className="size-4 shrink-0" /> : <FolderOpen className="size-4 shrink-0" />}
								<span className="truncate">{selectedPath}</span>
							</div>
						</div>
					)}

					<div className="flex items-center gap-2">
						<Input
							placeholder="Project name"
							value={projectName}
							onChange={(e) => setProjectName(e.target.value)}
							className="flex-1"
						/>
						<Button
							onClick={handleAdd}
							disabled={
								adding ||
								!projectName ||
								(showBrowser && !selectedPath) ||
								(source === "manual" && !manualPath.trim())
							}
						>
							{adding ? <Loader2 className="size-4 animate-spin" /> : "Add"}
						</Button>
					</div>
				</div>
			</SheetContent>
		</Sheet>
	);
}
