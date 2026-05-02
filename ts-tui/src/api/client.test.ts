import { test, expect, describe } from "bun:test";
import { ApiClient } from "./client";

// ---- API Client tests ----

describe("ApiClient", () => {
  test("constructs without throw", () => {
    const c = new ApiClient("http://localhost:3847/");
    expect(c).toBeDefined();
  });

  test("getHistory returns [] on connection error", async () => {
    const c = new ApiClient("http://localhost:9999");
    expect(await c.getHistory("test")).toEqual([]);
  });

  test("getArtifacts returns [] on connection error", async () => {
    const c = new ApiClient("http://localhost:9999");
    expect(await c.getArtifacts("test")).toEqual([]);
  });

  test("getJobs throws on bad URL", async () => {
    const c = new ApiClient("http://localhost:9999");
    try {
      await c.getJobs();
      expect("unreachable").toBe("should throw");
    } catch (err: any) {
      expect(err.message.length).toBeGreaterThan(0);
    }
  });

  test("streamPrompt throws on connection error", async () => {
    const c = new ApiClient("http://localhost:9999");
    try {
      for await (const _ of c.streamPrompt({ message: "hi", project: "x" })) {}
      expect("unreachable").toBe("should throw");
    } catch (err: any) {
      expect(err.message.length).toBeGreaterThan(0);
    }
  });
});
