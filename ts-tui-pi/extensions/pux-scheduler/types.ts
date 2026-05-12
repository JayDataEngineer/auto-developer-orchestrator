/**
 * Scheduler types — mirrors Go backend types from internal/scheduler/
 */

export type ScheduleType = "cron" | "every" | "at" | "manual";
export type JobStatus = "idle" | "running" | "error" | "disabled";
export type DeliveryMode = "store" | "webhook" | "session";

export interface SchedulerJob {
	id: string;
	name: string;
	description?: string;
	project: string;
	agentId?: string;
	message: string;
	model?: string;
	scheduleType: ScheduleType;
	cronExpr?: string;
	timezone?: string;
	everySeconds?: number;
	atTime?: string;
	autoBranch?: boolean;
	autoMerge?: boolean;
	enabled: boolean;
	deliveryMode?: DeliveryMode;
	deliveryWebhookUrl?: string;
	failureAlertAfter?: number;
	failureAlertWebhookUrl?: string;
	status: JobStatus;
	lastRunAt?: string;
	lastRunStatus?: string;
	lastError?: string;
	nextRunAt?: string;
	consecutiveErrors: number;
	createdAt: string;
	updatedAt: string;
	inputTokens?: number;
	outputTokens?: number;
	durationMs?: number;
	blocks?: string[];
	blockedBy?: string[];
	webhookToken?: string;
}

export interface RunLogEntry {
	ts: number;
	jobId: string;
	action: string;
	status?: string;
	error?: string;
	summary?: string;
	runAtMs?: number;
	durationMs?: number;
	nextRunAtMs?: number;
	model?: string;
	provider?: string;
}

export interface CreateJobRequest {
	name: string;
	description?: string;
	project: string;
	agentId?: string;
	message: string;
	model?: string;
	scheduleType: ScheduleType;
	cronExpr?: string;
	timezone?: string;
	everySeconds?: number;
	atTime?: string;
	enabled?: boolean;
}
