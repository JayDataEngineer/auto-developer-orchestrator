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
} from "lucide-react";
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
}: {
	entry: FileEntry;
	depth: number;
	onSelect: (path: string) => void;
	selectedPath: string;
}) {
	if (entry.type === "dir") {
		return (
			<Collapsible className="group/tree">
				<CollapsibleTrigger asChild>
					<button
						className={cn(
							"flex w-full items-center gap-1 rounded-sm px-1 py-0.5 text-xs hover:bg-accent",
						)}
						style={{ paddingLeft: `${depth * 12 + 4}px` }}
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
				<CollapsibleContent>
					{entry.children?.map((child) => (
						<FileTreeItem
							key={child.path}
							entry={child}
							depth={depth + 1}
							onSelect={onSelect}
							selectedPath={selectedPath}
						/>
					))}
				</CollapsibleContent>
			</Collapsible>
		);
	}

	return (
		<button
			onClick={() => onSelect(entry.path)}
			className={cn(
				"flex w-full items-center gap-1 rounded-sm px-1 py-0.5 text-xs hover:bg-accent",
				selectedPath === entry.path && "bg-accent text-accent-foreground",
			)}
			style={{ paddingLeft: `${depth * 12 + 4 + 16}px` }}
		>
			{getFileIcon(entry.name)}
			<span className="truncate">{entry.name}</span>
		</button>
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
	const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
	const activeProject = usePuxStore((s) => s.activeProject);

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
				fileCache.delete(path);
				dirtyFiles.delete(path);
				setDirty(new Set(dirtyFiles));
				closeTab(path);
				refreshTree();
				return data.trashPath as string;
			}
			return "";
		},
		[activeProject, closeTab, refreshTree],
	);

	// Restore file from trash
	const restoreFile = useCallback(
		async (trashPath: string) => {
			if (!activeProject) return;
			const resp = await fetch("/api/pux/file/restore", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ project: activeProject, trashPath }),
			});
			if (resp.ok) {
				refreshTree();
			}
		},
		[activeProject, refreshTree],
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
		<div className="flex h-full flex-col">
			<div className="flex flex-1 overflow-hidden">
				{/* File tree sidebar */}
				<div className="w-48 shrink-0 overflow-y-auto border-r border-border py-1">
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
							/>
						))
					)}
				</div>
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
		</div>
	);
}
