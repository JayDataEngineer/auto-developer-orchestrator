import { useEffect, useState, useCallback } from "react";
import { relativeTime } from "@pux/shared";
import { cn } from "@/lib/utils";
import { usePuxStore } from "@/lib/pux-store";
import {
	Play,
	Pause,
	Clock,
	AlertTriangle,
	CheckCircle,
	XCircle,
	Loader2,
	RefreshCw,
	Calendar,
	Trash2,
	Plus,
	MessageSquare,
	X,
	Pencil,
} from "lucide-react";

// ── Shared input style ──
const inputSm = "rounded-md border border-border bg-transparent px-2 py-1.5 text-xs outline-none focus:border-ring w-full";

// ── Types ──

interface Job {
	id: string;
	name: string;
	description?: string;
	project?: string;
	message?: string;
	model?: string;
	scheduleType: "cron" | "every" | "at" | "manual";
	cronExpr?: string;
	everySeconds?: number;
	enabled: boolean;
	status: "idle" | "running" | "error" | "disabled";
	lastRunAt?: string;
	lastRunStatus?: string;
	lastError?: string;
	nextRunAt?: string;
	consecutiveErrors: number;
	durationMs?: number;
}

type ScheduleType = "manual" | "cron" | "every";

interface FormData {
	name: string;
	message: string;
	scheduleType: ScheduleType;
	cronExpr: string;
	everyMinutes: string;
	model: string;
	description: string;
}

const emptyForm: FormData = {
	name: "",
	message: "",
	scheduleType: "manual",
	cronExpr: "",
	everyMinutes: "30",
	model: "",
	description: "",
};

// ── Helpers ──

const STATUS_CFG: Record<string, { icon: typeof Clock; color: string; spin?: boolean }> = {
	idle: { icon: Clock, color: "text-muted-foreground" },
	running: { icon: Loader2, color: "text-blue-400", spin: true },
	error: { icon: AlertTriangle, color: "text-red-400" },
	disabled: { icon: Pause, color: "text-muted-foreground" },
};

const RESULT_ICON: Record<string, React.ReactNode> = {
	success: <CheckCircle size={12} className="text-green-400" />,
	error: <XCircle size={12} className="text-red-400" />,
};

function formatDuration(ms: number): string {
	if (ms < 1000) return `${ms}ms`;
	const s = Math.floor(ms / 1000);
	if (s < 60) return `${s}s`;
	const m = Math.floor(s / 60);
	return `${m}m ${s % 60}s`;
}

function formatSchedule(job: Job): string {
	if (job.scheduleType === "cron" && job.cronExpr) return job.cronExpr;
	if (job.scheduleType === "every" && job.everySeconds) {
		const m = Math.floor(job.everySeconds / 60);
		return m > 0 ? `every ${m}m` : `every ${job.everySeconds}s`;
	}
	if (job.scheduleType === "at") return "one-shot";
	return "manual";
}

function jobToForm(job: Job): FormData {
	return {
		name: job.name || "",
		message: job.message || "",
		scheduleType: job.scheduleType === "at" ? "manual" : (job.scheduleType as ScheduleType),
		cronExpr: job.cronExpr || "",
		everyMinutes: job.everySeconds ? String(Math.floor(job.everySeconds / 60) || 30) : "30",
		model: job.model || "",
		description: job.description || "",
	};
}

// ── Job Form (create + edit) ──

function JobForm({
	initial,
	jobId,
	onSubmit,
	onCancel,
	onAskAI,
	submitting,
}: {
	initial: FormData;
	jobId?: string;
	onSubmit: (data: FormData) => void;
	onCancel: () => void;
	onAskAI: (data: FormData) => void;
	submitting: boolean;
}) {
	const [form, setForm] = useState<FormData>(initial);
	const update = (key: keyof FormData, val: string) => setForm((f) => ({ ...f, [key]: val }));
	const models = usePuxStore((s) => s.modelList);
	const canSubmit = form.name.trim() && form.message.trim();
	const isEdit = !!jobId;

	return (
		<div className="flex flex-col gap-2.5 border-b border-border p-3">
			<div className="flex items-center justify-between">
				<span className="text-xs font-medium">{isEdit ? "Edit Job" : "New Job"}</span>
				<button onClick={onCancel} className="rounded-md p-1 text-muted-foreground hover:bg-accent">
					<X size={12} />
				</button>
			</div>

			<Field label="Name">
				<input
					value={form.name}
					onChange={(e) => update("name", e.target.value)}
					placeholder="daily-standup"
					className={inputSm}
				/>
			</Field>

			<Field label="Prompt">
				<textarea
					value={form.message}
					onChange={(e) => update("message", e.target.value)}
					placeholder="What should this job do?"
					rows={3}
					className={cn(inputSm, "resize-none")}
				/>
			</Field>

			<Field label="Model">
				<select value={form.model} onChange={(e) => update("model", e.target.value)} className={inputSm}>
					<option value="">Default</option>
					{models.map((m) => (
						<option key={m.id} value={m.id}>{m.name}</option>
					))}
				</select>
			</Field>

			<Field label="Schedule">
				<select
					value={form.scheduleType}
					onChange={(e) => update("scheduleType", e.target.value)}
					className={inputSm}
				>
					<option value="manual">Manual</option>
					<option value="cron">Cron</option>
					<option value="every">Interval</option>
				</select>
			</Field>

			{form.scheduleType === "cron" && (
				<Field label="Cron expression">
					<input
						value={form.cronExpr}
						onChange={(e) => update("cronExpr", e.target.value)}
						placeholder="0 9 * * *"
						className={cn(inputSm, "font-mono")}
					/>
				</Field>
			)}

			{form.scheduleType === "every" && (
				<Field label="Every N minutes">
					<input
						value={form.everyMinutes}
						onChange={(e) => update("everyMinutes", e.target.value)}
						placeholder="30"
						type="number"
						min="1"
						className={inputSm}
					/>
				</Field>
			)}

			<Field label="Description">
				<input
					value={form.description}
					onChange={(e) => update("description", e.target.value)}
					placeholder="Optional note"
					className={inputSm}
				/>
			</Field>

			<div className="flex items-center gap-2 pt-1">
				<button
					onClick={() => onSubmit(form)}
					disabled={!canSubmit || submitting}
					className="flex-1 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-40"
				>
					{submitting ? "Saving..." : isEdit ? "Save" : "Create"}
				</button>
				<button
					onClick={() => onAskAI(form)}
					className="inline-flex items-center gap-1 rounded-md border border-border px-3 py-1.5 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground"
				>
					<MessageSquare size={11} />
					Ask AI
				</button>
			</div>
		</div>
	);
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
	return (
		<label className="flex flex-col gap-1">
			<span className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</span>
			{children}
		</label>
	);
}

// ── Job row ──

function JobRow({
	job,
	onTrigger,
	onToggle,
	onDelete,
	onEdit,
}: {
	job: Job;
	onTrigger: (id: string) => void;
	onToggle: (id: string, enabled: boolean) => void;
	onDelete: (id: string) => void;
	onEdit: (job: Job) => void;
}) {
	const cfg = STATUS_CFG[job.status] || STATUS_CFG.idle;
	const Icon = cfg.icon;

	return (
		<div className={cn(
			"flex items-start gap-2 rounded-md border border-border px-3 py-2",
			!job.enabled && "opacity-60",
		)}>
			<div className="mt-0.5">
				<Icon size={14} className={cn(cfg.color, cfg.spin && "animate-spin")} />
			</div>
			<div className="min-w-0 flex-1">
				<div className="flex items-center gap-2">
					<span className="truncate text-sm font-medium">{job.name}</span>
					<span className="shrink-0 text-[10px] uppercase tracking-wider text-muted-foreground">
						{job.scheduleType}
					</span>
				</div>
				<div className="mt-0.5 flex items-center gap-3 text-[11px] text-muted-foreground">
					<span className="font-mono text-[10px]">{formatSchedule(job)}</span>
					{job.lastRunAt && job.lastRunAt !== "0001-01-01T00:00:00Z" && (
						<span className="flex items-center gap-1">
							{RESULT_ICON[job.lastRunStatus || "success"]}
							{relativeTime(job.lastRunAt, { never: "never", now: "just now", suffix: " ago" })}
						</span>
					)}
					{job.durationMs != null && job.durationMs > 0 && (
						<span>{formatDuration(job.durationMs)}</span>
					)}
				</div>
				{job.lastError && (
					<p className="mt-1 truncate text-[11px] text-red-400">{job.lastError}</p>
				)}
			</div>
			<div className="flex shrink-0 items-center gap-0.5">
				<button onClick={() => onEdit(job)} className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground" title="Edit">
					<Pencil size={14} />
				</button>
				<button onClick={() => onTrigger(job.id)} disabled={job.status === "running"} className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground disabled:opacity-30" title="Run now">
					<Play size={14} />
				</button>
				<button onClick={() => onToggle(job.id, !job.enabled)} className={cn("rounded-md p-1.5 hover:bg-accent", job.enabled ? "text-muted-foreground hover:text-accent-foreground" : "text-muted-foreground/40 hover:text-accent-foreground")} title={job.enabled ? "Disable" : "Enable"}>
					<Pause size={14} />
				</button>
				<button onClick={() => onDelete(job.id)} className="rounded-md p-1.5 text-muted-foreground/40 hover:bg-destructive/10 hover:text-destructive" title="Delete">
					<Trash2 size={14} />
				</button>
			</div>
		</div>
	);
}

// ── Main panel ──

export function SchedulerPanel() {
	const [jobs, setJobs] = useState<Job[]>([]);
	const [loading, setLoading] = useState(true);
	const [submitting, setSubmitting] = useState(false);
	const [showForm, setShowForm] = useState(false);
	const [editJob, setEditJob] = useState<Job | null>(null);

	const fetchJobs = useCallback(async () => {
		try {
			const resp = await fetch("/api/scheduler/");
			if (resp.ok) {
				const data = await resp.json();
				setJobs(Array.isArray(data) ? data : data.jobs ?? []);
			}
		} catch { /* ignore */ } finally { setLoading(false); }
	}, []);

	useEffect(() => { fetchJobs(); const iv = setInterval(fetchJobs, 10000); return () => clearInterval(iv); }, [fetchJobs]);

	const triggerJob = async (id: string) => { await fetch(`/api/scheduler/${id}/trigger`, { method: "POST" }); setTimeout(fetchJobs, 500); };
	const toggleJob = async (id: string, enabled: boolean) => { await fetch(`/api/scheduler/${id}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ enabled }) }); fetchJobs(); };
	const deleteJob = async (id: string) => { await fetch(`/api/scheduler/${id}`, { method: "DELETE" }); fetchJobs(); };

	const buildBody = (form: FormData) => {
		const body: Record<string, any> = {
			name: form.name.trim(),
			message: form.message.trim(),
			scheduleType: form.scheduleType,
			project: "default",
		};
		if (form.description.trim()) body.description = form.description.trim();
		if (form.model) body.model = form.model;
		if (form.scheduleType === "cron") body.cronExpr = form.cronExpr.trim();
		if (form.scheduleType === "every") {
			body.everySeconds = (parseInt(form.everyMinutes) || 30) * 60;
		}
		return body;
	};

	const submitCreate = async (form: FormData) => {
		setSubmitting(true);
		try {
			await fetch("/api/scheduler/", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(buildBody(form)) });
			setShowForm(false);
			fetchJobs();
		} finally { setSubmitting(false); }
	};

	const submitEdit = async (form: FormData) => {
		if (!editJob) return;
		setSubmitting(true);
		try {
			await fetch(`/api/scheduler/${editJob.id}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(buildBody(form)) });
			setEditJob(null);
			fetchJobs();
		} finally { setSubmitting(false); }
	};

	const askAI = (form: FormData) => {
		const parts: string[] = [editJob ? `Update the schedule "${editJob.name}" for me.` : "Create a scheduled job for me."];
		if (form.name.trim()) parts.push(`Name: ${form.name.trim()}`);
		if (form.message.trim()) parts.push(`Prompt: ${form.message.trim()}`);
		if (form.model) parts.push(`Model: ${form.model}`);
		if (form.scheduleType !== "manual") parts.push(`Schedule: ${form.scheduleType}`);
		if (form.scheduleType === "cron" && form.cronExpr.trim()) parts.push(`Cron: ${form.cronExpr.trim()}`);
		if (form.scheduleType === "every" && form.everyMinutes) parts.push(`Every ${form.everyMinutes} minutes`);
		if (form.description.trim()) parts.push(`Note: ${form.description.trim()}`);
		dispatchEvent(new CustomEvent("pux:send-message", { detail: { text: parts.join(" ") } }));
		setShowForm(false);
		setEditJob(null);
	};

	if (loading) {
		return <div className="flex h-full items-center justify-center"><Loader2 className="size-5 animate-spin text-muted-foreground" /></div>;
	}

	const formVisible = showForm || editJob;

	return (
		<div className="flex h-full flex-col">
			<div className="flex items-center justify-between border-b border-border px-3 py-1.5">
				<span className="text-xs text-muted-foreground">
					{jobs.length} job{jobs.length !== 1 ? "s" : ""}
				</span>
				<div className="flex items-center gap-1">
					<button
						onClick={() => { setEditJob(null); setShowForm((v) => !v); }}
						className={cn("rounded-md p-1 hover:bg-accent hover:text-accent-foreground", showForm ? "bg-accent text-accent-foreground" : "text-muted-foreground")}
						title="New job"
					>
						<Plus size={12} />
					</button>
					<button onClick={fetchJobs} className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground" title="Refresh">
						<RefreshCw size={12} />
					</button>
				</div>
			</div>

			{showForm && !editJob && (
				<JobForm initial={emptyForm} onSubmit={submitCreate} onCancel={() => setShowForm(false)} onAskAI={askAI} submitting={submitting} />
			)}
			{editJob && (
				<JobForm initial={jobToForm(editJob)} jobId={editJob.id} onSubmit={submitEdit} onCancel={() => setEditJob(null)} onAskAI={askAI} submitting={submitting} />
			)}

			{!formVisible && jobs.length === 0 ? (
				<div className="flex flex-1 flex-col items-center justify-center gap-3">
					<Calendar className="size-8 text-muted-foreground/50" />
					<span className="text-xs text-muted-foreground">No scheduled jobs</span>
					<button onClick={() => setShowForm(true)} className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90">
						<Plus className="size-3" /> New Job
					</button>
				</div>
			) : (
				<div className="flex-1 space-y-1.5 overflow-y-auto p-2">
					{jobs.map((job) => (
						<JobRow key={job.id} job={job} onTrigger={triggerJob} onToggle={toggleJob} onDelete={deleteJob} onEdit={setEditJob} />
					))}
				</div>
			)}
		</div>
	);
}
