import { useEffect, useState, useCallback } from "react";
import { relativeTime } from "@pux/shared";
import { cn } from "@/lib/utils";
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
} from "lucide-react";

// ── Types ──

interface Job {
	id: string;
	name: string;
	description?: string;
	project?: string;
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
	createdAt: string;
	durationMs?: number;
}

type ScheduleType = "manual" | "cron" | "every" | "at";

interface FormData {
	name: string;
	message: string;
	scheduleType: ScheduleType;
	cronExpr: string;
	everyMinutes: string;
	description: string;
}

const emptyForm: FormData = {
	name: "",
	message: "",
	scheduleType: "manual",
	cronExpr: "",
	everyMinutes: "30",
	description: "",
};

// ── Helpers ──

const STATUS_CONFIG: Record<string, { icon: typeof Clock; color: string; spin?: boolean }> = {
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

// ── Job row ──

function JobRow({
	job,
	onTrigger,
	onToggle,
	onDelete,
}: {
	job: Job;
	onTrigger: (id: string) => void;
	onToggle: (id: string, enabled: boolean) => void;
	onDelete: (id: string) => void;
}) {
	const cfg = STATUS_CONFIG[job.status] || STATUS_CONFIG.idle;
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
				<button
					onClick={() => onTrigger(job.id)}
					disabled={job.status === "running"}
					className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground disabled:opacity-30"
					title="Run now"
				>
					<Play size={14} />
				</button>
				<button
					onClick={() => onToggle(job.id, !job.enabled)}
					className={cn(
						"rounded-md p-1.5 hover:bg-accent",
						job.enabled ? "text-muted-foreground hover:text-accent-foreground" : "text-muted-foreground/40 hover:text-accent-foreground",
					)}
					title={job.enabled ? "Disable" : "Enable"}
				>
					<Pause size={14} />
				</button>
				<button
					onClick={() => onDelete(job.id)}
					className="rounded-md p-1.5 text-muted-foreground/40 hover:bg-destructive/10 hover:text-destructive"
					title="Delete"
				>
					<Trash2 size={14} />
				</button>
			</div>
		</div>
	);
}

// ── New job form ──

function NewJobForm({
	onSubmit,
	onCancel,
	onAskAI,
	submitting,
}: {
	onSubmit: (data: FormData) => void;
	onCancel: () => void;
	onAskAI: (data: FormData) => void;
	submitting: boolean;
}) {
	const [form, setForm] = useState<FormData>(emptyForm);
	const update = (key: keyof FormData, val: string) => setForm((f) => ({ ...f, [key]: val }));

	const canSubmit = form.name.trim() && form.message.trim();

	return (
		<div className="flex flex-col gap-3 border-b border-border p-3">
			<div className="flex items-center justify-between">
				<span className="text-xs font-medium">New Job</span>
				<button onClick={onCancel} className="rounded-md p-1 text-muted-foreground hover:bg-accent">
					<X size={12} />
				</button>
			</div>

			<label className="flex flex-col gap-1">
				<span className="text-[10px] uppercase tracking-wider text-muted-foreground">Name</span>
				<input
					value={form.name}
					onChange={(e) => update("name", e.target.value)}
					placeholder="daily-standup"
					className="rounded-md border border-border bg-transparent px-2 py-1.5 text-xs outline-none focus:border-ring"
				/>
			</label>

			<label className="flex flex-col gap-1">
				<span className="text-[10px] uppercase tracking-wider text-muted-foreground">Prompt</span>
				<textarea
					value={form.message}
					onChange={(e) => update("message", e.target.value)}
					placeholder="What should this job do each time it runs?"
					rows={3}
					className="rounded-md border border-border bg-transparent px-2 py-1.5 text-xs outline-none focus:border-ring resize-none"
				/>
			</label>

			<label className="flex flex-col gap-1">
				<span className="text-[10px] uppercase tracking-wider text-muted-foreground">Schedule</span>
				<select
					value={form.scheduleType}
					onChange={(e) => update("scheduleType", e.target.value)}
					className="rounded-md border border-border bg-transparent px-2 py-1.5 text-xs outline-none focus:border-ring"
				>
					<option value="manual">Manual (run on demand)</option>
					<option value="cron">Cron expression</option>
					<option value="every">Interval</option>
				</select>
			</label>

			{form.scheduleType === "cron" && (
				<label className="flex flex-col gap-1">
					<span className="text-[10px] uppercase tracking-wider text-muted-foreground">Cron</span>
					<input
						value={form.cronExpr}
						onChange={(e) => update("cronExpr", e.target.value)}
						placeholder="0 9 * * *"
						className="rounded-md border border-border bg-transparent px-2 py-1.5 font-mono text-xs outline-none focus:border-ring"
					/>
				</label>
			)}

			{form.scheduleType === "every" && (
				<label className="flex flex-col gap-1">
					<span className="text-[10px] uppercase tracking-wider text-muted-foreground">Every N minutes</span>
					<input
						value={form.everyMinutes}
						onChange={(e) => update("everyMinutes", e.target.value)}
						placeholder="30"
						type="number"
						min="1"
						className="rounded-md border border-border bg-transparent px-2 py-1.5 text-xs outline-none focus:border-ring"
					/>
				</label>
			)}

			<label className="flex flex-col gap-1">
				<span className="text-[10px] uppercase tracking-wider text-muted-foreground">Description</span>
				<input
					value={form.description}
					onChange={(e) => update("description", e.target.value)}
					placeholder="Optional note"
					className="rounded-md border border-border bg-transparent px-2 py-1.5 text-xs outline-none focus:border-ring"
				/>
			</label>

			<div className="flex items-center gap-2 pt-1">
				<button
					onClick={() => onSubmit(form)}
					disabled={!canSubmit || submitting}
					className="flex-1 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-40"
				>
					{submitting ? "Creating..." : "Create Job"}
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

// ── Main panel ──

export function SchedulerPanel() {
	const [jobs, setJobs] = useState<Job[]>([]);
	const [loading, setLoading] = useState(true);
	const [submitting, setSubmitting] = useState(false);
	const [showForm, setShowForm] = useState(false);

	const fetchJobs = useCallback(async () => {
		try {
			const resp = await fetch("/api/scheduler/");
			if (resp.ok) {
				const data = await resp.json();
				setJobs(Array.isArray(data) ? data : data.jobs ?? []);
			}
		} catch {
			// ignore
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		fetchJobs();
		const interval = setInterval(fetchJobs, 10000);
		return () => clearInterval(interval);
	}, [fetchJobs]);

	const triggerJob = async (id: string) => {
		await fetch(`/api/scheduler/${id}/trigger`, { method: "POST" });
		setTimeout(fetchJobs, 500);
	};

	const toggleJob = async (id: string, enabled: boolean) => {
		await fetch(`/api/scheduler/${id}`, {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ enabled }),
		});
		fetchJobs();
	};

	const deleteJob = async (id: string) => {
		await fetch(`/api/scheduler/${id}`, { method: "DELETE" });
		fetchJobs();
	};

	const submitJob = async (form: FormData) => {
		setSubmitting(true);
		try {
			const body: Record<string, any> = {
				name: form.name.trim(),
				message: form.message.trim(),
				scheduleType: form.scheduleType,
				project: "default",
			};
			if (form.description.trim()) body.description = form.description.trim();
			if (form.scheduleType === "cron") body.cronExpr = form.cronExpr.trim();
			if (form.scheduleType === "every") {
				const mins = parseInt(form.everyMinutes) || 30;
				body.everySeconds = mins * 60;
			}

			await fetch("/api/scheduler/", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(body),
			});
			setShowForm(false);
			fetchJobs();
		} finally {
			setSubmitting(false);
		}
	};

	const askAI = (form: FormData) => {
		const parts: string[] = ["Create a scheduled job for me."];
		if (form.name.trim()) parts.push(`Name: ${form.name.trim()}`);
		if (form.message.trim()) parts.push(`Prompt: ${form.message.trim()}`);
		if (form.scheduleType !== "manual") parts.push(`Schedule: ${form.scheduleType}`);
		if (form.scheduleType === "cron" && form.cronExpr.trim()) parts.push(`Cron: ${form.cronExpr.trim()}`);
		if (form.scheduleType === "every" && form.everyMinutes) parts.push(`Every ${form.everyMinutes} minutes`);
		if (form.description.trim()) parts.push(`Description: ${form.description.trim()}`);

		dispatchEvent(new CustomEvent("pux:send-message", {
			detail: { text: parts.join(" ") },
		}));
		setShowForm(false);
	};

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center">
				<Loader2 className="size-5 animate-spin text-muted-foreground" />
			</div>
		);
	}

	return (
		<div className="flex h-full flex-col">
			<div className="flex items-center justify-between border-b border-border px-3 py-1.5">
				<span className="text-xs text-muted-foreground">
					{jobs.length} job{jobs.length !== 1 ? "s" : ""}
				</span>
				<div className="flex items-center gap-1">
					<button
						onClick={() => setShowForm((v) => !v)}
						className={cn(
							"rounded-md p-1 hover:bg-accent hover:text-accent-foreground",
							showForm ? "bg-accent text-accent-foreground" : "text-muted-foreground",
						)}
						title="New job"
					>
						<Plus size={12} />
					</button>
					<button
						onClick={fetchJobs}
						className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
						title="Refresh"
					>
						<RefreshCw size={12} />
					</button>
				</div>
			</div>

			{showForm && (
				<NewJobForm
					onSubmit={submitJob}
					onCancel={() => setShowForm(false)}
					onAskAI={askAI}
					submitting={submitting}
				/>
			)}

			{!showForm && jobs.length === 0 ? (
				<div className="flex flex-1 flex-col items-center justify-center gap-3">
					<Calendar className="size-8 text-muted-foreground/50" />
					<span className="text-xs text-muted-foreground">No scheduled jobs</span>
					<button
						onClick={() => setShowForm(true)}
						className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
					>
						<Plus className="size-3" />
						New Job
					</button>
				</div>
			) : (
				<div className="flex-1 space-y-1.5 overflow-y-auto p-2">
					{jobs.map((job) => (
						<JobRow
							key={job.id}
							job={job}
							onTrigger={triggerJob}
							onToggle={toggleJob}
							onDelete={deleteJob}
						/>
					))}
				</div>
			)}
		</div>
	);
}
