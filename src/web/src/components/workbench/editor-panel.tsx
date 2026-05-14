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

// Skip noisy dirs/files
const SKIP_NAMES = new Set([
	"node_modules",
	".git",
	"__pycache__",
	".next",
	"dist",
	".cache",
	"vendor",
]);

// ── Sandbox API ──

async function getSandboxId(): Promise<string | null> {
	try {
		const resp = await fetch("/api/sandboxes");
		if (!resp.ok) return null;
		const data = await resp.json();
		const sandboxes = Array.isArray(data) ? data : [];
		if (sandboxes.length === 0) return null;
		return sandboxes[0].id || sandboxes[0];
	} catch {
		return null;
	}
}

async function listDir(
	sandboxId: string,
	path: string,
): Promise<Array<{ name: string; size: number; isDir: boolean }>> {
	try {
		const resp = await fetch(
			`/api/sandbox/${sandboxId}/files/list?path=${encodeURIComponent(path)}`,
		);
		if (!resp.ok) return [];
		const data = await resp.json();
		return Array.isArray(data.entries) ? data.entries : [];
	} catch {
		return [];
	}
}

async function buildTree(
	sandboxId: string,
	path: string,
	depth = 0,
): Promise<FileEntry[]> {
	if (depth > 6) return [];
	const entries = await listDir(sandboxId, path);

	const result: FileEntry[] = [];
	for (const e of entries) {
		if (e.name.startsWith(".") || SKIP_NAMES.has(e.name)) continue;

		const fullPath = `${path}/${e.name}`;
		if (e.isDir) {
			const children = await buildTree(sandboxId, fullPath, depth + 1);
			result.push({
				name: e.name,
				type: "dir",
				children,
				path: fullPath,
			});
		} else {
			result.push({ name: e.name, type: "file", path: fullPath });
		}
	}
	return result;
}

async function readFile(sandboxId: string, path: string): Promise<string> {
	try {
		const resp = await fetch(
			`/api/sandbox/${sandboxId}/files/download?path=${encodeURIComponent(path)}`,
		);
		if (!resp.ok) return "";
		return await resp.text();
	} catch {
		return "";
	}
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
	const [sandboxId, setSandboxId] = useState<string | null>(null);
	const [loading, setLoading] = useState(true);
	const activeProject = usePuxStore((s) => s.activeProject);

	// Resolve sandbox ID
	useEffect(() => {
		fileCache.clear();
		setFiles([]);
		setSelectedPath("");
		setContent("");
		setLoading(true);

		getSandboxId()
			.then((id) => {
				setSandboxId(id);
				if (!id) {
					setLoading(false);
					return;
				}
				// Build file tree from sandbox workspace
				return buildTree(id, "/sandbox/workspace").then((tree) => {
					setFiles(tree);
					// Auto-select first file
					const first = findFirstFile(tree);
					if (first) loadFile(id, first.path);
				});
			})
			.catch(() => {})
			.finally(() => setLoading(false));
	}, [activeProject]);

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

	const loadFile = useCallback(
		(sbId: string | null, path: string) => {
			if (!sbId) return;
			setSelectedPath(path);
			if (fileCache.has(path)) {
				setContent(fileCache.get(path)!);
				return;
			}
			readFile(sbId, path).then((text) => {
				fileCache.set(path, text);
				setContent(text);
			});
		},
		[],
	);

	const selectFile = useCallback(
		(path: string) => loadFile(sandboxId, path),
		[sandboxId, loadFile],
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

	if (!sandboxId) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-2">
				<FileIcon className="size-8 text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">
					No sandbox running
				</span>
				<span className="text-[11px] text-muted-foreground/70">
					Start a sandbox to browse project files
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
							Empty workspace
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
