import { describe, expect, test } from "bun:test";
import { relativeTime } from "./relative-time";

describe("relativeTime", () => {
  test("returns empty string for undefined input", () => {
    expect(relativeTime()).toBe("");
  });

  test("returns custom never text for undefined input", () => {
    expect(relativeTime(undefined, { never: "never" })).toBe("never");
  });

  test('returns "now" for timestamp less than 1 minute ago', () => {
    const justNow = new Date(Date.now() - 30_000).toISOString();
    expect(relativeTime(justNow)).toBe("now");
  });

  test("returns custom now text", () => {
    const justNow = new Date(Date.now() - 10_000).toISOString();
    expect(relativeTime(justNow, { now: "just now" })).toBe("just now");
  });

  test("returns minutes for < 60 min", () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60_000).toISOString();
    expect(relativeTime(fiveMinAgo)).toBe("5m");
  });

  test("appends suffix for minutes", () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60_000).toISOString();
    expect(relativeTime(fiveMinAgo, { suffix: " ago" })).toBe("5m ago");
  });

  test("returns hours for < 24 hours", () => {
    const threeHrsAgo = new Date(Date.now() - 3 * 3600_000).toISOString();
    expect(relativeTime(threeHrsAgo)).toBe("3h");
  });

  test("appends suffix for hours", () => {
    const threeHrsAgo = new Date(Date.now() - 3 * 3600_000).toISOString();
    expect(relativeTime(threeHrsAgo, { suffix: " ago" })).toBe("3h ago");
  });

  test("returns days for >= 24 hours", () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 86400_000).toISOString();
    expect(relativeTime(twoDaysAgo)).toBe("2d");
  });

  test("appends suffix for days", () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 86400_000).toISOString();
    expect(relativeTime(twoDaysAgo, { suffix: " ago" })).toBe("2d ago");
  });

  test("handles invalid date string", () => {
    expect(relativeTime("not-a-date")).toBe("—");
  });

  test("handles invalid date with custom never", () => {
    expect(relativeTime("not-a-date", { never: "N/A" })).toBe("N/A");
  });

  test("handles 59 minutes ago", () => {
    const time = new Date(Date.now() - 59 * 60_000).toISOString();
    expect(relativeTime(time)).toBe("59m");
  });

  test("handles 1 minute ago", () => {
    const time = new Date(Date.now() - 60_000).toISOString();
    expect(relativeTime(time)).toBe("1m");
  });

  test("handles 23 hours ago", () => {
    const time = new Date(Date.now() - 23 * 3600_000).toISOString();
    expect(relativeTime(time)).toBe("23h");
  });

  test("handles 1 hour ago boundary", () => {
    const time = new Date(Date.now() - 60 * 60_000).toISOString();
    expect(relativeTime(time)).toBe("1h");
  });

  test("handles exactly 24 hours ago as 1d", () => {
    const time = new Date(Date.now() - 24 * 3600_000).toISOString();
    expect(relativeTime(time)).toBe("1d");
  });

  test("combines all options", () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60_000).toISOString();
    expect(
      relativeTime(fiveMinAgo, { never: "never", now: "just now", suffix: " ago" }),
    ).toBe("5m ago");
  });

  test("returns now text for 0 diff (same millisecond)", () => {
    const now = new Date().toISOString();
    expect(relativeTime(now, { now: "just now" })).toBe("just now");
  });
});
