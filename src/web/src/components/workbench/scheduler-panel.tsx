import { useEffect, useState, useCallback } from "react";
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
} from "lucide-react";

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

const STATUS_CONFIG: Record<string, { icon: typeof Clock; color: string; label: string; spin?: boolean }> = {
	idle: { icon: Clock, color: "text-muted-foreground", label: "Idle" },
	running: { icon: Loader2, color: "text-blue-400", label: "Running", spin: true },
	error: { icon: AlertTriangle, color: "text-red-400", label: "Error" },
	disabled: { icon: Pause, color: "text-muted-foreground", label: "Disabled" },
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

function relativeTime(iso?: string): string {
	if (!iso) return "never";
	const diff = Date.now() - new Date(iso).getTime();
	if (isNaN(diff)) return "—";
	const mins = Math.floor(diff / 60000);
	if (mins < 1) return "just now";
	if (mins < 60) return `${mins}m ago`;
	const hrs = Math.floor(mins / 60);
	if (hrs < 24) return `${hrs}h ago`;
	const days = Math.floor(hrs / 24);
	return `${days}d ago`;
}

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
	const statusCfg = STATUS_CONFIG[job.status] || STATUS_CONFIG.idle;
	const StatusIcon = statusCfg.icon;

	return (
		<div className={cn(
			"flex items-start gap-2 rounded-md border border-border px-3 py-2",
			!job.enabled && "opacity-60",
		)}>
			<div className="mt-0.5">
				<StatusIcon
					size={14}
					className={cn(statusCfg.color, statusCfg.spin && "animate-spin")}
				/>
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
							{relativeTime(job.lastRunAt)}
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

export function SchedulerPanel() {
	const [jobs, setJobs] = useState<Job[]>([]);
	const [loading, setLoading] = useState(true);
	const [creating, setCreating] = useState(false);

	const fetchJobs = useCallback(async () => {
		try {
			const resp = await fetch("/api/scheduler/");
			if (resp.ok) {
				const data = await resp.json();
				const list = Array.isArray(data) ? data : data.jobs ?? [];
				setJobs(list);
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
		// Brief delay then refresh to show "running" status
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

	const createTestJob = async () => {
		setCreating(true);
		try {
			await fetch("/api/scheduler/", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({
					name: `test-${Date.now().toString(36)}`,
					description: "Test job created from scheduler panel",
					project: "default",
					message: "Echo hello world. Say the current time.",
					scheduleType: "manual",
				}),
			});
			fetchJobs();
		} finally {
			setCreating(false);
		}
	};

	if (loading) {
		return (
			<div className="flex h-full items-center justify-center">
				<Loader2 className="size-5 animate-spin text-muted-foreground" />
			</div>
		);
	}

	if (jobs.length === 0) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3">
				<Calendar className="size-8 text-muted-foreground/50" />
				<span className="text-xs text-muted-foreground">No scheduled jobs</span>
				<button
					onClick={createTestJob}
					disabled={creating}
					className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
				>
					<Plus className="size-3" />
					{creating ? "Creating..." : "Create test job"}
				</button>
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
						onClick={createTestJob}
						disabled={creating}
						className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground disabled:opacity-30"
						title="Create test job"
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
		</div>
	);
}
