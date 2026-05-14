import { useEffect, useState, useCallback } from "react";
import Editor from "@monaco-editor/react";
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

// ── File Tree Item (using shadcn Collapsible) ──

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
			<Collapsible defaultOpen className="group/tree">
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

// ── Cache ──

const fileCache = new Map<string, string>();

// ── Panel ──

export function EditorPanel() {
	const [files, setFiles] = useState<FileEntry[]>([]);
	const [selectedPath, setSelectedPath] = useState("");
	const [content, setContent] = useState("");
	const [loading, setLoading] = useState(true);
	const activeProject = usePuxStore((s) => s.activeProject);

	// Load file tree from local filesystem via backend
	useEffect(() => {
		if (!activeProject) {
			setFiles([]);
			setSelectedPath("");
			setContent("");
			setLoading(false);
			return;
		}

		fileCache.clear();
		setLoading(true);

		fetch(`/api/pux/files?project=${encodeURIComponent(activeProject)}`)
			.then((r) => (r.ok ? r.json() : []))
			.then((data) => {
				const tree = Array.isArray(data) ? data : [];
				setFiles(tree);
				if (tree.length > 0) {
					const first = findFirstFile(tree);
					if (first) selectFile(first.path);
				}
			})
			.catch(() => setFiles([]))
			.finally(() => setLoading(false));
	}, [activeProject]);

	const selectFile = useCallback(
		(path: string) => {
			setSelectedPath(path);
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
					setContent(text);
				})
				.catch(() => setContent(""));
		},
		[activeProject],
	);

	const filename = selectedPath.split("/").pop() || "";

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
		<div className="flex h-full">
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
							onSelect={selectFile}
							selectedPath={selectedPath}
						/>
					))
				)}
			</div>
			{/* Monaco editor */}
			<div className="flex-1">
				{selectedPath ? (
					<Editor
						key={selectedPath}
						path={selectedPath}
						defaultLanguage={getLang(filename)}
						defaultValue={content}
						theme="vs-dark"
						options={{
							minimap: { enabled: false },
							fontSize: 13,
							lineNumbers: "on",
							scrollBeyondLastLine: false,
							wordWrap: "on",
							padding: { top: 8 },
							readOnly: true,
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
	);
}
