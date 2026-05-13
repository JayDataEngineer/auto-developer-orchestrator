/**
 * Scheduler render tests — pure function tests for terminal output.
 *
 * Tests the formatting and rendering logic shared between TUI and extension.
 */

import { describe, test, expect } from "bun:test";
import {
	renderJobList,
	renderJobDetail,
	renderRunLog,
	renderStatusWidget,
} from "./render.js";
import type { SchedulerJob, RunLogEntry } from "./types.js";

// ── Fixtures ─────────────────────────────────────────────────

function makeJob(overrides: Partial<SchedulerJob> = {}): SchedulerJob {
	return {
		id: "j1",
		name: "test-job",
		project: "myapp",
		message: "run the test suite",
		scheduleType: "cron",
		cronExpr: "0 * * * *",
		enabled: true,
		status: "idle",
		consecutiveErrors: 0,
		createdAt: "2026-01-01T00:00:00Z",
		updatedAt: "2026-01-01T00:00:00Z",
		...overrides,
	};
}

function makeRun(overrides: Partial<RunLogEntry> = {}): RunLogEntry {
	return {
		ts: Date.now(),
		jobId: "j1",
		action: "trigger",
		status: "ok",
		durationMs: 3200,
		...overrides,
	};
}

// ── renderJobList ─────────────────────────────────────────────

describe("renderJobList", () => {
	test("empty jobs — shows empty message", () => {
		const out = renderJobList([]);
		expect(out).toContain("No scheduled jobs");
		expect(out).toContain("/scheduler create");
	});

	test("single idle job — shows name and schedule", () => {
		const out = renderJobList([makeJob()]);
		expect(out).toContain("test-job");
		expect(out).toContain("0 * * * *");
		expect(out).toContain("Scheduled Jobs (1)");
	});

	test("running job — appears first in sort", () => {
		const jobs = [
			makeJob({ id: "a", name: "alpha", status: "idle" }),
			makeJob({ id: "b", name: "bravo", status: "running" }),
		];
		const out = renderJobList(jobs);
		const alphaPos = out.indexOf("alpha");
		const bravoPos = out.indexOf("bravo");
		expect(bravoPos).toBeLessThan(alphaPos);
	});

	test("job with error — shows error tag", () => {
		const out = renderJobList([makeJob({ lastError: "something broke" })]);
		expect(out).toContain("error");
	});

	test("running job — shows running tag", () => {
		const out = renderJobList([makeJob({ status: "running" })]);
		expect(out).toContain("running");
	});

	test("every schedule — formats seconds", () => {
		const out = renderJobList([makeJob({ scheduleType: "every", everySeconds: 30, cronExpr: undefined })]);
		expect(out).toContain("every 30s");
	});

	test("every schedule — formats minutes", () => {
		const out = renderJobList([makeJob({ scheduleType: "every", everySeconds: 300, cronExpr: undefined })]);
		expect(out).toContain("every 5m");
	});

	test("every schedule — formats hours", () => {
		const out = renderJobList([makeJob({ scheduleType: "every", everySeconds: 7200, cronExpr: undefined })]);
		expect(out).toContain("every 2h");
	});

	test("every schedule — formats days", () => {
		const out = renderJobList([makeJob({ scheduleType: "every", everySeconds: 172800, cronExpr: undefined })]);
		expect(out).toContain("every 2d");
	});

	test("manual schedule — shows manual", () => {
		const out = renderJobList([makeJob({ scheduleType: "manual", cronExpr: undefined })]);
		expect(out).toContain("manual");
	});

	test("disabled job — shows disabled glyph", () => {
		const out = renderJobList([makeJob({ enabled: false })]);
		// Disabled glyph contains ANSI codes + ⊘
		expect(out).toContain("⊘");
	});

	test("job with lastRunAt — shows last run info", () => {
		const out = renderJobList([makeJob({
			lastRunAt: new Date().toISOString(),
			durationMs: 45000,
			inputTokens: 1500,
			outputTokens: 800,
		})]);
		expect(out).toContain("last:");
		expect(out).toContain("1.5k");   // inputTokens
	});

	test("shows hint commands at bottom", () => {
		const out = renderJobList([makeJob()]);
		expect(out).toContain("/scheduler trigger");
		expect(out).toContain("/scheduler runs");
	});
});

// ── renderJobDetail ───────────────────────────────────────────

describe("renderJobDetail", () => {
	test("shows job name and ID", () => {
		const out = renderJobDetail(makeJob());
		expect(out).toContain("test-job");
		expect(out).toContain("j1");
	});

	test("shows project", () => {
		const out = renderJobDetail(makeJob());
		expect(out).toContain("myapp");
	});

	test("shows schedule type and cron", () => {
		const out = renderJobDetail(makeJob());
		expect(out).toContain("0 * * * *");
	});

	test("shows enabled status", () => {
		const out = renderJobDetail(makeJob({ enabled: true }));
		expect(out).toContain("Enabled:     true");
	});

	test("shows description when present", () => {
		const out = renderJobDetail(makeJob({ description: "my desc" }));
		expect(out).toContain("Description:");
		expect(out).toContain("my desc");
	});

	test("shows model when present", () => {
		const out = renderJobDetail(makeJob({ model: "qwen3.6" }));
		expect(out).toContain("Model:");
		expect(out).toContain("qwen3.6");
	});

	test("shows error when present", () => {
		const out = renderJobDetail(makeJob({ lastError: "boom" }));
		expect(out).toContain("Error:");
		expect(out).toContain("boom");
	});

	test("shows consecutive errors when > 0", () => {
		const out = renderJobDetail(makeJob({ consecutiveErrors: 3 }));
		expect(out).toContain("Consecutive errors: 3");
	});

	test("shows webhook when present", () => {
		const out = renderJobDetail(makeJob({ webhookToken: "abc123" }));
		expect(out).toContain("Webhook:");
		expect(out).toContain("abc123");
	});

	test("shows prompt (truncated at 120 chars)", () => {
		const longMsg = "x".repeat(200);
		const out = renderJobDetail(makeJob({ message: longMsg }));
		expect(out).toContain("x".repeat(120));
		expect(out).toContain("...");
	});

	test("shows token counts after last run", () => {
		const out = renderJobDetail(makeJob({
			lastRunAt: new Date().toISOString(),
			durationMs: 5000,
			inputTokens: 2000,
			outputTokens: 500,
		}));
		expect(out).toContain("2.0k");
		expect(out).toContain("500");
	});
});

// ── renderRunLog ──────────────────────────────────────────────

describe("renderRunLog", () => {
	test("empty runs — shows empty message", () => {
		const out = renderRunLog([]);
		expect(out).toContain("No run history");
	});

	test("single run — shows timestamp and model", () => {
		const out = renderRunLog([makeRun({ model: "qwen3.6" })]);
		expect(out).toContain("Run History (1)");
		expect(out).toContain("qwen3.6");
	});

	test("ok status — shows checkmark", () => {
		const out = renderRunLog([makeRun({ status: "ok" })]);
		expect(out).toContain("✓");
	});

	test("error status — shows X and error text", () => {
		const out = renderRunLog([makeRun({ status: "error", error: "timeout" })]);
		expect(out).toContain("✗");
		expect(out).toContain("timeout");
	});

	test("summary — shows truncated", () => {
		const out = renderRunLog([makeRun({ summary: "All tests passed successfully" })]);
		expect(out).toContain("All tests passed");
	});

	test("limits to 20 runs", () => {
		const runs = Array.from({ length: 25 }, (_, i) => makeRun({ ts: Date.now() + i }));
		const out = renderRunLog(runs);
		expect(out).toContain("Run History (25)");
		// Check that only 20 entries rendered (20 lines with status glyphs)
		const checkmarks = (out.match(/✓/g) || []).length;
		expect(checkmarks).toBe(20);
	});
});

// ── renderStatusWidget ────────────────────────────────────────

describe("renderStatusWidget", () => {
	test("empty jobs — returns empty string", () => {
		expect(renderStatusWidget([])).toBe("");
	});

	test("idle jobs only — shows count", () => {
		const out = renderStatusWidget([makeJob(), makeJob()]);
		expect(out).toContain("2 jobs");
	});

	test("running job — highlights running count", () => {
		const out = renderStatusWidget([makeJob({ status: "running" })]);
		expect(out).toContain("1 running");
		expect(out).toContain("running");  // ANSI-wrapped yellow
	});

	test("error job — highlights error count", () => {
		const out = renderStatusWidget([makeJob({ status: "error" })]);
		expect(out).toContain("1 error");
		expect(out).toContain("error");    // ANSI-wrapped red
	});

	test("mixed — shows all counts", () => {
		const jobs = [
			makeJob({ id: "a", status: "idle" }),
			makeJob({ id: "b", status: "running" }),
			makeJob({ id: "c", status: "error" }),
		];
		const out = renderStatusWidget(jobs);
		expect(out).toContain("3 jobs");
		expect(out).toContain("1 running");
		expect(out).toContain("1 error");
	});
});
