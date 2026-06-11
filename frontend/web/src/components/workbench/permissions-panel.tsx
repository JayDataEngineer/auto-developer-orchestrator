import { useEffect, useState, useCallback, useMemo } from "react";
import { cn } from "@/lib/utils";
import {
	Collapsible,
	CollapsibleContent,
	CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
	Terminal,
	FileText,
	Users,
	Brain,
	Shield,
	ShieldCheck,
	ShieldAlert,
	ShieldOff,
	ChevronRight,
	Lock,
	Unlock,
	Plus,
	X,
} from "lucide-react";

// ── Types ──

interface ToolPermission {
	tool: string;
	level: "auto" | "confirm" | "deny";
	reason?: string;
	risk?: string;
}

interface BashCommandRule {
	id: string;
	pattern: string;
	level: "auto" | "confirm" | "deny";
}

interface SystemRulesSummary {
	hard_deny: string[];
	ask_rules: string[];
	read_only: string[];
}

// ── Tool metadata (descriptions + categories) ──

const TOOL_META: Record<string, { label: string; desc: string; category: string }> = {
	bash:           { label: "Bash",           desc: "Execute shell commands",             category: "execution" },
	file_read:      { label: "File Read",      desc: "Read file contents",                 category: "files" },
	file_write:     { label: "File Write",     desc: "Create or overwrite files",          category: "files" },
	file_edit:      { label: "File Edit",      desc: "Search-and-replace edits in files",  category: "files" },
	file_grep:      { label: "File Grep",      desc: "Search file contents by pattern",    category: "files" },
	file_glob:      { label: "File Glob",      desc: "Find files by name pattern",         category: "files" },
	delegate_to:    { label: "Delegate To",    desc: "Synchronous delegation to employee", category: "agent" },
	delegate_async: { label: "Delegate Async", desc: "Async delegation to employee",       category: "agent" },
	memory:         { label: "Memory",         desc: "Read/write persistent memory",       category: "memory" },
	create_plan:    { label: "Create Plan",    desc: "Create structured task plans",       category: "memory" },
};

const CATEGORIES = [
	{ id: "execution", label: "Execution",      icon: Terminal, color: "text-orange-500" },
	{ id: "files",     label: "File Operations", icon: FileText,  color: "text-blue-500" },
	{ id: "agent",     label: "Agent Control",   icon: Users,     color: "text-purple-500" },
	{ id: "memory",    label: "Memory & Plans",  icon: Brain,     color: "text-emerald-500" },
] as const;

const DEFAULT_CATEGORY = { id: "other", label: "Other Tools", icon: Shield, color: "text-muted-foreground" } as const;

// ── Permission level config ──

const LEVELS = [
	{ value: "auto",    label: "Auto",    icon: ShieldCheck, dot: "bg-green-500" },
	{ value: "confirm", label: "Confirm", icon: ShieldAlert, dot: "bg-yellow-500" },
	{ value: "deny",    label: "Block",   icon: ShieldOff,   dot: "bg-red-500" },
] as const;

type LevelValue = (typeof LEVELS)[number]["value"];

// ── Summary bar ──

function SummaryBar({ permissions }: { permissions: Record<string, ToolPermission> }) {
	const counts = useMemo(() => {
		let auto = 0, confirm = 0, deny = 0;
		for (const p of Object.values(permissions)) {
			if (p.level === "auto") auto++;
			else if (p.level === "confirm") confirm++;
			else if (p.level === "deny") deny++;
		}
		return { auto, confirm, deny, total: auto + confirm + deny };
	}, [permissions]);

	if (counts.total === 0) return null;

	return (
		<div className="flex items-center gap-2 text-[11px] text-muted-foreground">
			<span className="flex items-center gap-1"><span className="h-1.5 w-1.5 rounded-full bg-green-500" />{counts.auto} auto</span>
			<span className="flex items-center gap-1"><span className="h-1.5 w-1.5 rounded-full bg-yellow-500" />{counts.confirm} confirm</span>
			<span className="flex items-center gap-1"><span className="h-1.5 w-1.5 rounded-full bg-red-500" />{counts.deny} blocked</span>
		</div>
	);
}

// ── Segmented control ──

function LevelControl({
	value,
	onChange,
	disabled,
}: {
	value: LevelValue;
	onChange: (v: LevelValue) => void;
	disabled?: boolean;
}) {
	return (
		<div className="flex rounded-md border border-border overflow-hidden">
			{LEVELS.map((lv) => {
				const active = value === lv.value;
				return (
					<button
						key={lv.value}
						onClick={() => onChange(lv.value)}
						disabled={disabled}
						className={cn(
							"px-2 py-1 text-[10px] font-medium transition-colors border-r border-border last:border-r-0",
							"disabled:opacity-40 disabled:cursor-not-allowed",
							active
								? lv.value === "auto"
									? "bg-green-500/15 text-green-600 dark:text-green-400"
									: lv.value === "confirm"
										? "bg-yellow-500/15 text-yellow-600 dark:text-yellow-400"
										: "bg-red-500/15 text-red-600 dark:text-red-400"
								: "bg-transparent text-muted-foreground hover:bg-accent/50",
						)}
					>
						{lv.label}
					</button>
				);
			})}
		</div>
	);
}

// ── Bash sub-panel ──

function BashSubPanel() {
	const [bashRules, setBashRules] = useState<BashCommandRule[]>([]);
	const [systemRules, setSystemRules] = useState<SystemRulesSummary | null>(null);
	const [newPattern, setNewPattern] = useState("");
	const [newLevel, setNewLevel] = useState<LevelValue>("confirm");
	const [systemOpen, setSystemOpen] = useState(false);
	const [adding, setAdding] = useState(false);

	// Fetch both on mount
	useEffect(() => {
		fetch("/api/pux/bash-rules")
			.then((r) => r.ok ? r.json() : [])
			.then(setBashRules)
			.catch(() => {});
		fetch("/api/pux/bash-system-rules")
			.then((r) => r.ok ? r.json() : null)
			.then(setSystemRules)
			.catch(() => {});
	}, []);

	const addRule = useCallback(async () => {
		const pattern = newPattern.trim();
		if (!pattern) return;
		setAdding(true);
		try {
			const resp = await fetch("/api/pux/bash-rules", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ pattern, level: newLevel }),
			});
			if (resp.ok) {
				const rule = await resp.json();
				setBashRules((prev) => [...prev, rule]);
				setNewPattern("");
			}
		} catch { /* ignore */ } finally {
			setAdding(false);
		}
	}, [newPattern, newLevel]);

	const removeRule = useCallback(async (id: string) => {
		try {
			const resp = await fetch(`/api/pux/bash-rules/${id}`, { method: "DELETE" });
			if (resp.ok) {
				setBashRules((prev) => prev.filter((r) => r.id !== id));
			}
		} catch { /* ignore */ }
	}, []);

	const levelDot = (level: string) =>
		level === "auto" ? "bg-green-500" : level === "confirm" ? "bg-yellow-500" : "bg-red-500";
	const levelLabel = (level: string) =>
		level === "auto" ? "Allow" : level === "confirm" ? "Confirm" : "Block";

	return (
		<div className="ml-2 mr-1 mt-1 mb-1 border border-border rounded-md overflow-hidden">
			{/* System rules */}
			{systemRules && (
				<Collapsible open={systemOpen} onOpenChange={setSystemOpen}>
					<CollapsibleTrigger className="flex items-center gap-1.5 w-full px-2.5 py-1.5 hover:bg-accent/30 text-muted-foreground transition-colors">
						<ChevronRight className={cn("h-2.5 w-2.5 transition-transform", systemOpen && "rotate-90")} />
						<span className="text-[10px] font-medium uppercase tracking-wider">System Rules</span>
					</CollapsibleTrigger>
					<CollapsibleContent>
						<div className="px-2.5 pb-2 space-y-1.5">
							<SystemRuleGroup label="Always Blocked" items={systemRules.hard_deny} dot="bg-red-500" />
							<SystemRuleGroup label="Ask First" items={systemRules.ask_rules} dot="bg-yellow-500" />
							<SystemRuleGroup label="Auto-Approved" items={systemRules.read_only} dot="bg-green-500" />
						</div>
					</CollapsibleContent>
				</Collapsible>
			)}

			{/* User rules */}
			<div className="border-t border-border px-2.5 py-2">
				<div className="flex items-center justify-between mb-1.5">
					<span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Your Rules</span>
					{bashRules.length > 0 && (
						<span className="text-[9px] text-muted-foreground/60">{bashRules.length} rule{bashRules.length !== 1 ? "s" : ""}</span>
					)}
				</div>
				{bashRules.length === 0 ? (
					<p className="text-[10px] text-muted-foreground/60 py-1">No custom rules. Commands use system defaults.</p>
				) : (
					<div className="space-y-0.5">
						{bashRules.map((rule) => (
							<div key={rule.id} className="flex items-center gap-1.5 py-0.5 group">
								<span className={cn("h-1.5 w-1.5 rounded-full shrink-0", levelDot(rule.level))} />
								<code className="text-[10px] font-mono flex-1 truncate">{rule.pattern}</code>
								<span className={cn(
									"text-[9px] px-1.5 py-0.5 rounded font-medium",
									rule.level === "auto" ? "bg-green-500/10 text-green-600 dark:text-green-400"
									: rule.level === "confirm" ? "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400"
									: "bg-red-500/10 text-red-600 dark:text-red-400",
								)}>
									{levelLabel(rule.level)}
								</span>
								<button
									onClick={() => removeRule(rule.id)}
									className="h-4 w-4 flex items-center justify-center rounded opacity-0 group-hover:opacity-100 hover:bg-accent/50 transition-all"
								>
									<X className="h-2.5 w-2.5 text-muted-foreground" />
								</button>
							</div>
						))}
					</div>
				)}
			</div>

			{/* Add rule form */}
			<div className="border-t border-border px-2.5 py-2">
				<div className="flex items-center gap-1.5">
					<input
						type="text"
						value={newPattern}
						onChange={(e) => setNewPattern(e.target.value)}
						onKeyDown={(e) => e.key === "Enter" && addRule()}
						placeholder="e.g. docker*, rm, git push*"
						className="flex-1 min-w-0 px-2 py-1 text-[10px] font-mono bg-transparent border border-border rounded placeholder:text-muted-foreground/40 focus:outline-none focus:border-primary/50"
					/>
					<select
						value={newLevel}
						onChange={(e) => setNewLevel(e.target.value as LevelValue)}
						className="px-1.5 py-1 text-[10px] bg-transparent border border-border rounded focus:outline-none focus:border-primary/50"
					>
						<option value="auto">Allow</option>
						<option value="confirm">Confirm</option>
						<option value="deny">Block</option>
					</select>
					<button
						onClick={addRule}
						disabled={adding || !newPattern.trim()}
						className="h-6 w-6 flex items-center justify-center rounded border border-border hover:bg-accent/50 disabled:opacity-40 transition-colors"
					>
						<Plus className="h-3 w-3" />
					</button>
				</div>
			</div>
		</div>
	);
}

function SystemRuleGroup({ label, items, dot }: { label: string; items: string[]; dot: string }) {
	const MAX_SHOW = 6;
	if (items.length === 0) return null;
	const shown = items.slice(0, MAX_SHOW);
	const rest = items.length - MAX_SHOW;
	return (
		<div>
			<div className="flex items-center gap-1.5 mb-0.5">
				<span className={cn("h-1.5 w-1.5 rounded-full", dot)} />
				<span className="text-[10px] font-medium text-muted-foreground">{label}</span>
			</div>
			<div className="flex flex-wrap gap-1 pl-3">
				{shown.map((item, i) => (
					<span key={i} className="text-[9px] px-1.5 py-0.5 bg-secondary/50 rounded font-mono truncate max-w-[200px]" title={item}>
						{item}
					</span>
				))}
				{rest > 0 && (
					<span className="text-[9px] px-1.5 py-0.5 text-muted-foreground/60">+{rest} more</span>
				)}
			</div>
		</div>
	);
}

// ── Tool row ──

function ToolRow({
	perm,
	saving,
	onChange,
}: {
	perm: ToolPermission;
	saving: string | null;
	onChange: (tool: string, level: string) => void;
}) {
	const meta = TOOL_META[perm.tool];
	const riskColor = perm.risk === "high"
		? "bg-red-500"
		: perm.risk === "medium"
			? "bg-yellow-500"
			: "bg-green-500";

	return (
		<div className="flex items-center justify-between gap-2 px-2 py-1.5 rounded-md hover:bg-accent/30 transition-colors group">
			<div className="flex items-center gap-2 min-w-0 flex-1">
				<span className={cn("h-1.5 w-1.5 rounded-full shrink-0", riskColor)} />
				<div className="min-w-0">
					<span className="text-xs font-medium block truncate">
						{meta?.label ?? perm.tool}
					</span>
					{meta?.desc && (
						<span className="text-[10px] text-muted-foreground block truncate">
							{meta.desc}
						</span>
					)}
				</div>
			</div>
			<LevelControl
				value={perm.level as LevelValue}
				onChange={(v) => onChange(perm.tool, v)}
				disabled={saving === perm.tool}
			/>
		</div>
	);
}

// ── Bash tool row (expandable) ──

function BashToolRow({
	perm,
	saving,
	onChange,
}: {
	perm: ToolPermission;
	saving: string | null;
	onChange: (tool: string, level: string) => void;
}) {
	const [expanded, setExpanded] = useState(false);
	const riskColor = perm.risk === "high"
		? "bg-red-500"
		: perm.risk === "medium"
			? "bg-yellow-500"
			: "bg-green-500";

	return (
		<div>
			<div
				className="flex items-center justify-between gap-2 px-2 py-1.5 rounded-md hover:bg-accent/30 transition-colors cursor-pointer group"
				onClick={() => setExpanded(!expanded)}
			>
				<div className="flex items-center gap-2 min-w-0 flex-1">
					<ChevronRight className={cn("h-2.5 w-2.5 text-muted-foreground transition-transform shrink-0", expanded && "rotate-90")} />
					<span className={cn("h-1.5 w-1.5 rounded-full shrink-0", riskColor)} />
					<div className="min-w-0">
						<span className="text-xs font-medium block truncate">Bash</span>
						<span className="text-[10px] text-muted-foreground block truncate">Execute shell commands</span>
					</div>
				</div>
				<div onClick={(e) => e.stopPropagation()}>
					<LevelControl
						value={perm.level as LevelValue}
						onChange={(v) => onChange(perm.tool, v)}
						disabled={saving === perm.tool}
					/>
				</div>
			</div>
			{expanded && <BashSubPanel />}
		</div>
	);
}

// ── Category section ──

function CategorySection({
	category,
	tools,
	saving,
	onChange,
	defaultOpen = true,
}: {
	category: typeof CATEGORIES[number] | typeof DEFAULT_CATEGORY;
	tools: ToolPermission[];
	saving: string | null;
	onChange: (tool: string, level: string) => void;
	defaultOpen?: boolean;
}) {
	const [open, setOpen] = useState(defaultOpen);
	const Icon = category.icon;

	return (
		<Collapsible open={open} onOpenChange={setOpen}>
			<CollapsibleTrigger className="flex items-center gap-2 w-full py-1.5 px-1 hover:bg-accent/30 rounded transition-colors group">
				<ChevronRight className={cn(
					"h-3 w-3 text-muted-foreground transition-transform",
					open && "rotate-90",
				)} />
				<Icon className={cn("h-3.5 w-3.5", category.color)} />
				<span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
					{category.label}
				</span>
				<span className="text-[10px] text-muted-foreground/60 ml-auto">
					{tools.length}
				</span>
			</CollapsibleTrigger>
			<CollapsibleContent className="pl-3 pr-1">
				<div className="flex flex-col gap-0.5 mt-0.5">
					{tools.map((perm) =>
						perm.tool === "bash" ? (
							<BashToolRow key={perm.tool} perm={perm} saving={saving} onChange={onChange} />
						) : (
							<ToolRow key={perm.tool} perm={perm} saving={saving} onChange={onChange} />
						)
					)}
				</div>
			</CollapsibleContent>
		</Collapsible>
	);
}

// ── Bulk actions ──

function BulkActions({ onBulk }: { onBulk: (level: string, filter?: "high" | "medium") => void }) {
	return (
		<div className="flex items-center gap-1.5">
			<button
				onClick={() => onBulk("auto")}
				className="text-[10px] px-2 py-1 rounded border border-border text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
			>
				<Unlock className="h-3 w-3 inline mr-1" />
				Allow all
			</button>
			<button
				onClick={() => onBulk("confirm", "high")}
				className="text-[10px] px-2 py-1 rounded border border-border text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
			>
				<Lock className="h-3 w-3 inline mr-1" />
				Confirm risky
			</button>
			<button
				onClick={() => onBulk("deny")}
				className="text-[10px] px-2 py-1 rounded border border-border text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors"
			>
				<ShieldOff className="h-3 w-3 inline mr-1" />
				Block all
			</button>
		</div>
	);
}

// ── Main panel ──

export function PermissionsPanel({ embedded = false }: { embedded?: boolean }) {
	const [permissions, setPermissions] = useState<Record<string, ToolPermission>>({});
	const [loading, setLoading] = useState(true);
	const [saving, setSaving] = useState<string | null>(null);

	const fetchPermissions = useCallback(async () => {
		try {
			const resp = await fetch("/api/pux/tool-permissions");
			if (resp.ok) {
				setPermissions(await resp.json());
			}
		} catch {
			/* ignore */
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		fetchPermissions();
	}, [fetchPermissions]);

	const updatePermission = useCallback(async (tool: string, level: string) => {
		setSaving(tool);
		try {
			const resp = await fetch("/api/pux/tool-permissions", {
				method: "PUT",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ tool, level }),
			});
			if (resp.ok) {
				setPermissions((prev) => ({
					...prev,
					[tool]: { ...prev[tool], level: level as ToolPermission["level"] },
				}));
			}
		} catch {
			/* ignore */
		} finally {
			setSaving(null);
		}
	}, []);

	const bulkUpdate = useCallback(async (level: string, riskFilter?: "high" | "medium") => {
		const targets = Object.values(permissions).filter((p) => {
			if (riskFilter) return p.risk === riskFilter || p.risk === "high";
			return true;
		});
		// Optimistic update
		setPermissions((prev) => {
			const next = { ...prev };
			for (const t of targets) {
				next[t.tool] = { ...t, level: level as ToolPermission["level"] };
			}
			return next;
		});
		// Fire all updates
		await Promise.all(
			targets.map((t) =>
				fetch("/api/pux/tool-permissions", {
					method: "PUT",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ tool: t.tool, level }),
				}).catch(() => {}),
			),
		);
	}, [permissions]);

	// Group tools by category
	const grouped = useMemo(() => {
		const tools = Object.values(permissions).sort((a, b) => {
			const riskOrder: Record<string, number> = { high: 0, medium: 1, low: 2 };
			return (riskOrder[a.risk || "low"] ?? 2) - (riskOrder[b.risk || "low"] ?? 2);
		});

		const groups: Map<string, ToolPermission[]> = new Map();
		for (const cat of CATEGORIES) groups.set(cat.id, []);
		groups.set(DEFAULT_CATEGORY.id, []);

		for (const tool of tools) {
			const cat = TOOL_META[tool.tool]?.category ?? DEFAULT_CATEGORY.id;
			const arr = groups.get(cat);
			if (arr) arr.push(tool);
			else {
				groups.get(DEFAULT_CATEGORY.id)!.push(tool);
			}
		}
		return groups;
	}, [permissions]);

	if (loading) {
		return (
			<div className="flex items-center justify-center p-8 text-muted-foreground text-xs">
				Loading...
			</div>
		);
	}

	return (
		<div className={cn("flex flex-col gap-2", embedded ? "px-3 pb-4" : "p-4")}>
			{!embedded && (
				<div className="flex items-center gap-2 mb-2">
					<Shield className="h-4 w-4 text-muted-foreground" />
					<h3 className="text-sm font-semibold">Tool Permissions</h3>
				</div>
			)}

			{/* Summary + bulk actions */}
			<div className="flex items-center justify-between gap-2">
				<SummaryBar permissions={permissions} />
				<BulkActions onBulk={bulkUpdate} />
			</div>

			{/* Categorized tool list */}
			<div className="flex flex-col gap-1">
				{CATEGORIES.map((cat) => {
					const tools = grouped.get(cat.id) ?? [];
					if (tools.length === 0) return null;
					return (
						<CategorySection
							key={cat.id}
							category={cat}
							tools={tools}
							saving={saving}
							onChange={updatePermission}
						/>
					);
				})}
				{/* Uncategorised tools */}
				{(() => {
					const tools = grouped.get(DEFAULT_CATEGORY.id) ?? [];
					if (tools.length === 0) return null;
					return (
						<CategorySection
							category={DEFAULT_CATEGORY}
							tools={tools}
							saving={saving}
							onChange={updatePermission}
						/>
					);
				})()}
			</div>
		</div>
	);
}
