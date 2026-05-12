/**
 * Shared formatSchedule test — validates the single source of truth.
 *
 * The web panel imports this from ts-tui-pi/extensions/pux-scheduler/render.js.
 * If we ever change the format, these tests catch regressions in both TUI and web.
 */

import { describe, test, expect } from "bun:test";
import { formatSchedule } from "../../ts-tui-pi/extensions/pux-scheduler/render.js";
import type { SchedulerJob } from "../../ts-tui-pi/extensions/pux-scheduler/types.js";

describe("formatSchedule (shared — TUI + web)", () => {
	test("cron — shows expression", () => {
		const job = { scheduleType: "cron" as const, cronExpr: "*/5 * * * *" } as SchedulerJob;
		expect(formatSchedule(job)).toBe("*/5 * * * *");
	});

	test("cron — no expression — shows 'cron'", () => {
		const job = { scheduleType: "cron" as const } as SchedulerJob;
		expect(formatSchedule(job)).toBe("cron");
	});

	test("every — seconds", () => {
		const job = { scheduleType: "every" as const, everySeconds: 15 } as SchedulerJob;
		expect(formatSchedule(job)).toBe("every 15s");
	});

	test("every — minutes", () => {
		const job = { scheduleType: "every" as const, everySeconds: 180 } as SchedulerJob;
		expect(formatSchedule(job)).toBe("every 3m");
	});

	test("every — hours", () => {
		const job = { scheduleType: "every" as const, everySeconds: 7200 } as SchedulerJob;
		expect(formatSchedule(job)).toBe("every 2h");
	});

	test("every — days", () => {
		const job = { scheduleType: "every" as const, everySeconds: 172800 } as SchedulerJob;
		expect(formatSchedule(job)).toBe("every 2d");
	});

	test("at — shows time", () => {
		const job = { scheduleType: "at" as const, atTime: "2026-01-01T12:00:00Z" } as SchedulerJob;
		expect(formatSchedule(job)).toBe("at 2026-01-01T12:00:00");
	});

	test("at — no time — shows 'at ?'", () => {
		const job = { scheduleType: "at" as const } as SchedulerJob;
		expect(formatSchedule(job)).toBe("at ?");
	});

	test("manual — shows manual", () => {
		const job = { scheduleType: "manual" as const } as SchedulerJob;
		expect(formatSchedule(job)).toBe("manual");
	});

	test("unknown — shows ?", () => {
		const job = { scheduleType: "unknown" as any } as SchedulerJob;
		expect(formatSchedule(job)).toBe("?");
	});

	test("every — zero seconds", () => {
		const job = { scheduleType: "every" as const, everySeconds: 0 } as SchedulerJob;
		expect(formatSchedule(job)).toBe("every 0s");
	});
});
