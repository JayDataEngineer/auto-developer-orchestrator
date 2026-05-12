/**
 * SchedulerClient tests — mock fetch, verify API contract.
 *
 * These prove the shared client code works for both TUI extension and web panel.
 */

import { describe, test, expect, mock, beforeEach, afterEach } from "bun:test";
import { SchedulerClient } from "./api.js";
import type { SchedulerJob, RunLogEntry, CreateJobRequest } from "./types.js";

// ── Helpers ──────────────────────────────────────────────────

const BASE = "http://localhost:3847";

function mockFetch(status: number, body: unknown): void {
	(globalThis as any).fetch = mock(() =>
		Promise.resolve({
			ok: status >= 200 && status < 300,
			status,
			json: () => Promise.resolve(body),
		})
	);
}

function mockFetchReject(err: Error): void {
	(globalThis as any).fetch = mock(() => Promise.reject(err));
}

function lastCallUrl(): string {
	const calls = (globalThis.fetch as any).mock.calls;
	return calls[calls.length - 1][0];
}

function lastCallOptions(): RequestInit | undefined {
	const calls = (globalThis.fetch as any).mock.calls;
	return calls[calls.length - 1][1];
}

const sampleJob: SchedulerJob = {
	id: "j1",
	name: "test-job",
	project: "myapp",
	message: "run tests",
	scheduleType: "cron",
	cronExpr: "0 * * * *",
	enabled: true,
	status: "idle",
	consecutiveErrors: 0,
	createdAt: "2026-01-01T00:00:00Z",
	updatedAt: "2026-01-01T00:00:00Z",
};

const sampleRun: RunLogEntry = {
	ts: Date.now(),
	jobId: "j1",
	action: "trigger",
	status: "ok",
	durationMs: 5234,
	model: "qwen3.6",
};

let originalFetch: typeof fetch;

beforeEach(() => {
	originalFetch = globalThis.fetch;
});

afterEach(() => {
	globalThis.fetch = originalFetch;
});

// ── Tests ────────────────────────────────────────────────────

describe("SchedulerClient", () => {
	test("constructor sets baseUrl correctly", () => {
		const client = new SchedulerClient(BASE);
		// baseUrl is private but we verify it via fetch calls
		mockFetch(200, { jobs: [] });
		client.listJobs();
		expect(lastCallUrl()).toBe(`${BASE}/api/scheduler`);
	});

	test("listJobs — returns jobs array", async () => {
		mockFetch(200, { jobs: [sampleJob] });
		const client = new SchedulerClient(BASE);
		const jobs = await client.listJobs();
		expect(jobs).toHaveLength(1);
		expect(jobs[0].name).toBe("test-job");
	});

	test("listJobs — returns empty array when response has no jobs key", async () => {
		mockFetch(200, {});
		const client = new SchedulerClient(BASE);
		const jobs = await client.listJobs();
		expect(jobs).toEqual([]);
	});

	test("listJobs — throws on non-200", async () => {
		mockFetch(500, {});
		const client = new SchedulerClient(BASE);
		expect(client.listJobs()).rejects.toThrow("scheduler list: 500");
	});

	test("getJob — fetches single job by id", async () => {
		mockFetch(200, sampleJob);
		const client = new SchedulerClient(BASE);
		const job = await client.getJob("j1");
		expect(job.id).toBe("j1");
		expect(lastCallUrl()).toBe(`${BASE}/api/scheduler/j1`);
	});

	test("getJob — throws on 404", async () => {
		mockFetch(404, {});
		const client = new SchedulerClient(BASE);
		expect(client.getJob("missing")).rejects.toThrow("scheduler get: 404");
	});

	test("createJob — POSTs with correct body", async () => {
		mockFetch(200, { job: sampleJob });
		const client = new SchedulerClient(BASE);
		const req: CreateJobRequest = {
			name: "test-job",
			project: "myapp",
			message: "run tests",
			scheduleType: "cron",
			cronExpr: "0 * * * *",
		};
		const job = await client.createJob(req);
		expect(job.name).toBe("test-job");

		const opts = lastCallOptions();
		expect(opts?.method).toBe("POST");
		expect(opts?.headers).toEqual({ "Content-Type": "application/json" });
		expect(JSON.parse(opts?.body as string)).toEqual(req);
	});

	test("updateJob — PUTs partial update", async () => {
		const updated = { ...sampleJob, cronExpr: "*/30 * * * *" };
		mockFetch(200, { job: updated });
		const client = new SchedulerClient(BASE);
		const job = await client.updateJob("j1", { cronExpr: "*/30 * * * *" });
		expect(job.cronExpr).toBe("*/30 * * * *");

		const opts = lastCallOptions();
		expect(opts?.method).toBe("PUT");
		expect(lastCallUrl()).toBe(`${BASE}/api/scheduler/j1`);
	});

	test("deleteJob — sends DELETE", async () => {
		mockFetch(200, {});
		const client = new SchedulerClient(BASE);
		await client.deleteJob("j1");
		const opts = lastCallOptions();
		expect(opts?.method).toBe("DELETE");
		expect(lastCallUrl()).toBe(`${BASE}/api/scheduler/j1`);
	});

	test("deleteJob — throws on error", async () => {
		mockFetch(403, {});
		const client = new SchedulerClient(BASE);
		expect(client.deleteJob("j1")).rejects.toThrow("scheduler delete: 403");
	});

	test("triggerJob — POSTs trigger and returns message", async () => {
		mockFetch(200, { message: "triggered" });
		const client = new SchedulerClient(BASE);
		const msg = await client.triggerJob("j1");
		expect(msg).toBe("triggered");

		const opts = lastCallOptions();
		expect(opts?.method).toBe("POST");
		expect(lastCallUrl()).toBe(`${BASE}/api/scheduler/j1/trigger`);
	});

	test("triggerJob — throws on error", async () => {
		mockFetch(409, {});
		const client = new SchedulerClient(BASE);
		expect(client.triggerJob("j1")).rejects.toThrow("scheduler trigger: 409");
	});

	test("listRuns — fetches all runs when no jobId", async () => {
		mockFetch(200, { runs: [sampleRun] });
		const client = new SchedulerClient(BASE);
		const runs = await client.listRuns();
		expect(runs).toHaveLength(1);
		expect(runs[0].status).toBe("ok");
		expect(lastCallUrl()).toContain(`${BASE}/api/scheduler/runs?`);
	});

	test("listRuns — fetches runs for specific job", async () => {
		mockFetch(200, { runs: [sampleRun] });
		const client = new SchedulerClient(BASE);
		await client.listRuns("j1");
		expect(lastCallUrl()).toContain(`${BASE}/api/scheduler/j1/runs?`);
	});

	test("listRuns — passes limit param", async () => {
		mockFetch(200, { runs: [] });
		const client = new SchedulerClient(BASE);
		await client.listRuns(undefined, 10);
		expect(lastCallUrl()).toContain("limit=10");
	});

	test("listRuns — returns empty array when no runs key", async () => {
		mockFetch(200, {});
		const client = new SchedulerClient(BASE);
		const runs = await client.listRuns();
		expect(runs).toEqual([]);
	});

	test("listRuns — throws on server error", async () => {
		mockFetch(500, {});
		const client = new SchedulerClient(BASE);
		expect(client.listRuns()).rejects.toThrow("scheduler runs: 500");
	});

	test("network failure — throws fetch error", async () => {
		mockFetchReject(new Error("ECONNREFUSED"));
		const client = new SchedulerClient(BASE);
		expect(client.listJobs()).rejects.toThrow("ECONNREFUSED");
	});
});
