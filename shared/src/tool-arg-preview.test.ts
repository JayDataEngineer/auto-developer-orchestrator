import { describe, expect, test } from "bun:test";
import { getToolArgPreview } from "./tool-arg-preview";

describe("getToolArgPreview", () => {
  test("returns empty string for undefined args", () => {
    expect(getToolArgPreview("bash")).toBe("");
  });

  test("returns empty string for empty args", () => {
    expect(getToolArgPreview("bash", {})).toBe("");
  });

  // bash / shell / run_command
  test("shows command for bash tool", () => {
    expect(getToolArgPreview("bash", { command: "ls -la" })).toBe("ls -la");
  });

  test("shows cmd field for shell tool", () => {
    expect(getToolArgPreview("shell", { cmd: "npm test" })).toBe("npm test");
  });

  test("truncates long commands", () => {
    const long = "a".repeat(80);
    const result = getToolArgPreview("bash", { command: long });
    expect(result).toHaveLength(60);
    expect(result).toEndWith("...");
  });

  test("truncates to custom maxLen", () => {
    const long = "a".repeat(80);
    const result = getToolArgPreview("bash", { command: long }, 20);
    expect(result).toHaveLength(20);
    expect(result).toEndWith("...");
  });

  // delegate_to / delegate_async
  test("shows agent name for delegate_to", () => {
    expect(getToolArgPreview("delegate_to", { agent: "sarah" })).toBe("sarah");
  });

  test("shows agent name for delegate_async", () => {
    expect(getToolArgPreview("delegate_async", { agent: "jake" })).toBe("jake");
  });

  // file tools
  test("shows path for file_read", () => {
    expect(getToolArgPreview("file_read", { path: "/src/index.ts" })).toBe(
      "/src/index.ts",
    );
  });

  test("shows file_path for file_write", () => {
    expect(
      getToolArgPreview("file_write", { file_path: "/src/main.go" }),
    ).toBe("/src/main.go");
  });

  test("truncates long file paths", () => {
    const long = "/very/long/".repeat(10) + "file.ts";
    const result = getToolArgPreview("file_read", { path: long });
    expect(result).toHaveLength(60);
    expect(result).toEndWith("...");
  });

  // generic fallback — single string value
  test("shows first string value for unknown tool", () => {
    expect(getToolArgPreview("search", { query: "golang generics" })).toBe(
      "golang generics",
    );
  });

  test("truncates long first string value", () => {
    const long = "x".repeat(80);
    const result = getToolArgPreview("search", { query: long });
    expect(result).toHaveLength(60);
    expect(result).toEndWith("...");
  });

  // generic fallback — key-value pairs
  test("shows key-value pairs for non-string first value with <= 2 entries", () => {
    const result = getToolArgPreview("config", { a: 1, b: "two" });
    expect(result).toContain("a: 1");
    expect(result).toContain("b: two");
  });

  test("shows count for > 2 entries with non-string first value", () => {
    const result = getToolArgPreview("config", {
      a: 1,
      b: { nested: true },
      c: null,
    });
    expect(result).toBe("3 args");
  });

  test("shows first string value for single entry", () => {
    expect(getToolArgPreview("set", { name: "test" })).toBe("test");
  });
});
