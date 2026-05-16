import { useState, useEffect, useCallback } from "react";
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

export function AddProjectDialog({
	open,
	onOpenChange,
}: AddProjectDialogProps) {
	const [currentPath, setCurrentPath] = useState("");
	const [entries, setEntries] = useState<BrowseEntry[]>([]);
	const [loading, setLoading] = useState(false);
	const [selectedPath, setSelectedPath] = useState("");
	const [projectName, setProjectName] = useState("");
	const [mode, setMode] = useState<"browse" | "manual">("browse");
	const [manualPath, setManualPath] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [adding, setAdding] = useState(false);
	const loadProjects = usePuxStore((s) => s.loadProjects);

	const fetchDir = useCallback(async (path: string) => {
		setLoading(true);
		setError(null);
		try {
			const resp = await browseFs(path);
			setCurrentPath(resp.path);
			setEntries(resp.entries);
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : "Failed to list directory");
		} finally {
			setLoading(false);
		}
	}, []);

	// Fetch on open
	useEffect(() => {
		if (open) {
			setSelectedPath("");
			setProjectName("");
			setError(null);
			setMode("browse");
			setManualPath("");
			fetchDir("");
		}
	}, [open, fetchDir]);

	const navigateTo = (path: string) => {
		setSelectedPath("");
		fetchDir(path);
	};

	const selectFolder = (path: string) => {
		setSelectedPath(path);
		const name = path.split("/").pop() || path;
		setProjectName(name);
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
		const path = mode === "manual" ? manualPath : selectedPath;
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
			setMode("browse");
		}
	};

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

				{mode === "browse" ? (
					<>
						{/* Breadcrumb */}
						<div className="flex items-center gap-1 px-4 py-2 text-xs border-b bg-muted/30 overflow-x-auto">
							<button
								type="button"
								className="shrink-0 text-muted-foreground hover:text-foreground"
								onClick={() => navigateTo("/home")}
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
							{/* Parent dir */}
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
				) : (
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
				)}

				{/* Footer */}
				<div className="border-t px-4 py-3 space-y-3">
					{/* Toggle manual/browse */}
					<button
						type="button"
						className="text-xs text-muted-foreground hover:text-foreground underline"
						onClick={() => setMode(mode === "browse" ? "manual" : "browse")}
					>
						{mode === "browse"
							? "Enter path manually..."
							: "Browse filesystem..."}
					</button>

					{error && (
						<div className="flex items-center gap-2 text-sm text-destructive">
							<AlertCircle className="size-4 shrink-0" />
							{error}
						</div>
					)}

					{/* Selected path + name */}
					{selectedPath && mode === "browse" && (
						<div className="space-y-2">
							<div className="flex items-center gap-2 text-sm text-muted-foreground bg-muted/30 rounded px-3 py-1.5">
								<FolderOpen className="size-4 shrink-0" />
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
								(mode === "browse" && !selectedPath) ||
								(mode === "manual" && !manualPath.trim())
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
