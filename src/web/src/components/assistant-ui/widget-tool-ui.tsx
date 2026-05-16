"use client";

import { useState, useCallback, Fragment } from "react";
import { makeAssistantToolUI } from "@assistant-ui/react";
import { relativeTime } from "@pux/shared";
import { cn } from "@/lib/utils";
import {
	CheckCircle,
	XCircle,
	Clock,
	Loader2,
	Trash2,
	Play,
	RefreshCw,
	AlertCircle,
} from "lucide-react";
import * as LucideIcons from "lucide-react";

// ── Widget types (mirrors Go core/widget.go) ──

interface WidgetColumn {
	key: string;
	label: string;
	type: "text" | "badge" | "boolean" | "date" | "mono" | "status";
	colorMap?: Record<string, string>;
}

interface WidgetAction {
	label: string;
	icon?: string;
	method: string;
	url: string;
	confirm?: string;
	variant?: string;
}

interface WidgetResult {
	type: "list" | "detail" | "confirm";
	title?: string;
	icon?: string;
	columns?: WidgetColumn[];
	rows?: Record<string, any>[];
	item?: Record<string, any>;
	actions?: WidgetAction[];
	message?: string;
	empty?: string;
}

// ── Helpers ──

function resolveIcon(name?: string): React.ElementType {
	if (!name) return Fragment;
	const key = name as keyof typeof LucideIcons;
	return (LucideIcons[key] as React.ElementType) || Fragment;
}

function resolveUrl(url: string, row: Record<string, any>): string {
	return url.replace(/\{(\w+)\}/g, (_, k) => row[k] ?? k);
}

function formatValue(val: any, col: WidgetColumn): React.ReactNode {
	if (val === undefined || val === null) return null;

	switch (col.type) {
		case "boolean":
			return val ? (
				<CheckCircle size={12} className="text-green-400" />
			) : (
				<XCircle size={12} className="text-muted-foreground/40" />
			);
		case "date": {
			const s = String(val);
			if (!s || s === "0001-01-01T00:00:00Z") return null;
			return relativeTime(s, { now: "just now", suffix: " ago" });
		}
		case "badge":
			return (
				<span className="shrink-0 rounded bg-muted px-1.5 text-[10px] uppercase tracking-wider text-muted-foreground">
					{String(val)}
				</span>
			);
		case "mono":
			return <span className="font-mono text-[11px]">{String(val)}</span>;
		case "status": {
			const cls = col.colorMap?.[String(val)] ?? "text-muted-foreground";
			return (
				<span className={cn("flex items-center gap-1 text-[11px]", cls)}>
					{val === "running" && <Loader2 size={10} className="animate-spin" />}
					{val === "error" && <AlertCircle size={10} />}
					{val === "idle" && <Clock size={10} />}
					{val === "success" && <CheckCircle size={10} />}
					{val === "done" && <CheckCircle size={10} />}
					{String(val)}
				</span>
			);
		}
		default:
			return <span className="truncate text-xs">{String(val)}</span>;
	}
}

// ── Action handler ──

function useWidgetActions(onRefresh?: () => void) {
	const [loading, setLoading] = useState<string | null>(null);

	const exec = useCallback(
		async (action: WidgetAction, row: Record<string, any>) => {
			if (action.confirm && !window.confirm(action.confirm)) return;
			const url = resolveUrl(action.url, row);
			setLoading(action.label);
			try {
				await fetch(url, { method: action.method });
			} catch {
				// ignore
			}
			setLoading(null);
			onRefresh?.();
		},
		[onRefresh],
	);

	return { exec, loading };
}

// ── Widget: List ──

function WidgetList({ widget }: { widget: WidgetResult }) {
	const [rows, setRows] = useState(widget.rows ?? []);
	const { exec, loading } = useWidgetActions(() => {
		// Refresh not wired for inline — action fires and widget shows result
	});

	const Icon = resolveIcon(widget.icon);

	return (
		<div className="my-2 space-y-1.5">
			<div className="flex items-center justify-between">
				<div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
					<Icon size={12} />
					<span>{widget.title}</span>
				</div>
			</div>
			{rows.length === 0 ? (
				<div className="rounded-lg border border-dashed border-border px-3 py-3 text-center text-xs text-muted-foreground">
					{widget.empty ?? "No items"}
				</div>
			) : (
				rows.map((row, i) => (
					<div
						key={i}
						className="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-2"
					>
						<div className="min-w-0 flex-1 space-y-0.5">
							{(widget.columns ?? []).map((col) => (
								<div key={col.key} className="flex items-center gap-2">
									{col.type !== "badge" && col.type !== "boolean" && (
										<span className="text-[10px] text-muted-foreground">
											{col.label}:
										</span>
									)}
									{formatValue(row[col.key], col)}
								</div>
							))}
						</div>
						{(widget.actions ?? []).length > 0 && (
							<div className="flex shrink-0 items-center gap-0.5">
								{widget.actions!.map((action, j) => {
									const ActionIcon = resolveIcon(action.icon);
									const isLoading = loading === action.label;
									const isDestructive = action.variant === "destructive";
									return (
										<button
											key={j}
											onClick={() => exec(action, row)}
											disabled={isLoading}
											className={cn(
												"rounded-md p-1.5 disabled:opacity-30",
												isDestructive
													? "text-muted-foreground/40 hover:bg-destructive/10 hover:text-destructive"
													: "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
											)}
											title={action.label}
										>
											{isLoading ? (
												<Loader2 size={14} className="animate-spin" />
											) : (
												<ActionIcon size={14} />
											)}
										</button>
									);
								})}
							</div>
						)}
					</div>
				))
			)}
		</div>
	);
}

// ── Widget: Detail ──

function WidgetDetail({ widget }: { widget: WidgetResult }) {
	const item = widget.item ?? {};
	const { exec, loading } = useWidgetActions();
	const Icon = resolveIcon(widget.icon);

	return (
		<div className="my-2 rounded-lg border border-border bg-card px-3 py-2">
			<div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground mb-2">
				<Icon size={12} />
				<span>{widget.title}</span>
			</div>
			<div className="space-y-1">
				{(widget.columns ?? []).map((col) => (
					<div key={col.key} className="flex items-center gap-2 text-xs">
						<span className="w-24 shrink-0 text-muted-foreground">
							{col.label}
						</span>
						{formatValue(item[col.key], col)}
					</div>
				))}
			</div>
			{(widget.actions ?? []).length > 0 && (
				<div className="mt-2 flex items-center gap-1 border-t border-border pt-2">
					{widget.actions!.map((action, j) => {
						const ActionIcon = resolveIcon(action.icon);
						const isLoading = loading === action.label;
						const isDestructive = action.variant === "destructive";
						return (
							<button
								key={j}
								onClick={() => exec(action, item)}
								disabled={isLoading}
								className={cn(
									"inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] disabled:opacity-30",
									isDestructive
										? "text-destructive hover:bg-destructive/10"
										: "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
								)}
							>
								{isLoading ? (
									<Loader2 size={11} className="animate-spin" />
								) : (
									<ActionIcon size={11} />
								)}
								{action.label}
							</button>
						);
					})}
				</div>
			)}
		</div>
	);
}

// ── Widget: Confirm ──

function WidgetConfirm({ widget }: { widget: WidgetResult }) {
	const Icon = resolveIcon(widget.icon);
	return (
		<div className="my-2 flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-xs text-muted-foreground">
			<Icon size={12} className="text-green-400" />
			{widget.message}
		</div>
	);
}

// ── Generic renderer ──

function WidgetRenderer({
	args,
	result,
	artifact,
	status,
}: {
	args: Record<string, any>;
	result?: any;
	artifact?: any;
	status?: { type: string };
}) {
	// Widget comes via the artifact pipeline
	const wrapper = artifact as { type: string; widget?: WidgetResult } | undefined;
	const widget = wrapper?.type === "widget" ? wrapper.widget : undefined;

	if (!widget) {
		// Still loading or no widget data — show minimal state
		if (status?.type === "running") {
			return (
				<div className="my-2 flex items-center gap-2 rounded-lg border border-border px-3 py-2 text-xs text-muted-foreground">
					<Loader2 size={12} className="animate-spin" />
					{args.operation
						? `${String(args.operation)}...`
						: args.action
							? `${String(args.action)}...`
							: "Loading..."}
				</div>
			);
		}
		return null;
	}

	switch (widget.type) {
		case "list":
			return <WidgetList widget={widget} />;
		case "detail":
			return <WidgetDetail widget={widget} />;
		case "confirm":
			return <WidgetConfirm widget={widget} />;
		default:
			return null;
	}
}

// ── Register for all widget-capable tools ──

const WIDGET_TOOLS = [
	"manage_schedule",
	"manage_worker",
	"manage_profile",
	"todo",
];

export const WidgetToolUIs = WIDGET_TOOLS.map((toolName) =>
	makeAssistantToolUI({
		toolName,
		render: WidgetRenderer,
	}),
);
