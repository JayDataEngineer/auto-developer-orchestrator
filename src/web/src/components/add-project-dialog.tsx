import { useState, useEffect, useCallback, useRef, useMemo } from "react";
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
	FileCode,
	FileImage,
	FileText,
	FileJson,
	FileArchive,
	ChevronRight,
	Loader2,
	AlertCircle,
	ArrowUp,
	Keyboard,
	Server,
	Search,
	Eye,
	EyeOff,
	FolderPlus,
	Home,
	ArrowRight,
	Wifi,
	Check,
	X,
	Plus,
} from "lucide-react";

// ── Types ──

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

interface TailscaleDevice {
	name: string;
	hostname: string;
	tailscaleIPs: string[];
	os: string;
	online: boolean;
}

interface SshConnection {
	host: string;
	port: string;
	user: string;
	id: string; // user@host:port
}

interface AddProjectDialogProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

// ── API helpers ──

async function browseFs(path: string, showHidden = false): Promise<BrowseResponse> {
	const params = new URLSearchParams({ path });
	if (showHidden) params.set("hidden", "1");
	const resp = await fetch(`/api/fs/browse?${params}`);
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Failed to browse");
	}
	return resp.json();
}

async function mkdirFs(path: string, name: string): Promise<{ path: string }> {
	const resp = await fetch("/api/fs/mkdir", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ path, name }),
	});
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Failed to create folder");
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

async function sshMkdir(sessionKey: string, path: string, name: string): Promise<{ path: string }> {
	const resp = await fetch("/api/pux/ssh/mkdir", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ sessionKey, path, name }),
	});
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Failed to create remote folder");
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

async function sshTrustHost(host: string, port: string): Promise<{ fingerprint: string }> {
	const resp = await fetch("/api/pux/ssh/trust-host", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ host, port }),
	});
	if (!resp.ok) {
		const data = await resp.json().catch(() => ({}));
		throw new Error(data.error || "Failed to get host key");
	}
	return resp.json();
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

async function fetchTailscaleDevices(): Promise<{ available: boolean; devices: TailscaleDevice[] }> {
	const resp = await fetch("/api/tailscale/devices");
	if (!resp.ok) return { available: false, devices: [] };
	return resp.json();
}

// ── localStorage for recent SSH ──

const SSH_RECENT_KEY = "pux:ssh-recent";

function loadRecentSsh(): SshConnection[] {
	try {
		return JSON.parse(localStorage.getItem(SSH_RECENT_KEY) || "[]");
	} catch { return []; }
}

function saveRecentSsh(conn: SshConnection) {
	const existing = loadRecentSsh().filter(c => c.id !== conn.id);
	existing.unshift(conn);
	if (existing.length > 5) existing.length = 5;
	localStorage.setItem(SSH_RECENT_KEY, JSON.stringify(existing));
}

// ── File icon helper ──

function getFileIcon(name: string, isDir: boolean) {
	if (isDir) return Folder;
	const ext = name.split(".").pop()?.toLowerCase() || "";
	if (["ts", "tsx", "js", "jsx", "py", "go", "rs", "rb", "java", "c", "cpp", "h", "swift", "kt"].includes(ext)) return FileCode;
	if (["png", "jpg", "jpeg", "gif", "svg", "webp", "ico", "bmp"].includes(ext)) return FileImage;
	if (["json", "yaml", "yml", "toml", "xml", "env"].includes(ext)) return FileJson;
	if (["zip", "tar", "gz", "bz2", "xz", "7z", "rar", "deb"].includes(ext)) return FileArchive;
	if (["md", "txt", "log", "csv"].includes(ext)) return FileText;
	return File;
}

// ── Source tabs ──

type Source = "local" | "ssh" | "tailscale" | "manual";

const SOURCES: { key: Source; label: string; icon: typeof Server }[] = [
	{ key: "local", label: "Local", icon: FolderOpen },
	{ key: "ssh", label: "SSH", icon: Server },
	{ key: "tailscale", label: "Tailscale", icon: Wifi },
	{ key: "manual", label: "Manual", icon: Keyboard },
];

// ── Main component ──

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
	const [searchQuery, setSearchQuery] = useState("");
	const [showHidden, setShowHidden] = useState(false);
	const [creatingFolder, setCreatingFolder] = useState(false);
	const [newFolderName, setNewFolderName] = useState("");
	const [focusedIndex, setFocusedIndex] = useState(-1);
	const loadProjects = usePuxStore((s) => s.loadProjects);
	const listRef = useRef<HTMLDivElement>(null);
	const searchRef = useRef<HTMLInputElement>(null);

	// SSH state
	const [sshHost, setSshHost] = useState("");
	const [sshPort, setSshPort] = useState("22");
	const [sshUser, setSshUser] = useState("");
	const [sshPassword, setSshPassword] = useState("");
	const [sshKeyData, setSshKeyData] = useState("");
	const [sshSessionKey, setSshSessionKey] = useState<string | null>(null);
	const [sshConnecting, setSshConnecting] = useState(false);
	const [sshRecentConns] = useState<SshConnection[]>(loadRecentSsh);
	const [sshHostKeyError, setSshHostKeyError] = useState<string | null>(null);
	const [sshHostFingerprint, setSshHostFingerprint] = useState<string | null>(null);
	const sshConnected = !!sshSessionKey;

	// Tailscale state
	const [tsDevices, setTsDevices] = useState<TailscaleDevice[]>([]);
	const [tsLoading, setTsLoading] = useState(false);
	const [tsAvailable, setTsAvailable] = useState(false);

	// Reset on open/close
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
			setSearchQuery("");
			setShowHidden(false);
			setNewFolderName("");
			setCreatingFolder(false);
			setFocusedIndex(-1);
		}
		prevOpen.current = open;
	}, [open, sshSessionKey]);

	const fetchDir = useCallback(async (path: string) => {
		setLoading(true);
		setError(null);
		setSearchQuery("");
		setFocusedIndex(-1);
		setNewFolderName("");
		setCreatingFolder(false);
		try {
			const resp = source === "ssh" && sshSessionKey
				? await sshBrowse(sshSessionKey, path)
				: await browseFs(path, showHidden);
			setCurrentPath(resp.path);
			setEntries(resp.entries);
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : "Failed to list directory");
		} finally {
			setLoading(false);
		}
	}, [source, sshSessionKey, showHidden]);

	// Fetch on open or when SSH connects or when showHidden changes
	useEffect(() => {
		if (open && (source !== "ssh" || sshConnected) && source !== "tailscale" && source !== "manual") {
			fetchDir(currentPath || "");
		}
	}, [open, source, sshConnected, showHidden]); // eslint-disable-line react-hooks/exhaustive-deps

	// Fetch Tailscale devices when tab selected
	useEffect(() => {
		if (open && source === "tailscale") {
			setTsLoading(true);
			fetchTailscaleDevices().then(r => {
				setTsAvailable(r.available);
				setTsDevices(r.devices);
			}).finally(() => setTsLoading(false));
		}
	}, [open, source]);

	const navigateTo = useCallback((path: string) => {
		fetchDir(path);
	}, [fetchDir]);

	// Selection: single click selects, does NOT navigate
	const selectEntry = useCallback((entry: BrowseEntry) => {
		if (!entry.isDir) return;
		const newPath = currentPath === "/" ? `/${entry.name}` : `${currentPath}/${entry.name}`;
		setSelectedPath(newPath);
		setProjectName(entry.name);
		setFocusedIndex(-1);
	}, [currentPath]);

	// Double click navigates into
	const openEntry = useCallback((entry: BrowseEntry) => {
		if (!entry.isDir) return;
		const newPath = currentPath === "/" ? `/${entry.name}` : `${currentPath}/${entry.name}`;
		navigateTo(newPath);
	}, [currentPath, navigateTo]);

	// Select current directory (the "Use this folder" action)
	const selectCurrentDir = useCallback(() => {
		if (!currentPath) return;
		setSelectedPath(currentPath);
		setProjectName(currentPath.split("/").filter(Boolean).pop() || currentPath);
	}, [currentPath]);

	// Keyboard navigation
	useEffect(() => {
		if (!open || source === "manual" || source === "tailscale") return;
		if (source === "ssh" && !sshConnected) return;

		const handler = (e: KeyboardEvent) => {
			// Don't capture when typing in inputs
			if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;

			const filteredEntries = getFilteredEntries();

			switch (e.key) {
				case "ArrowDown":
					e.preventDefault();
					setFocusedIndex(prev => {
						const next = Math.min(prev + 1, filteredEntries.length - 1);
						scrollToIndex(next);
						return next;
					});
					break;
				case "ArrowUp":
					e.preventDefault();
					setFocusedIndex(prev => {
						const next = Math.max(prev - 1, 0);
						scrollToIndex(next);
						return next;
					});
					break;
				case "Enter":
					e.preventDefault();
					if (focusedIndex >= 0 && focusedIndex < filteredEntries.length) {
						openEntry(filteredEntries[focusedIndex]);
					}
					break;
				case " ":
					e.preventDefault();
					if (focusedIndex >= 0 && focusedIndex < filteredEntries.length) {
						selectEntry(filteredEntries[focusedIndex]);
					}
					break;
				case "Backspace":
					e.preventDefault();
					if (currentPath && currentPath !== "/") {
						navigateTo(currentPath.split("/").slice(0, -1).join("/") || "/");
					}
					break;
				case "Escape":
					e.preventDefault();
					if (searchQuery) {
						setSearchQuery("");
					}
					break;
			}
		};

		window.addEventListener("keydown", handler);
		return () => window.removeEventListener("keydown", handler);
	}, [open, source, sshConnected, focusedIndex, searchQuery, currentPath, entries, openEntry, selectEntry, navigateTo]);

	const scrollToIndex = (index: number) => {
		if (!listRef.current) return;
		const items = listRef.current.querySelectorAll("[data-entry]");
		items[index]?.scrollIntoView({ block: "nearest" });
	};

	// Filtered entries for search
	const getFilteredEntries = useCallback(() => {
		if (!searchQuery) return entries;
		const q = searchQuery.toLowerCase();
		return entries.filter(e => e.name.toLowerCase().includes(q));
	}, [entries, searchQuery]);

	const filteredEntries = useMemo(() => getFilteredEntries(), [getFilteredEntries]);

	// SSH connect
	const handleSshConnect = async (host?: string, user?: string, port?: string) => {
		const h = host || sshHost;
		const u = user || sshUser;
		const p = port || sshPort;
		if (!h || !u) return;
		setSshConnecting(true);
		setError(null);
		setSshHostKeyError(null);
		setSshHostFingerprint(null);
		try {
			const { sessionKey } = await sshConnect(u, h, p, sshPassword, sshKeyData);
			setSshSessionKey(sessionKey);
			saveRecentSsh({ host: h, port: p, user: u, id: `${u}@${h}:${p}` });
		} catch (err: unknown) {
			const msg = err instanceof Error ? err.message : "Connection failed";
			// Check if it's a host key error
			if (msg.toLowerCase().includes("host key") || msg.toLowerCase().includes("known_hosts")) {
				setSshHostKeyError(msg);
				// Try to get fingerprint
				try {
					const { fingerprint } = await sshTrustHost(h, p);
					setSshHostFingerprint(fingerprint);
				} catch { /* ignore */ }
			} else {
				setError(msg);
			}
		} finally {
			setSshConnecting(false);
		}
	};

	// SSH trust and retry
	const handleSshTrust = async () => {
		if (!sshHost) return;
		try {
			// Trust host by attempting trust-host, which adds to known_hosts
			await sshTrustHost(sshHost, sshPort);
			// Retry connection
			setSshHostKeyError(null);
			handleSshConnect();
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : "Failed to trust host");
		}
	};

	// Tailscale connect — uses SSH with the Tailscale IP
	const handleTailscaleConnect = async (device: TailscaleDevice) => {
		const ip = device.tailscaleIPs?.[0];
		if (!ip) {
			setError("No Tailscale IP for this device");
			return;
		}
		setSshHost(ip);
		setSshUser("ubuntu");
		setSshPort("22");
		setSource("ssh");
		// Auto-connect
		setSshConnecting(true);
		setError(null);
		try {
			const { sessionKey } = await sshConnect("ubuntu", ip, "22", "", "");
			setSshSessionKey(sessionKey);
		} catch (err: unknown) {
			// Tailscale SSH might need different user, show SSH form for manual adjust
			setError(err instanceof Error ? err.message : "Tailscale SSH failed. Adjust credentials below.");
		} finally {
			setSshConnecting(false);
		}
	};

	// Create folder
	const handleCreateFolder = async () => {
		if (!newFolderName.trim()) return;
		setCreatingFolder(false);
		setError(null);
		try {
			if (source === "ssh" && sshSessionKey) {
				await sshMkdir(sshSessionKey, currentPath, newFolderName.trim());
			} else {
				await mkdirFs(currentPath, newFolderName.trim());
			}
			setNewFolderName("");
			fetchDir(currentPath); // refresh
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : "Failed to create folder");
		}
	};

	// Breadcrumb segments
	const segments = currentPath
		.split("/")
		.filter(Boolean)
		.reduce<{ label: string; path: string }[]>((acc, seg, i) => {
			const path = "/" + currentPath.split("/").filter(Boolean).slice(0, i + 1).join("/");
			acc.push({ label: seg, path });
			return acc;
		}, []);

	// Add project
	const handleAdd = async () => {
		let path: string;
		if (source === "manual") {
			path = manualPath;
		} else {
			path = selectedPath;
		}
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

	const showBrowser = (source === "local" || (source === "ssh" && sshConnected));
	const canAdd = !adding && projectName && (
		(source === "manual" && manualPath.trim()) ||
		(showBrowser && selectedPath)
	);

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
				<div className="flex border-b px-2 gap-0">
					{SOURCES.map(({ key, label, icon: Icon }) => (
						<button
							key={key}
							type="button"
							className={`px-3 py-2 text-xs font-medium border-b-2 transition-colors flex items-center gap-1.5 ${
								source === key
									? "border-primary text-foreground"
									: "border-transparent text-muted-foreground hover:text-foreground"
							}`}
							onClick={() => setSource(key)}
						>
							<Icon className="size-3" />
							{label}
						</button>
					))}
				</div>

				{/* ── SSH connection form ── */}
				{source === "ssh" && !sshConnected && (
					<div className="flex-1 overflow-y-auto flex flex-col gap-4 px-4 py-4">
						{/* Host key trust prompt */}
						{sshHostKeyError && (
							<div className="rounded-md border border-yellow-500/30 bg-yellow-500/5 p-3 space-y-2">
								<div className="flex items-center gap-2 text-sm text-yellow-600 dark:text-yellow-400">
									<AlertCircle className="size-4 shrink-0" />
									<span className="font-medium">Unknown Host Key</span>
								</div>
								{sshHostFingerprint && (
									<code className="block text-xs bg-muted p-2 rounded font-mono break-all">
										{sshHostFingerprint}
									</code>
								)}
								<Button size="sm" onClick={handleSshTrust} className="gap-1">
									<Check className="size-3" /> Trust & Connect
								</Button>
							</div>
						)}

						{/* Recent connections */}
						{sshRecentConns.length > 0 && (
							<div className="space-y-1">
								<span className="text-xs font-medium text-muted-foreground">Recent</span>
								{sshRecentConns.map(conn => (
									<button
										key={conn.id}
										type="button"
										className="flex items-center gap-2 w-full px-3 py-2 text-sm rounded-md hover:bg-muted/50 text-left"
										onClick={() => {
											setSshHost(conn.host);
											setSshUser(conn.user);
											setSshPort(conn.port);
										}}
									>
										<Server className="size-3.5 text-muted-foreground shrink-0" />
										<span className="font-mono text-xs">{conn.user}@{conn.host}</span>
										{conn.port !== "22" && <span className="text-xs text-muted-foreground">:{conn.port}</span>}
									</button>
								))}
							</div>
						)}

						<div className="grid grid-cols-[1fr_auto] gap-3">
							<label className="flex flex-col gap-1 text-sm">
								<span className="text-xs font-medium text-muted-foreground">Host</span>
								<Input
									placeholder="192.168.1.100 or user@host"
									value={sshHost}
									onChange={(e) => setSshHost(e.target.value)}
								/>
							</label>
							<label className="flex flex-col gap-1 text-sm w-20">
								<span className="text-xs font-medium text-muted-foreground">Port</span>
								<Input value={sshPort} onChange={(e) => setSshPort(e.target.value)} />
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
							onClick={() => handleSshConnect()}
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

				{/* ── Tailscale tab ── */}
				{source === "tailscale" && (
					<div className="flex-1 overflow-y-auto px-4 py-4">
						{tsLoading ? (
							<div className="flex items-center justify-center py-12 text-muted-foreground">
								<Loader2 className="size-5 animate-spin mr-2" /> Discovering devices...
							</div>
						) : !tsAvailable ? (
							<div className="flex flex-col items-center justify-center py-12 gap-3">
								<Wifi className="size-8 text-muted-foreground" />
								<p className="text-sm text-muted-foreground text-center">
									Tailscale is not running on this machine.
									<br />Install and start Tailscale to discover devices.
								</p>
							</div>
						) : tsDevices.length === 0 ? (
							<div className="flex flex-col items-center justify-center py-12 gap-3">
								<Wifi className="size-8 text-muted-foreground" />
								<p className="text-sm text-muted-foreground">No online devices found.</p>
							</div>
						) : (
							<div className="space-y-1">
								<span className="text-xs font-medium text-muted-foreground px-1">
									{tsDevices.length} device{tsDevices.length !== 1 ? "s" : ""} online
								</span>
								{tsDevices.map((device, i) => (
									<button
										key={i}
										type="button"
										className="flex items-center gap-3 w-full px-3 py-2.5 text-sm rounded-md hover:bg-muted/50 text-left"
										onClick={() => handleTailscaleConnect(device)}
									>
										<Server className="size-4 text-blue-500 shrink-0" />
										<div className="flex-1 min-w-0">
											<div className="font-medium truncate">{device.hostname}</div>
											<div className="text-xs text-muted-foreground font-mono">
												{device.tailscaleIPs?.[0] || device.name}
											</div>
										</div>
										<span className="text-xs text-muted-foreground">{device.os}</span>
									</button>
								))}
							</div>
						)}
						{error && (
							<div className="flex items-center gap-2 text-sm text-destructive mt-4">
								<AlertCircle className="size-4 shrink-0" />
								{error}
							</div>
						)}
					</div>
				)}

				{/* ── Manual path input ── */}
				{source === "manual" && (
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
				)}

				{/* ── Browser (local or SSH connected) ── */}
				{showBrowser && (
					<>
						{/* Toolbar: search + actions */}
						<div className="flex items-center gap-1.5 px-3 py-2 border-b">
							<div className="relative flex-1">
								<Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
								<Input
									ref={searchRef}
									placeholder="Search..."
									value={searchQuery}
									onChange={(e) => {
										setSearchQuery(e.target.value);
										setFocusedIndex(-1);
									}}
									className="h-8 text-xs pl-8"
								/>
							</div>
							<Button
								variant="ghost"
								size="icon"
								className="size-8"
								onClick={() => setShowHidden(!showHidden)}
								title={showHidden ? "Hide dotfiles" : "Show dotfiles"}
							>
								{showHidden ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
							</Button>
							<Button
								variant="ghost"
								size="icon"
								className="size-8"
								onClick={() => setCreatingFolder(true)}
								title="New folder"
							>
								<FolderPlus className="size-3.5" />
							</Button>
							<Button
								variant="ghost"
								size="icon"
								className="size-8"
								onClick={() => {
									if (source === "ssh") navigateTo("/");
									else {
										const home = "/home";
										navigateTo(home);
									}
								}}
								title="Home"
							>
								<Home className="size-3.5" />
							</Button>
						</div>

						{/* New folder inline input */}
						{creatingFolder && (
							<div className="flex items-center gap-2 px-4 py-2 border-b bg-muted/20">
								<FolderPlus className="size-4 text-muted-foreground shrink-0" />
								<Input
									autoFocus
									placeholder="Folder name"
									value={newFolderName}
									onChange={(e) => setNewFolderName(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter") handleCreateFolder();
										if (e.key === "Escape") { setCreatingFolder(false); setNewFolderName(""); }
									}}
									className="h-7 text-xs"
								/>
								<Button size="icon" variant="ghost" className="size-7" onClick={handleCreateFolder}>
									<Check className="size-3.5" />
								</Button>
								<Button size="icon" variant="ghost" className="size-7" onClick={() => { setCreatingFolder(false); setNewFolderName(""); }}>
									<X className="size-3.5" />
								</Button>
							</div>
						)}

						{/* Breadcrumb */}
						<div className="flex items-center gap-1 px-4 py-1.5 text-xs border-b bg-muted/30 overflow-x-auto">
							<button
								type="button"
								className="shrink-0 text-muted-foreground hover:text-foreground"
								onClick={() => navigateTo(source === "ssh" ? "/" : "/home")}
								title="Root"
							>
								/
							</button>
							{segments.map((seg) => (
								<span key={seg.path} className="flex items-center gap-1 shrink-0">
									<ChevronRight className="size-3 text-muted-foreground" />
									<button
										type="button"
										className="text-muted-foreground hover:text-foreground truncate max-w-32"
										onClick={() => navigateTo(seg.path)}
									>
										{seg.label}
									</button>
								</span>
							))}
						</div>

						{/* Folder list */}
						<div className="flex-1 overflow-y-auto min-h-0" ref={listRef}>
							{currentPath && currentPath !== "/" && (
								<button
									type="button"
									className="flex items-center gap-2 w-full px-4 py-1.5 text-sm hover:bg-muted/50 text-muted-foreground"
									onClick={() => navigateTo(currentPath.split("/").slice(0, -1).join("/") || "/")}
								>
									<ArrowUp className="size-4 shrink-0" />
									<span>..</span>
								</button>
							)}

							{/* Select current dir option */}
							{currentPath && (
								<button
									type="button"
									className={`flex items-center gap-2 w-full px-4 py-1.5 text-sm hover:bg-muted/50 ${
										selectedPath === currentPath ? "bg-accent" : ""
									}`}
									onClick={selectCurrentDir}
								>
									<FolderOpen className="size-4 shrink-0 text-primary" />
									<span className="text-primary font-medium">Use this folder</span>
									<ArrowRight className="size-3 text-muted-foreground ml-auto" />
								</button>
							)}

							{loading ? (
								<div className="flex items-center justify-center py-12 text-muted-foreground">
									<Loader2 className="size-5 animate-spin mr-2" />
									Loading...
								</div>
							) : (
								filteredEntries.map((entry, i) => {
									const Icon = getFileIcon(entry.name, entry.isDir);
									const fullPath = currentPath === "/" ? `/${entry.name}` : `${currentPath}/${entry.name}`;
									const isSelected = selectedPath === fullPath;
									const isFocused = focusedIndex === i;

									if (!entry.isDir) {
										return (
											<div
												key={entry.name}
												className="flex items-center gap-2 px-4 py-1.5 text-sm text-muted-foreground/50"
											>
												<Icon className="size-4 shrink-0 opacity-50" />
												<span className="truncate">{entry.name}</span>
											</div>
										);
									}

									return (
										<button
											key={entry.name}
											type="button"
											data-entry
											className={`flex items-center gap-2 w-full px-4 py-1.5 text-sm transition-colors ${
												isSelected
													? "bg-accent text-accent-foreground"
													: isFocused
														? "bg-muted/50"
														: "hover:bg-muted/50"
											}`}
											onClick={() => selectEntry(entry)}
											onDoubleClick={() => openEntry(entry)}
											title={`Click to select, double-click to open ${entry.name}`}
										>
											{isSelected ? (
												<FolderOpen className="size-4 shrink-0 text-primary" />
											) : (
												<Folder className="size-4 shrink-0 text-yellow-500" />
											)}
											<span className="truncate">{entry.name}</span>
										</button>
									);
								})
							)}

							{!loading && filteredEntries.length === 0 && (
								<div className="px-4 py-8 text-sm text-muted-foreground text-center">
									{searchQuery ? `No matches for "${searchQuery}"` : "Empty directory"}
								</div>
							)}
						</div>
					</>
				)}

				{/* Footer */}
				<div className="border-t px-4 py-3 space-y-3">
					{error && source !== "ssh" && (
						<div className="flex items-center gap-2 text-sm text-destructive">
							<AlertCircle className="size-4 shrink-0" />
							{error}
						</div>
					)}

					{/* Selected path preview */}
					{selectedPath && showBrowser && (
						<div className="flex items-center gap-2 text-sm text-muted-foreground bg-muted/30 rounded px-3 py-1.5">
							{source === "ssh" ? <Server className="size-4 shrink-0" /> : <FolderOpen className="size-4 shrink-0" />}
							<span className="truncate">{selectedPath}</span>
						</div>
					)}

					<div className="flex items-center gap-2">
						<Input
							placeholder="Project name"
							value={projectName}
							onChange={(e) => setProjectName(e.target.value)}
							className="flex-1"
						/>
						<Button onClick={handleAdd} disabled={!canAdd}>
							{adding ? <Loader2 className="size-4 animate-spin" /> : <><Plus className="size-4" /> Add</>}
						</Button>
					</div>
				</div>
			</SheetContent>
		</Sheet>
	);
}
