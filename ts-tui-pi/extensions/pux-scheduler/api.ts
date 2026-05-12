/**
 * Scheduler API client — thin fetch wrapper to Go backend /api/scheduler/*
 */

import type { SchedulerJob, RunLogEntry, CreateJobRequest } from "./types.js";

export class SchedulerClient {
	private baseUrl: string;

	constructor(serverUrl: string) {
		this.baseUrl = `${serverUrl}/api/scheduler`;
	}

	async listJobs(): Promise<SchedulerJob[]> {
		const resp = await fetch(this.baseUrl);
		if (!resp.ok) throw new Error(`scheduler list: ${resp.status}`);
		const data = await resp.json() as { jobs: SchedulerJob[] };
		return data.jobs || [];
	}

	async getJob(id: string): Promise<SchedulerJob> {
		const resp = await fetch(`${this.baseUrl}/${id}`);
		if (!resp.ok) throw new Error(`scheduler get: ${resp.status}`);
		return resp.json() as Promise<SchedulerJob>;
	}

	async createJob(req: CreateJobRequest): Promise<SchedulerJob> {
		const resp = await fetch(this.baseUrl, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(req),
		});
		if (!resp.ok) throw new Error(`scheduler create: ${resp.status}`);
		const data = await resp.json() as { job: SchedulerJob };
		return data.job;
	}

	async updateJob(id: string, req: Partial<CreateJobRequest>): Promise<SchedulerJob> {
		const resp = await fetch(`${this.baseUrl}/${id}`, {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(req),
		});
		if (!resp.ok) throw new Error(`scheduler update: ${resp.status}`);
		const data = await resp.json() as { job: SchedulerJob };
		return data.job;
	}

	async deleteJob(id: string): Promise<void> {
		const resp = await fetch(`${this.baseUrl}/${id}`, { method: "DELETE" });
		if (!resp.ok) throw new Error(`scheduler delete: ${resp.status}`);
	}

	async triggerJob(id: string): Promise<string> {
		const resp = await fetch(`${this.baseUrl}/${id}/trigger`, { method: "POST" });
		if (!resp.ok) throw new Error(`scheduler trigger: ${resp.status}`);
		const data = await resp.json() as { message: string };
		return data.message;
	}

	async listRuns(jobId?: string, limit?: number): Promise<RunLogEntry[]> {
		const params = new URLSearchParams();
		if (limit) params.set("limit", String(limit));
		const base = jobId ? `${this.baseUrl}/${jobId}/runs` : `${this.baseUrl}/runs`;
		const resp = await fetch(`${base}?${params}`);
		if (!resp.ok) throw new Error(`scheduler runs: ${resp.status}`);
		const data = await resp.json() as { runs: RunLogEntry[] };
		return data.runs || [];
	}
}
