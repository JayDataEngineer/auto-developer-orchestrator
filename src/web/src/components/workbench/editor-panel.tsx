import { useEffect, useState, useCallback } from "react";
import Editor from "@monaco-editor/react";
import {
	FileIcon,
	FolderIcon,
	FolderOpenIcon,
	ChevronRightIcon,
	ChevronDownIcon,
	FileCodeIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface FileEntry {
	name: string;
	type: "file" | "dir";
	children?: FileEntry[];
	path: string;
}

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
	const [expanded, setExpanded] = useState(true);

	if (entry.type === "dir") {
		return (
			<div>
				<button
					onClick={() => setExpanded(!expanded)}
					className={cn(
						"flex w-full items-center gap-1 rounded-sm px-1 py-0.5 text-xs hover:bg-accent",
					)}
					style={{ paddingLeft: `${depth * 12 + 4}px` }}
				>
					{expanded ? (
						<ChevronDownIcon size={12} className="shrink-0" />
					) : (
						<ChevronRightIcon size={12} className="shrink-0" />
					)}
					{expanded ? (
						<FolderOpenIcon size={14} className="shrink-0 text-yellow-500" />
					) : (
						<FolderIcon size={14} className="shrink-0 text-yellow-500" />
					)}
					<span className="truncate">{entry.name}</span>
				</button>
				{expanded &&
					entry.children?.map((child) => (
						<FileTreeItem
							key={child.path}
							entry={child}
							depth={depth + 1}
							onSelect={onSelect}
							selectedPath={selectedPath}
						/>
					))}
			</div>
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

// Cache file contents so switching files preserves content
const fileCache = new Map<string, string>();

export function EditorPanel() {
	const [files, setFiles] = useState<FileEntry[]>([]);
	const [selectedPath, setSelectedPath] = useState("");
	const [content, setContent] = useState("");

	// Load project file tree
	useEffect(() => {
		fetch("/api/pux/files")
			.then((r) => (r.ok ? r.json() : []))
			.then((data) => {
				if (Array.isArray(data) && data.length > 0) {
					setFiles(data);
					const firstFile = findFirstFile(data);
					if (firstFile) selectFile(firstFile.path);
				}
			})
			.catch(() => {});
	}, []);

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

	const selectFile = useCallback((path: string) => {
		setSelectedPath(path);
		// Check cache first
		if (fileCache.has(path)) {
			setContent(fileCache.get(path)!);
			return;
		}
		fetch(`/api/pux/file?path=${encodeURIComponent(path)}`)
			.then((r) => (r.ok ? r.text() : ""))
			.then((text) => {
				fileCache.set(path, text);
				setContent(text);
			})
			.catch(() => setContent(""));
	}, []);

	const filename = selectedPath.split("/").pop() || "";

	return (
		<div className="flex h-full">
			{/* File tree sidebar */}
			<div className="w-48 shrink-0 overflow-y-auto border-r border-border py-1">
				{files.length === 0 ? (
					<div className="flex h-full items-center justify-center">
						<span className="px-2 text-center text-[11px] text-muted-foreground">
							No project loaded
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
			{/* Monaco editor — uses path prop for proper Model management */}
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
