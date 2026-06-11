import { useEffect, useState, useCallback } from "react";
import {
	Users,
	RotateCcw,
	Crown,
	Pencil,
	Trash2,
	Plus,
	RefreshCw,
	Loader2,
	X,
	MessageSquare,
	ChevronDown,
	ChevronRight,
	FolderOpen,
} from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { PromptDetail } from "./prompt-panel";
import { ConfigPanel } from "./config-panel/config-panel";
import { FieldRenderer } from "./config-panel/field-renderer";
import type { FieldConfig } from "./config-panel/types";

// ── Types ──

interface WorkerFormData {
	name: string;
	hint: string;
	persona: string;
	capabilities: string[];
	hooks: string[];
	model: string;
	maxRounds: number | undefined;
	temperature: number | undefined;
	sandbox: string;
	delegatesTo: string[];
}

interface Worker {
	name: string;
	hint: string;
	persona: string;
	capabilities: string[];
	imports?: string[];
	hooks?: string[];
	model: string;
	max_rounds: number;
	temperature: number;
	sandbox: string;
	delegates_to: string[];
	isDefault: boolean;
	isModified: boolean;
	isOrg?: boolean;
	source: string;
	sourceDescription?: string;
	sourcePath?: string;
}

interface WorkerGroup {
	source: string;
	description: string;
	workers: Worker[];
	collapsed: boolean;
}

// ── Constants ──

const emptyForm: WorkerFormData = {
	name: "",
	hint: "",
	persona: "",
	capabilities: [],
	hooks: [],
	model: "",
	maxRounds: undefined,
	temperature: undefined,
	sandbox: "",
	delegatesTo: [],
};

const workerFields: FieldConfig<WorkerFormData>[] = [
	{ key: "name", type: "text", label: "Name", placeholder: "data-collector", required: true },
	{ key: "hint", type: "text", label: "Hint", placeholder: "One-line CTO-facing description" },
	{ key: "persona", type: "textarea", label: "Persona", placeholder: "You are a data collection specialist...", rows: 3, required: true },
	{
		key: "capabilities", type: "multiselect", label: "Capabilities",
		options: [
			{ value: "browser", label: "Browser" },
			{ value: "code", label: "Code" },
			{ value: "desktop", label: "Desktop" },
			{ value: "research", label: "Research" },
			{ value: "shell", label: "Shell" },
			{ value: "vision", label: "Vision" },
		],
	},
	{ key: "model", type: "model", label: "Model" },
	{ key: "maxRounds", type: "number", label: "Max Rounds", min: 1, max: 100 },
	{ key: "temperature", type: "number", label: "Temperature", min: 0, max: 2 },
	{
		key: "sandbox", type: "select", label: "Sandbox",
		options: [
			{ value: "", label: "Default" },
			{ value: "isolated", label: "Isolated" },
			{ value: "bridged", label: "Bridged" },
			{ value: "native", label: "Native" },
		],
	},
	{
		key: "hooks", type: "multiselect", label: "Hooks",
		options: [
			{ value: "file_checkpoint", label: "File Checkpoint" },
			{ value: "git_checkpoint", label: "Git Checkpoint" },
			{ value: "raise_browser", label: "Raise Browser" },
			{ value: "journal_checkpoint", label: "Journal Checkpoint" },
		],
	},
	{ key: "delegatesTo", type: "workers", label: "Can Delegate To" },
];

// ── Helpers ──

function workerToForm(w: Worker): WorkerFormData {
	return {
		name: w.name || "",
		hint: w.hint || "",
		persona: w.persona || "",
		capabilities: w.capabilities || [],
		hooks: w.hooks || [],
		model: w.model || "",
		maxRounds: w.max_rounds || undefined,
		temperature: w.temperature || undefined,
		sandbox: w.sandbox || "",
		delegatesTo: w.delegates_to || [],
	};
}

function buildBody(form: WorkerFormData): Record<string, any> {
	const body: Record<string, any> = {
		name: form.name.trim(),
		persona: form.persona.trim(),
	};
	if (form.hint.trim()) body.hint = form.hint.trim();
	if (form.capabilities.length > 0) body.capabilities = form.capabilities;
	if (form.model) body.model = form.model;
	if (form.maxRounds) body.maxRounds = form.maxRounds;
	if (form.temperature != null) body.temperature = form.temperature;
	if (form.sandbox) body.sandbox = form.sandbox;
	if (form.delegatesTo.length > 0) body.delegatesTo = form.delegatesTo;
	if (form.hooks.length > 0) body.hooks = form.hooks;
	return body;
}

function groupWorkers(workers: Worker[]): WorkerGroup[] {
	const groups = new Map<string, WorkerGroup>();

	// Ensure kernel group is first
	for (const w of workers) {
		const source = w.source || "kernel";
		if (!groups.has(source)) {
			groups.set(source, {
				source,
				description: source === "kernel" ? "Built-in workers" : (w.sourceDescription || ""),
				workers: [],
				collapsed: source !== "kernel",
			});
		}
		groups.get(source)!.workers.push(w);
	}

	// Kernel first, then alphabetical
	const sorted = Array.from(groups.values());
	sorted.sort((a, b) => {
		if (a.source === "kernel") return -1;
		if (b.source === "kernel") return 1;
		return a.source.localeCompare(b.source);
	});

	return sorted;
}

// ── CTO Card ──

function CTOCard({ onClick }: { onClick: () => void }) {
	return (
		<Card className="px-3 py-2 cursor-pointer hover:bg-accent/30 transition-colors" onClick={onClick}>
			<div className="flex items-center gap-2">
				<div className="flex size-6 items-center justify-center rounded bg-amber-500/20">
					<Crown className="size-3.5 text-amber-500" />
				</div>
				<div className="min-w-0 flex-1">
					<div className="flex items-center gap-2">
						<span className="truncate text-sm font-medium">CTO (Pux)</span>
						<Badge variant="outline" className="text-[9px]">orchestrator</Badge>
					</div>
					<p className="mt-0.5 truncate text-[11px] text-muted-foreground">
						Edit system prompt sections
					</p>
				</div>
				<Button variant="ghost" size="icon" className="size-7 shrink-0" title="Edit prompt">
					<Pencil size={14} />
				</Button>
			</div>
		</Card>
	);
}

// ── Worker Card ──

function WorkerCard({
	worker,
	onEdit,
	onDelete,
	onRevert,
}: {
	worker: Worker;
	onEdit: (w: Worker) => void;
	onDelete: (name: string) => void;
	onRevert: (w: Worker) => void;
}) {
	const isOrg = worker.isOrg;

	return (
		<Card className="px-3 py-2">
			<div className="flex items-start gap-2">
				<div className="min-w-0 flex-1">
					<div className="flex items-center gap-2">
						<span className="truncate text-sm font-medium">{worker.name}</span>
						{worker.isDefault && !isOrg && (
							<Badge variant="outline" className="text-[9px]">default</Badge>
						)}
						{(worker.capabilities || worker.imports || []).map((c: string) => (
							<Badge key={c} variant="secondary" className="text-[9px]">{c}</Badge>
						))}
						{(worker.hooks || []).map((h: string) => (
							<Badge key={h} variant="outline" className="text-[9px] text-amber-600 border-amber-600/30">{h}</Badge>
						))}
						{worker.isModified && (
							<Badge variant="destructive" className="text-[9px]">modified</Badge>
						)}
					</div>
					{(worker.hint || worker.persona) && (
						<p className="mt-0.5 truncate text-[11px] text-muted-foreground">
							{worker.hint || worker.persona}
						</p>
					)}
				</div>
				<div className="flex shrink-0 items-center gap-0.5">
					{worker.isModified && (
						<Button
							variant="ghost"
							size="icon"
							className="size-7"
							onClick={() => onRevert(worker)}
							title="Revert to default"
						>
							<RotateCcw size={14} />
						</Button>
					)}
					<Button
						variant="ghost"
						size="icon"
						className="size-7"
						onClick={() => onEdit(worker)}
						title="Edit"
					>
						<Pencil size={14} />
					</Button>
					{!worker.isDefault && !isOrg && (
						<Button
							variant="ghost"
							size="icon"
							className="size-7 text-muted-foreground/40 hover:text-destructive"
							onClick={() => onDelete(worker.name)}
							title="Delete"
						>
							<Trash2 size={14} />
						</Button>
					)}
				</div>
			</div>
		</Card>
	);
}

// ── Group Section ──

function GroupSection({
	group,
	onToggle,
	children,
}: {
	group: WorkerGroup;
	onToggle: () => void;
	children: React.ReactNode;
}) {
	const isKernel = group.source === "kernel";

	return (
		<div>
			<button
				className="flex w-full items-center gap-1.5 px-3 py-1.5 hover:bg-accent/30 transition-colors"
				onClick={onToggle}
			>
				{group.collapsed ? (
					<ChevronRight className="size-3 text-muted-foreground" />
				) : (
					<ChevronDown className="size-3 text-muted-foreground" />
				)}
				{isKernel ? (
					<Users className="size-3 text-muted-foreground" />
				) : (
					<FolderOpen className="size-3 text-muted-foreground" />
				)}
				<span className="text-xs font-medium">
					{isKernel ? "Kernel" : group.source}
				</span>
				{group.description && !isKernel && (
					<span className="truncate text-[10px] text-muted-foreground ml-1">
						{group.description}
					</span>
				)}
				<Badge variant="secondary" className="text-[9px] ml-auto">
					{group.workers.length}
				</Badge>
			</button>
			{!group.collapsed && (
				<div className="space-y-1 px-2 pb-2">
					{children}
				</div>
			)}
		</div>
	);
}

// ── Edit/Create Form ──

function WorkerForm({
	initial,
	itemId,
	onReset,
	onSubmit,
	onCancel,
	onAskAI,
	submitting,
}: {
	initial: WorkerFormData;
	itemId?: string;
	onReset?: () => void;
	onSubmit: (data: WorkerFormData) => void;
	onCancel: () => void;
	onAskAI: (data: WorkerFormData) => void;
	submitting: boolean;
}) {
	const [form, setForm] = useState<WorkerFormData>(initial);
	const update = (key: string, val: any) => setForm((f) => ({ ...f, [key]: val }));
	const isEdit = !!itemId;

	const requiredKeys = workerFields
		.filter((f) => ("required" in f ? f.required : false))
		.map((f) => f.key);
	const canSubmit = requiredKeys.every((k) => {
		const val = (form as any)[k];
		if (typeof val === "string") return val.trim().length > 0;
		return val !== undefined && val !== null;
	});

	return (
		<Card className="mx-2 mt-2 p-3">
			<div className="flex items-center justify-between mb-2.5">
				<span className="text-xs font-medium">{isEdit ? "Edit" : "New"} Worker</span>
				<Button variant="ghost" size="icon" className="size-6" onClick={onCancel}>
					<X size={12} />
				</Button>
			</div>
			<div className="space-y-2.5">
				{workerFields.map((field) => (
					<FieldRenderer
						key={field.key as string}
						field={field}
						value={(form as any)[field.key]}
						onChange={(val: any) => update(field.key as string, val)}
					/>
				))}
			</div>
			<div className="flex items-center gap-2 mt-3">
				<Button
					onClick={() => onSubmit(form)}
					disabled={!canSubmit || submitting}
					className="flex-1 h-7 text-xs"
				>
					{submitting ? "Saving..." : isEdit ? "Save" : "Create"}
				</Button>
				{isEdit && onReset && (
					<Button
						variant="outline"
						onClick={onReset}
						className="h-7 text-xs gap-1"
						title="Reset to default"
					>
						<RotateCcw size={11} />
						Reset
					</Button>
				)}
				<Button
					variant="outline"
					onClick={() => onAskAI(form)}
					className="h-7 text-xs gap-1"
				>
					<MessageSquare size={11} />
					Ask AI
				</Button>
			</div>
		</Card>
	);
}

// ── Panel ──

export function WorkersPanel() {
	const [view, setView] = useState<"list" | "cto">("list");
	const [workers, setWorkers] = useState<Worker[]>([]);
	const [groups, setGroups] = useState<WorkerGroup[]>([]);
	const [loading, setLoading] = useState(true);
	const [submitting, setSubmitting] = useState(false);
	const [showForm, setShowForm] = useState(false);
	const [editItem, setEditItem] = useState<Worker | null>(null);

	const fetchWorkers = useCallback(async () => {
		try {
			const resp = await fetch("/api/workers/");
			if (resp.ok) {
				const data = await resp.json();
				const parsed: Worker[] = data.workers || [];
				setWorkers(parsed);
				setGroups(groupWorkers(parsed));
			}
		} catch { /* ignore */ } finally { setLoading(false); }
	}, []);

	useEffect(() => {
		fetchWorkers();
		const iv = setInterval(fetchWorkers, 10000);
		return () => clearInterval(iv);
	}, [fetchWorkers]);

	const toggleGroup = (source: string) => {
		setGroups((prev) =>
			prev.map((g) =>
				g.source === source ? { ...g, collapsed: !g.collapsed } : g
			)
		);
	};

	const submitCreate = async (form: WorkerFormData) => {
		setSubmitting(true);
		try {
			await fetch("/api/workers/", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(buildBody(form)),
			});
			setShowForm(false);
			fetchWorkers();
		} finally { setSubmitting(false); }
	};

	const submitEdit = async (form: WorkerFormData) => {
		if (!editItem) return;
		setSubmitting(true);
		try {
			const body = buildBody(form);
			// Include source for org workers so backend routes correctly
			if (editItem.isOrg) {
				body.source = editItem.source;
			}
			await fetch(`/api/workers/${editItem.name}`, {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(body),
			});
			setEditItem(null);
			fetchWorkers();
		} finally { setSubmitting(false); }
	};

	const deleteWorker = async (name: string) => {
		await fetch(`/api/workers/${name}`, { method: "DELETE" });
		fetchWorkers();
	};

	const revertWorker = async (worker: Worker) => {
		const params = worker.isOrg ? `?source=${encodeURIComponent(worker.source)}` : "";
		await fetch(`/api/workers/${worker.name}/revert${params}`, { method: "POST" });
		fetchWorkers();
	};

	const askAI = (form: WorkerFormData) => {
		const isEdit = !!editItem;
		const parts: string[] = [isEdit ? `Update the worker "${editItem?.name}" for me.` : "Create a new worker for me."];
		if (form.name.trim()) parts.push(`Name: ${form.name.trim()}`);
		if (form.hint.trim()) parts.push(`Hint: ${form.hint.trim()}`);
		if (form.persona.trim()) parts.push(`Persona: ${form.persona.trim()}`);
		if (form.capabilities.length > 0) parts.push(`Capabilities: ${form.capabilities.join(", ")}`);
		if (form.model) parts.push(`Model: ${form.model}`);
		if (form.maxRounds) parts.push(`Max rounds: ${form.maxRounds}`);
		if (form.temperature != null) parts.push(`Temperature: ${form.temperature}`);
		if (form.hooks.length > 0) parts.push(`Hooks: ${form.hooks.join(", ")}`);
		if (form.delegatesTo.length > 0) parts.push(`Can delegate to: ${form.delegatesTo.join(", ")}`);
		dispatchEvent(new CustomEvent("pux:send-message", { detail: { text: parts.join(" ") } }));
		setShowForm(false);
		setEditItem(null);
	};

	if (view === "cto") {
		return <PromptDetail onBack={() => setView("list")} />;
	}

	const formVisible = showForm || editItem;
	const totalWorkers = workers.length;

	return (
		<div className="flex h-full flex-col">
			{/* CTO card */}
			<div className="p-2 pb-0">
				<CTOCard onClick={() => setView("cto")} />
			</div>

			{/* Header */}
			<div className="flex items-center justify-between border-b border-border px-3 py-1.5">
				<span className="text-xs text-muted-foreground">
					{totalWorkers} worker{totalWorkers !== 1 ? "s" : ""} in {groups.length} group{groups.length !== 1 ? "s" : ""}
				</span>
				<div className="flex items-center gap-1">
					<Button
						variant="ghost"
						size="icon"
						className="size-6"
						onClick={() => { setEditItem(null); setShowForm((v) => !v); }}
						title="New worker"
					>
						<Plus size={12} />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						className="size-6"
						onClick={fetchWorkers}
						title="Refresh"
					>
						<RefreshCw size={12} />
					</Button>
				</div>
			</div>

			{/* Create form */}
			{showForm && !editItem && (
				<WorkerForm
					initial={emptyForm}
					onSubmit={submitCreate}
					onCancel={() => setShowForm(false)}
					onAskAI={askAI}
					submitting={submitting}
				/>
			)}

			{/* Edit form */}
			{editItem && (
				<WorkerForm
					initial={workerToForm(editItem)}
					itemId={editItem.name}
					onReset={() => { revertWorker(editItem); setEditItem(null); }}
					onSubmit={submitEdit}
					onCancel={() => setEditItem(null)}
					onAskAI={askAI}
					submitting={submitting}
				/>
			)}

			{/* Grouped worker list */}
			{!formVisible && loading ? (
				<div className="flex flex-1 items-center justify-center">
					<Loader2 className="size-5 animate-spin text-muted-foreground" />
				</div>
			) : !formVisible && workers.length === 0 ? (
				<div className="flex flex-1 flex-col items-center justify-center gap-3">
					<Users className="size-8 text-muted-foreground/50" />
					<span className="text-xs text-muted-foreground">No workers configured</span>
					<Button size="sm" onClick={() => setShowForm(true)} className="gap-1.5">
						<Plus className="size-3" /> New Worker
					</Button>
				</div>
			) : (
				<div className="flex-1 overflow-y-auto">
					{groups.map((group) => (
						<GroupSection
							key={group.source}
							group={group}
							onToggle={() => toggleGroup(group.source)}
						>
							{group.workers.map((worker) => (
								<WorkerCard
									key={`${worker.source}-${worker.name}`}
									worker={worker}
									onEdit={setEditItem}
									onDelete={deleteWorker}
									onRevert={revertWorker}
								/>
							))}
						</GroupSection>
					))}
				</div>
			)}
		</div>
	);
}
