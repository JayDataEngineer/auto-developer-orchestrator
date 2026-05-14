import { useEffect, useState, useCallback, useRef } from "react";
import Editor, { type OnMount } from "@monaco-editor/react";
import type { editor } from "monaco-editor";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
	FileIcon,
	FolderIcon,
	FolderOpenIcon,
	ChevronRightIcon,
	FileCodeIcon,
	XIcon,
	Trash2Icon,
	PlusIcon,
	PanelLeftCloseIcon,
	PanelLeftOpenIcon,
} from "lucide-react";
import {
	ContextMenu,
	ContextMenuTrigger,
	ContextMenuContent,
	ContextMenuItem,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import { usePuxStore } from "@/lib/pux-store";

// ── Types ──

interface FileEntry {
	name: string;
	type: "file" | "dir";
	children?: FileEntry[];
	path: string;
}

interface Tab {
	path: string;
	name: string;
}

// ── Helpers ──

const LANG_MAP: Record<string, string> = {
	ts: "typescript",
	tsx: "typescript",
	js: "javascript",
	jsx: "javascript",
	go: "go",
	py: "python",
	rs: "rust",
	md: "markdown",
	json: "json",
	yaml: "yaml",
	yml: "yaml",
	css: "css",
	html: "html",
	sql: "sql",
	sh: "shell",
	toml: "toml",
};

const LANG_LABELS: Record<string, string> = {
	typescript: "TypeScript",
	javascript: "JavaScript",
	go: "Go",
	python: "Python",
	rust: "Rust",
	markdown: "Markdown",
	json: "JSON",
	yaml: "YAML",
	css: "CSS",
	html: "HTML",
	sql: "SQL",
	shell: "Shell",
	toml: "TOML",
};

function getLang(filename: string): string {
	const ext = filename.split(".").pop()?.toLowerCase() || "";
	return LANG_MAP[ext] || "plaintext";
}

function getFileIcon(name: string) {
	const ext = name.split(".").pop()?.toLowerCase();
	if (["ts", "tsx", "js", "jsx", "go", "rs", "py"].includes(ext || ""))
		return <FileCodeIcon size={14} className="shrink-0 text-blue-400" />;
	return <FileIcon size={14} className="shrink-0 text-muted-foreground" />;
}

function findFirstFile(entries: FileEntry[]): FileEntry | null {
	for (const e of entries) {
		if (e.type === "file") return e;
		if (e.children) {
			const f = findFirstFile(e.children);
			if (f) return f;
		}
	}
	return null;
}

function pathSegments(path: string): string[] {
	return path.split("/").filter(Boolean);
}

// ── File Tree Item ──

function FileTreeItem({
	entry,
	depth,
	onSelect,
	selectedPath,
	onDelete,
	onCreateFile,
	onMoveFile,
}: {
	entry: FileEntry;
	depth: number;
	onSelect: (path: string) => void;
	selectedPath: string;
	onDelete: (path: string) => void;
	onCreateFile: (dirPath: string) => void;
	onMoveFile: (from: string, to: string) => void;
}) {
	const [dragOver, setDragOver] = useState(false);

	if (entry.type === "dir") {
		return (
			<Collapsible className="group/tree">
				<div className="flex w-full items-center">
					<CollapsibleTrigger asChild>
						<button
							className={cn(
								"flex flex-1 items-center gap-1 rounded-sm px-1 py-0.5 text-xs hover:bg-accent",
								dragOver && "bg-accent ring-1 ring-primary",
							)}
							style={{ paddingLeft: `${depth * 12 + 4}px` }}
							onDragOver={(e) => {
								e.preventDefault();
								setDragOver(true);
							}}
							onDragLeave={() => setDragOver(false)}
							onDrop={(e) => {
								e.preventDefault();
								setDragOver(false);
								const fromPath = e.dataTransfer.getData("text/plain");
								if (fromPath && fromPath !== entry.path) {
									const name = fromPath.split("/").pop() || fromPath;
									onMoveFile(fromPath, entry.path + "/" + name);
								}
							}}
						>
							<ChevronRightIcon
								size={12}
								className="shrink-0 transition-transform group-data-[state=open]/tree:rotate-90"
							/>
							<FolderOpenIcon
								size={14}
								className="shrink-0 text-yellow-500 group-data-[state=closed]/tree:hidden"
							/>
							<FolderIcon
								size={14}
								className="shrink-0 text-yellow-500 group-data-[state=open]/tree:hidden"
							/>
							<span className="truncate">{entry.name}</span>
						</button>
					</CollapsibleTrigger>
					<ContextMenu>
						<ContextMenuTrigger asChild>
							<button className="shrink-0 rounded p-0.5 text-muted-foreground opacity-0 hover:bg-accent group-hover/tree:opacity-100">
								<svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor"><circle cx="5" cy="2" r="1"/><circle cx="5" cy="5" r="1"/><circle cx="5" cy="8" r="1"/></svg>
							</button>
						</ContextMenuTrigger>
						<ContextMenuContent>
							<ContextMenuItem onClick={() => onCreateFile(entry.path)}>
								<PlusIcon size={12} className="mr-1.5" />
								New File
							</ContextMenuItem>
							<ContextMenuItem
								onClick={() => onDelete(entry.path)}
								className="text-red-500 focus:text-red-500"
							>
								<Trash2Icon size={12} className="mr-1.5" />
								Delete
							</ContextMenuItem>
						</ContextMenuContent>
					</ContextMenu>
				</div>
				<CollapsibleContent>
					{entry.children?.map((child) => (
						<FileTreeItem
							key={child.path}
							entry={child}
							depth={depth + 1}
							onSelect={onSelect}
							selectedPath={selectedPath}
							onDelete={onDelete}
							onCreateFile={onCreateFile}
							onMoveFile={onMoveFile}
						/>
					))}
				</CollapsibleContent>
			</Collapsible>
		);
	}

	return (
		<ContextMenu>
			<ContextMenuTrigger asChild>
				<button
					onClick={() => onSelect(entry.path)}
					draggable
					onDragStart={(e) => {
						e.dataTransfer.setData("text/plain", entry.path);
						e.dataTransfer.effectAllowed = "move";
					}}
					className={cn(
						"flex w-full items-center gap-1 rounded-sm px-1 py-0.5 text-xs hover:bg-accent",
						selectedPath === entry.path && "bg-accent text-accent-foreground",
					)}
					style={{ paddingLeft: `${depth * 12 + 4 + 16}px` }}
				>
					{getFileIcon(entry.name)}
					<span className="truncate">{entry.name}</span>
				</button>
			</ContextMenuTrigger>
			<ContextMenuContent>
				<ContextMenuItem
					onClick={() => onDelete(entry.path)}
					className="text-red-500 focus:text-red-500"
				>
					<Trash2Icon size={12} className="mr-1.5" />
					Delete
				</ContextMenuItem>
			</ContextMenuContent>
		</ContextMenu>
	);
}

// ── Cache & dirty tracking ──

const fileCache = new Map<string, string>();
const dirtyFiles = new Set<string>();

// ── Panel ──

export function EditorPanel() {
	const [files, setFiles] = useState<FileEntry[]>([]);
	const [tabs, setTabs] = useState<Tab[]>([]);
	const [activePath, setActivePath] = useState("");
	const [content, setContent] = useState("");
	const [loading, setLoading] = useState(true);
	const [cursorPos, setCursorPos] = useState({ ln: 1, col: 1 });
	const [dirty, setDirty] = useState<Set<string>>(new Set());
	const [saving, setSaving] = useState(false);
	const [deleteToast, setDeleteToast] = useState<{
		name: string;
		trashPath: string;
	} | null>(null);
	const deleteTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
	const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
	const [newFileName, setNewFileName] = useState("");
	const [isCreating, setIsCreating] = useState(false);
	const newFileInputRef = useRef<HTMLInputElement>(null);
	const activeProject = usePuxStore((s) => s.activeProject);
	const [showFileTree, setShowFileTree] = useState(true);

	// Refresh file tree
	const refreshTree = useCallback(() => {
		if (!activeProject) return;
		fetch(`/api/pux/files?project=${encodeURIComponent(activeProject)}`)
			.then((r) => (r.ok ? r.json() : []))
			.then((data) => {
				const tree = Array.isArray(data) ? data : [];
				setFiles(tree);
			})
			.catch(() => {});
	}, [activeProject]);

	// Load file tree
	useEffect(() => {
		if (!activeProject) {
			setFiles([]);
			setTabs([]);
			setActivePath("");
			setContent("");
			setLoading(false);
			return;
		}

		fileCache.clear();
		dirtyFiles.clear();
		setDirty(new Set());
		setTabs([]);
		setActivePath("");
		setLoading(true);

		fetch(`/api/pux/files?project=${encodeURIComponent(activeProject)}`)
			.then((r) => (r.ok ? r.json() : []))
			.then((data) => {
				const tree = Array.isArray(data) ? data : [];
				setFiles(tree);
				if (tree.length > 0) {
					const first = findFirstFile(tree);
					if (first) openFile(first.path);
				}
			})
			.catch(() => setFiles([]))
			.finally(() => setLoading(false));
	}, [activeProject]);

	const openFile = useCallback(
		(path: string) => {
			const name = path.split("/").pop() || path;

			// Add tab if not already open
			setTabs((prev) => {
				if (prev.some((t) => t.path === path)) return prev;
				return [...prev, { path, name }];
			});

			setActivePath(path);

			// Load content
			if (fileCache.has(path)) {
				setContent(fileCache.get(path)!);
				return;
			}
			if (!activeProject) return;
			fetch(
				`/api/pux/file?project=${encodeURIComponent(activeProject)}&path=${encodeURIComponent(path)}`,
			)
				.then((r) => (r.ok ? r.text() : ""))
				.then((text) => {
					fileCache.set(path, text);
					// Only set if this path is still active
					setActivePath((current) => {
						if (current === path) setContent(text);
						return current;
					});
				})
				.catch(() => {});
		},
		[activeProject],
	);

	const closeTab = useCallback(
		(path: string) => {
			setTabs((prev) => {
				const next = prev.filter((t) => t.path !== path);
				// If closing the active tab, switch to the last remaining
				if (path === activePath && next.length > 0) {
					const switchTo = next[next.length - 1];
					openFile(switchTo.path);
				} else if (next.length === 0) {
					setActivePath("");
					setContent("");
				}
				return next;
			});
		},
		[activePath, openFile],
	);

	// Save current file
	const saveFile = useCallback(async () => {
		if (!activeProject || !activePath || !editorRef.current) return;
		const editorContent = editorRef.current.getValue();
		setSaving(true);
		try {
			const resp = await fetch("/api/pux/file", {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					project: activeProject,
					path: activePath,
					content: editorContent,
				}),
			});
			if (resp.ok) {
				fileCache.set(activePath, editorContent);
				dirtyFiles.delete(activePath);
				setDirty(new Set(dirtyFiles));
			}
		} finally {
			setSaving(false);
		}
	}, [activeProject, activePath]);

	// Delete file (moves to .pux/trash for undo)
	const deleteFile = useCallback(
		async (path: string) => {
			if (!activeProject) return;
			const resp = await fetch(
				`/api/pux/file?project=${encodeURIComponent(activeProject)}&path=${encodeURIComponent(path)}`,
				{ method: "DELETE" },
			);
			if (resp.ok) {
				const data = await resp.json();
				const trashPath = data.trashPath as string;
				fileCache.delete(path);
				dirtyFiles.delete(path);
				setDirty(new Set(dirtyFiles));
				closeTab(path);
				refreshTree();
				// Show undo toast
				if (deleteTimerRef.current) clearTimeout(deleteTimerRef.current);
				setDeleteToast({ name: path.split("/").pop() || path, trashPath });
				deleteTimerRef.current = setTimeout(() => setDeleteToast(null), 5000);
			}
		},
		[activeProject, closeTab, refreshTree],
	);

	// Restore file from trash (undo delete)
	const undoDelete = useCallback(async () => {
		if (!deleteToast || !activeProject) return;
		if (deleteTimerRef.current) clearTimeout(deleteTimerRef.current);
		const { trashPath } = deleteToast;
		setDeleteToast(null);
		const resp = await fetch("/api/pux/file/restore", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ project: activeProject, trashPath }),
		});
		if (resp.ok) {
			refreshTree();
		}
	}, [activeProject, deleteToast, refreshTree]);

	// Create new file
	const createFile = useCallback(
		async (name: string) => {
			if (!activeProject || !name.trim()) return;
			const resp = await fetch("/api/pux/file/create", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ project: activeProject, path: name.trim() }),
			});
			if (resp.ok) {
				await new Promise<void>((resolve) => {
					refreshTree();
					// Give tree a tick to re-render, then open the new file
					setTimeout(() => {
						openFile(name.trim());
						resolve();
					}, 100);
				});
			} else if (resp.status === 409) {
				// File exists — just open it
				openFile(name.trim());
			}
		},
		[activeProject, refreshTree, openFile],
	);

	// Move file (drag-and-drop)
	const moveFile = useCallback(
		async (from: string, to: string) => {
			if (!activeProject) return;
			const resp = await fetch("/api/pux/file/move", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ project: activeProject, from, to }),
			});
			if (resp.ok) {
				fileCache.delete(from);
				dirtyFiles.delete(from);
				setDirty(new Set(dirtyFiles));
				closeTab(from);
				refreshTree();
			}
		},
		[activeProject, closeTab, refreshTree],
	);

	const handleEditorMount: OnMount = (editor) => {
		editorRef.current = editor;
		editor.onDidChangeCursorPosition((e) => {
			setCursorPos({ ln: e.position.lineNumber, col: e.position.column });
		});
		editor.onDidChangeModelContent(() => {
			if (!activePath) return;
			const current = editor.getValue();
			const original = fileCache.get(activePath);
			if (current !== original) {
				dirtyFiles.add(activePath);
			} else {
				dirtyFiles.delete(activePath);
			}
			setDirty(new Set(dirtyFiles));
		});
		// Ctrl+S / Cmd+S to save
		editor.addCommand(
			// Monaco.KeyMod.CtrlCmd | Monaco.KeyCode.KeyS
			2048 | 49, // KeyMod.CtrlCmd=2048, KeyCode.KeyS=49
			() => saveFile(),
		);
	};

	const filename = activePath.split("/").pop() || "";
	const lang = getLang(filename);

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center">
				<span className="text-xs text-muted-foreground">
					Loading project files...
				</span>
			</div>
		);
	}

	if (!activeProject) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-2">
				<FileIcon className="size-8 text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">
					No project selected
				</span>
				<span className="text-[11px] text-muted-foreground/70">
					Select a project from the sidebar
				</span>
			</div>
		);
	}

	return (
		<div className="relative flex h-full flex-col">
			<div className="flex flex-1 overflow-hidden">
				{/* File tree sidebar */}
				{showFileTree ? (
				<div className="flex w-48 shrink-0 flex-col border-r border-border">
					{/* Tree header */}
					<div className="flex h-7 items-center justify-between border-b border-border px-2">
						<div className="flex items-center gap-1">
							<span className="text-[11px] font-medium text-muted-foreground">Files</span>
							<button
								onClick={() => {
									setIsCreating(true);
									setNewFileName("");
									setTimeout(() => newFileInputRef.current?.focus(), 0);
								}}
								className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
								title="New file"
							>
								<PlusIcon size={12} />
							</button>
						</div>
						<button
							onClick={() => setShowFileTree(false)}
							className="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
							title="Hide file tree"
						>
							<PanelLeftCloseIcon size={12} />
						</button>
					</div>
					{/* Inline new file input */}
					{isCreating && (
						<div className="flex items-center gap-1 border-b border-border px-1 py-0.5">
							<FileCodeIcon size={12} className="shrink-0 text-blue-400" />
							<input
								ref={newFileInputRef}
								value={newFileName}
								onChange={(e) => setNewFileName(e.target.value)}
								onKeyDown={(e) => {
									if (e.key === "Enter" && newFileName.trim()) {
										createFile(newFileName);
										setIsCreating(false);
										setNewFileName("");
									} else if (e.key === "Escape") {
										setIsCreating(false);
										setNewFileName("");
									}
								}}
								onBlur={() => {
									if (newFileName.trim()) {
										createFile(newFileName);
									}
									setIsCreating(false);
									setNewFileName("");
								}}
								placeholder="filename.ts"
								className="flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
							/>
						</div>
					)}
					<div className="flex-1 overflow-y-auto py-1">
					{files.length === 0 ? (
						<div className="flex h-full items-center justify-center">
							<span className="px-2 text-center text-[11px] text-muted-foreground">
								Empty project
							</span>
						</div>
					) : (
						files.map((entry) => (
							<FileTreeItem
								key={entry.path}
								entry={entry}
								depth={0}
								onSelect={openFile}
								selectedPath={activePath}
								onDelete={(path) => deleteFile(path)}
								onCreateFile={(dir) => {
									setIsCreating(true);
									setNewFileName(dir ? dir + "/" : "");
									setTimeout(() => {
										newFileInputRef.current?.focus();
										const len = newFileInputRef.current?.value.length || 0;
										newFileInputRef.current?.setSelectionRange(len, len);
									}, 0);
								}}
								onMoveFile={moveFile}
							/>
						))
					)}
					</div>
				</div>
				) : (
				<button
					onClick={() => setShowFileTree(true)}
					className="flex w-7 shrink-0 items-center justify-center border-r border-border text-muted-foreground hover:bg-accent hover:text-accent-foreground"
					title="Show file tree"
				>
					<PanelLeftOpenIcon size={14} />
				</button>
				)}
				{/* Editor area */}
				<div className="flex min-w-0 flex-1 flex-col">
					{/* Tab bar */}
					{tabs.length > 0 && (
						<div className="flex h-8 items-end overflow-x-auto border-b border-border bg-muted/30">
							{tabs.map((tab) => (
								<div
									key={tab.path}
									className={cn(
										"group/tab flex h-7 shrink-0 cursor-pointer items-center gap-1 border-r border-border px-2 text-xs transition-colors",
										tab.path === activePath
											? "border-b-2 border-b-primary bg-background text-foreground"
											: "text-muted-foreground hover:bg-accent/50",
									)}
									onClick={() => openFile(tab.path)}
								>
									<span className="max-w-28 truncate">
										{dirty.has(tab.path) && (
											<span className="mr-0.5 text-[10px] text-orange-400">●</span>
										)}
										{tab.name}
									</span>
									<button
										onClick={(e) => {
											e.stopPropagation();
											closeTab(tab.path);
										}}
										className="rounded p-0.5 opacity-0 hover:bg-accent group-hover/tab:opacity-100"
									>
										<XIcon size={10} />
									</button>
								</div>
							))}
						</div>
					)}
					{/* Breadcrumb */}
					{activePath && (
						<div className="flex h-6 items-center gap-0.5 border-b border-border bg-muted/20 px-2 text-[11px] text-muted-foreground">
							{pathSegments(activePath).map((seg, i, arr) => (
								<span key={i} className="flex items-center gap-0.5">
									{i > 0 && (
										<span className="text-muted-foreground/50">
											/
										</span>
									)}
									<span
										className={cn(
											i === arr.length - 1 &&
												"text-foreground",
										)}
									>
										{seg}
									</span>
								</span>
							))}
						</div>
					)}
					{/* Monaco editor */}
					<div className="flex-1">
						{activePath ? (
							<Editor
								key={activePath}
								path={activePath}
								defaultLanguage={lang}
								defaultValue={content}
								theme="vs-dark"
								onMount={handleEditorMount}
								options={{
									minimap: { enabled: false },
									fontSize: 13,
									lineNumbers: "on",
									scrollBeyondLastLine: false,
									wordWrap: "on",
									padding: { top: 4 },
								}}
							/>
						) : (
							<div className="flex h-full items-center justify-center">
								<span className="text-xs text-muted-foreground">
									Select a file to view
								</span>
							</div>
						)}
					</div>
				</div>
			</div>
			{/* Status bar */}
			{activePath && (
				<div className="flex h-6 items-center justify-between border-t border-border bg-muted/30 px-3 text-[11px] text-muted-foreground">
					<span>
						{LANG_LABELS[lang] || lang}
					</span>
					<span className="flex items-center gap-3">
						{dirty.has(activePath) && (
							<span className="text-orange-400">
								{saving ? "Saving..." : "Modified"}
							</span>
						)}
						<span>
							Ln {cursorPos.ln}, Col {cursorPos.col}
						</span>
					</span>
				</div>
			)}
			{/* Undo toast */}
			{deleteToast && (
				<div className="absolute bottom-8 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2 rounded-md border border-border bg-popover px-3 py-1.5 text-xs text-popover-foreground shadow-lg animate-in fade-in slide-in-from-bottom-2">
					<span>
						Deleted <span className="font-medium">{deleteToast.name}</span>
					</span>
					<button
						onClick={undoDelete}
						className="font-medium text-primary hover:underline"
					>
						Undo
					</button>
				</div>
			)}
		</div>
	);
}
