import { relativeTime } from "@pux/shared";
import {
	Clock,
	AlertTriangle,
	CheckCircle,
	Loader2,
	Pause,
	Play,
	XCircle,
	Calendar,
} from "lucide-react";
import { ConfigPanel } from "./config-panel/config-panel";
import type { FieldConfig, ConfigPanelProps } from "./config-panel/types";

// ── Types ──

interface FormData {
	name: string;
	message: string;
	scheduleType: "manual" | "cron" | "every";
	cronExpr: string;
	everyMinutes: string;
	model: string;
	description: string;
}

// ── Constants ──

const emptyForm: FormData = {
	name: "",
	message: "",
	scheduleType: "manual",
	cronExpr: "",
	everyMinutes: "30",
	model: "",
	description: "",
};

const STATUS_CFG: Record<string, { icon: React.ReactNode; color: string; spin?: boolean }> = {
	idle: { icon: <Clock size={14} />, color: "text-muted-foreground" },
	running: { icon: <Loader2 size={14} />, color: "text-blue-400", spin: true },
	error: { icon: <AlertTriangle size={14} />, color: "text-red-400" },
	disabled: { icon: <Pause size={14} />, color: "text-muted-foreground" },
};

const RESULT_ICON: Record<string, React.ReactNode> = {
	success: <CheckCircle size={10} className="text-green-400" />,
	error: <XCircle size={10} className="text-red-400" />,
};

// ── Helpers ──

function formatDuration(ms: number): string {
	if (ms < 1000) return `${ms}ms`;
	const s = Math.floor(ms / 1000);
	if (s < 60) return `${s}s`;
	const m = Math.floor(s / 60);
	return `${m}m ${s % 60}s`;
}

function formatSchedule(job: any): string {
	if (job.scheduleType === "cron" && job.cronExpr) return job.cronExpr;
	if (job.scheduleType === "every" && job.everySeconds) {
		const m = Math.floor(job.everySeconds / 60);
		return m > 0 ? `every ${m}m` : `every ${job.everySeconds}s`;
	}
	if (job.scheduleType === "at") return "one-shot";
	return "manual";
}

function buildBody(form: FormData): Record<string, any> {
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
}

function jobToForm(job: any): FormData {
	return {
		name: job.name || "",
		message: job.message || "",
		scheduleType: job.scheduleType === "at" ? "manual" : (job.scheduleType as FormData["scheduleType"]),
		cronExpr: job.cronExpr || "",
		everyMinutes: job.everySeconds ? String(Math.floor(job.everySeconds / 60) || 30) : "30",
		model: job.model || "",
		description: job.description || "",
	};
}

// ── Field config ──

const schedulerFields: FieldConfig<FormData>[] = [
	{ key: "name", type: "text", label: "Name", placeholder: "daily-standup", required: true },
	{ key: "message", type: "textarea", label: "Prompt", placeholder: "What should this job do?", rows: 3, required: true },
	{ key: "model", type: "model", label: "Model" },
	{
		key: "scheduleType", type: "select", label: "Schedule",
		options: [
			{ value: "manual", label: "Manual" },
			{ value: "cron", label: "Cron" },
			{ value: "every", label: "Interval" },
		],
	},
	{ key: "cronExpr", type: "text", label: "Cron expression", placeholder: "0 9 * * *" },
	{ key: "everyMinutes", type: "number", label: "Every N minutes", placeholder: "30", min: 1 },
	{ key: "description", type: "text", label: "Description", placeholder: "Optional note" },
];

// ── Panel ──

export function SchedulerPanel() {
	return (
		<ConfigPanel<FormData>
			fetchUrl="/api/scheduler/"
			createUrl="/api/scheduler/"
			updateUrl={(id) => `/api/scheduler/${id}`}
			deleteUrl={(id) => `/api/scheduler/${id}`}
			fields={schedulerFields}
			emptyForm={emptyForm}
			formToBody={buildBody}
			responseToForm={jobToForm}
			itemDef={{
				id: (job: any) => job.id,
				label: (job: any) => job.name,
				description: (job: any) => {
					const parts: string[] = [];
					parts.push(formatSchedule(job));
					if (job.lastRunAt && job.lastRunAt !== "0001-01-01T00:00:00Z") {
						const icon = RESULT_ICON[job.lastRunStatus || "success"];
						const time = relativeTime(job.lastRunAt, { never: "never", now: "just now", suffix: " ago" });
						parts.push(`${job.lastRunStatus === "error" ? "failed" : time}`);
					}
					if (job.durationMs != null && job.durationMs > 0) {
						parts.push(formatDuration(job.durationMs));
					}
					if (job.lastError) {
						parts.push(`Error: ${job.lastError}`);
					}
					return parts.join(" · ");
				},
				badges: (job: any) => [{ text: job.scheduleType, variant: "secondary" as const }],
				status: (job: any) => STATUS_CFG[job.status] || STATUS_CFG.idle,
			}}
			title="Scheduler"
			emptyMessage="No scheduled jobs"
			emptyIcon={<Calendar className="size-8 text-muted-foreground/50" />}
			enableToggle={{ url: (id) => `/api/scheduler/${id}`, key: "enabled" }}
			extraActions={(job: any) =>
				job.status !== "running"
					? [{ label: "Run now", icon: <Play size={14} />, url: `/api/scheduler/${job.id}/trigger`, method: "POST" }]
					: []
			}
			askAITemplate={(form, isEdit, editItem) => {
				const parts: string[] = [isEdit ? `Update the schedule "${(editItem as any)?.name}" for me.` : "Create a scheduled job for me."];
				if (form.name.trim()) parts.push(`Name: ${form.name.trim()}`);
				if (form.message.trim()) parts.push(`Prompt: ${form.message.trim()}`);
				if (form.model) parts.push(`Model: ${form.model}`);
				if (form.scheduleType !== "manual") parts.push(`Schedule: ${form.scheduleType}`);
				if (form.scheduleType === "cron" && form.cronExpr.trim()) parts.push(`Cron: ${form.cronExpr.trim()}`);
				if (form.scheduleType === "every" && form.everyMinutes) parts.push(`Every ${form.everyMinutes} minutes`);
				if (form.description.trim()) parts.push(`Note: ${form.description.trim()}`);
				return parts.join(" ");
			}}
			visible={(key, form) => {
				if (key === "cronExpr") return form.scheduleType === "cron";
				if (key === "everyMinutes") return form.scheduleType === "every";
				return true;
			}}
		/>
	);
}
