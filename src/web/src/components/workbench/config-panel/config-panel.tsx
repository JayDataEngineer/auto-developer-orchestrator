import { useEffect, useState, useCallback } from "react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import {
	Loader2,
	Plus,
	RefreshCw,
	Pencil,
	Trash2,
	MessageSquare,
	X,
	Play,
	Pause,
} from "lucide-react";
import { FieldRenderer } from "./field-renderer";
import type { ConfigPanelProps, ActionButton } from "./types";

// ── Config Form (create + edit) ──

function ConfigForm<T>({
	fields,
	initial,
	itemId,
	onSubmit,
	onCancel,
	onAskAI,
	submitting,
	visible,
}: {
	fields: ConfigPanelProps<T>["fields"];
	initial: T;
	itemId?: string;
	onSubmit: (data: T) => void;
	onCancel: () => void;
	onAskAI: (data: T) => void;
	submitting: boolean;
	visible?: (key: string, form: T) => boolean;
}) {
	const [form, setForm] = useState<T>(initial);
	const update = (key: string, val: any) => setForm((f) => ({ ...f, [key]: val }));
	const isEdit = !!itemId;

	const requiredKeys = fields
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
				<span className="text-xs font-medium">{isEdit ? "Edit" : "New"}</span>
				<Button variant="ghost" size="icon" className="size-6" onClick={onCancel}>
					<X size={12} />
				</Button>
			</div>
			<div className="space-y-2.5">
				{fields.map((field) => {
					if (visible && !visible(field.key as string, form)) return null;
					return (
						<FieldRenderer
							key={field.key as string}
							field={field}
							value={(form as any)[field.key]}
							onChange={(val: any) => update(field.key as string, val)}
						/>
					);
				})}
			</div>
			<div className="flex items-center gap-2 mt-3">
				<Button
					onClick={() => onSubmit(form)}
					disabled={!canSubmit || submitting}
					className="flex-1 h-7 text-xs"
				>
					{submitting ? "Saving..." : isEdit ? "Save" : "Create"}
				</Button>
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

// ── Item Card ──

function ItemCard({
	item,
	itemDef,
	onEdit,
	onDelete,
	onAction,
	enableToggle,
	extraActions,
}: {
	item: any;
	itemDef: ConfigPanelProps<any>["itemDef"];
	onEdit: (item: any) => void;
	onDelete: (id: string) => void;
	onAction?: () => void;
	enableToggle?: ConfigPanelProps<any>["enableToggle"];
	extraActions?: (item: any) => ActionButton[];
}) {
	const id = itemDef.id(item);
	const label = itemDef.label(item);
	const desc = itemDef.description?.(item);
	const badges = itemDef.badges?.(item);
	const statusCfg = itemDef.status?.(item);

	return (
		<Card className="px-3 py-2">
			<div className="flex items-start gap-2">
				{statusCfg && (
					<div className="mt-0.5 shrink-0">
						<span className={cn(statusCfg.color, statusCfg.spin && "animate-spin")}>
							{statusCfg.icon}
						</span>
					</div>
				)}
				<div className="min-w-0 flex-1">
					<div className="flex items-center gap-2">
						<span className="truncate text-sm font-medium">{label}</span>
						{badges?.map((b, i) => (
							<Badge key={i} variant={b.variant ?? "secondary"} className="text-[9px]">
								{b.text}
							</Badge>
						))}
					</div>
					{desc && (
						<p className="mt-0.5 truncate text-[11px] text-muted-foreground">{desc}</p>
					)}
				</div>
				<div className="flex shrink-0 items-center gap-0.5">
					{extraActions?.(item).map((action, j) => (
						<Button
							key={j}
							variant="ghost"
							size="icon"
							className="size-7"
							onClick={async () => { await fetch(action.url, { method: action.method }); onAction?.(); }}
							disabled={action.disabled}
							title={action.label}
						>
							{action.icon}
						</Button>
					))}
					<Button
						variant="ghost"
						size="icon"
						className="size-7"
						onClick={() => onEdit(item)}
						title="Edit"
					>
						<Pencil size={14} />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						className="size-7 text-muted-foreground/40 hover:text-destructive"
						onClick={() => onDelete(id)}
						title="Delete"
					>
						<Trash2 size={14} />
					</Button>
				</div>
			</div>
		</Card>
	);
}

// ── Main ConfigPanel ──

export function ConfigPanel<T>({
	fetchUrl,
	createUrl,
	updateUrl,
	deleteUrl,
	fields,
	emptyForm,
	formToBody,
	responseToForm,
	itemDef,
	title,
	emptyMessage,
	emptyIcon,
	enableToggle,
	extraActions,
	askAITemplate,
	visible,
}: ConfigPanelProps<T>) {
	const [items, setItems] = useState<any[]>([]);
	const [loading, setLoading] = useState(true);
	const [submitting, setSubmitting] = useState(false);
	const [showForm, setShowForm] = useState(false);
	const [editItem, setEditItem] = useState<any | null>(null);

	const fetchItems = useCallback(async () => {
		try {
			const resp = await fetch(fetchUrl);
			if (resp.ok) {
				const data = await resp.json();
				const parsed = Array.isArray(data) ? data : data.jobs ?? data.workers ?? [];
				setItems((prev) => {
					if (prev.length === parsed.length && prev.every((item, i) => item === parsed[i])) return prev;
					return parsed;
				});
			}
		} catch { /* ignore */ } finally { setLoading(false); }
	}, [fetchUrl]);

	useEffect(() => {
		fetchItems();
		const iv = setInterval(fetchItems, 10000);
		return () => clearInterval(iv);
	}, [fetchItems]);

	const submitCreate = async (form: T) => {
		setSubmitting(true);
		try {
			await fetch(createUrl, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(formToBody(form)),
			});
			setShowForm(false);
			fetchItems();
		} finally { setSubmitting(false); }
	};

	const submitEdit = async (form: T) => {
		if (!editItem) return;
		setSubmitting(true);
		try {
			await fetch(updateUrl(itemDef.id(editItem)), {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(formToBody(form)),
			});
			setEditItem(null);
			fetchItems();
		} finally { setSubmitting(false); }
	};

	const deleteItem = async (id: string) => {
		await fetch(deleteUrl(id), { method: "DELETE" });
		fetchItems();
	};

	const askAI = (form: T) => {
		const text = askAITemplate(form, !!editItem, editItem);
		dispatchEvent(new CustomEvent("pux:send-message", { detail: { text } }));
		setShowForm(false);
		setEditItem(null);
	};

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center">
				<Loader2 className="size-5 animate-spin text-muted-foreground" />
			</div>
		);
	}

	const formVisible = showForm || editItem;

	return (
		<div className="flex h-full flex-col">
			{/* Header */}
			<div className="flex items-center justify-between border-b border-border px-3 py-1.5">
				<span className="text-xs text-muted-foreground">
					{items.length} item{items.length !== 1 ? "s" : ""}
				</span>
				<div className="flex items-center gap-1">
					<Button
						variant="ghost"
						size="icon"
						className={cn("size-6", showForm && "bg-accent")}
						onClick={() => { setEditItem(null); setShowForm((v) => !v); }}
						title="New"
					>
						<Plus size={12} />
					</Button>
					<Button
						variant="ghost"
						size="icon"
						className="size-6"
						onClick={fetchItems}
						title="Refresh"
					>
						<RefreshCw size={12} />
					</Button>
				</div>
			</div>

			{/* Create form */}
			{showForm && !editItem && (
				<ConfigForm<T>
					fields={fields}
					initial={emptyForm}
					onSubmit={submitCreate}
					onCancel={() => setShowForm(false)}
					onAskAI={askAI}
					submitting={submitting}
					visible={visible}
				/>
			)}

			{/* Edit form */}
			{editItem && (
				<ConfigForm<T>
					fields={fields}
					initial={responseToForm(editItem)}
					itemId={itemDef.id(editItem)}
					onSubmit={submitEdit}
					onCancel={() => setEditItem(null)}
					onAskAI={askAI}
					submitting={submitting}
					visible={visible}
				/>
			)}

			{/* Items list or empty state */}
			{!formVisible && items.length === 0 ? (
				<div className="flex flex-1 flex-col items-center justify-center gap-3">
					<div className="text-muted-foreground/50">{emptyIcon}</div>
					<span className="text-xs text-muted-foreground">{emptyMessage}</span>
					<Button size="sm" onClick={() => setShowForm(true)} className="gap-1.5">
						<Plus className="size-3" /> New
					</Button>
				</div>
			) : (
				<div className="flex-1 space-y-1.5 overflow-y-auto p-2">
					{items.map((item) => (
						<ItemCard
							key={itemDef.id(item)}
							item={item}
							itemDef={itemDef}
							onEdit={setEditItem}
							onDelete={deleteItem}
							onAction={fetchItems}
							enableToggle={enableToggle}
							extraActions={extraActions}
						/>
					))}
				</div>
			)}
		</div>
	);
}
