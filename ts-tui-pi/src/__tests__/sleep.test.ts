import { describe, test, expect } from "bun:test";
import { sleep } from "../utils/sleep.js";

describe("sleep", () => {
  test("resolves after given ms", async () => {
    const start = Date.now();
    await sleep(10);
    const elapsed = Date.now() - start;
    expect(elapsed).toBeGreaterThanOrEqual(5);
  });

  test("rejects immediately on pre-aborted signal", async () => {
    const ac = new AbortController();
    ac.abort();
    await expect(sleep(1000, ac.signal)).rejects.toThrow("Aborted");
  });

  test("rejects when signal is aborted during sleep", async () => {
    const ac = new AbortController();
    const promise = sleep(1000, ac.signal);
    ac.abort();
    await expect(promise).rejects.toThrow("Aborted");
  });

  test("resolves when signal is not aborted", async () => {
    const ac = new AbortController();
    await sleep(5, ac.signal);
    expect(true).toBe(true);
  });

  test("works without a signal", async () => {
    await sleep(1);
    expect(true).toBe(true);
  });
});
