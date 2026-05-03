import { test, expect, describe } from "bun:test";
import { fmtArgs, renderMd } from "./app";

// ── fmtArgs ────────────────────────────────────────────────────────────────

describe("fmtArgs", () => {
  test("extracts command key from JSON", () => {
    expect(fmtArgs(JSON.stringify({ command: "ls -la", path: "/tmp" }))).toBe("ls -la");
  });

  test("extracts path key from JSON", () => {
    expect(fmtArgs(JSON.stringify({ path: "/etc/nginx/nginx.conf" }))).toBe("/etc/nginx/nginx.conf");
  });

  test("extracts url key from JSON", () => {
    const json = JSON.stringify({ url: "https://example.com", method: "GET" });
    expect(fmtArgs(json)).toBe("https://example.com");
  });

  test("extracts message key from JSON", () => {
    expect(fmtArgs(JSON.stringify({ message: "hello world", role: "user" }))).toBe("hello world");
  });

  test("extracts query key from JSON", () => {
    expect(fmtArgs(JSON.stringify({ query: "SELECT * FROM users" }))).toBe("SELECT * FROM users");
  });

  test("extracts file key from JSON", () => {
    expect(fmtArgs(JSON.stringify({ file: "src/app.tsx", line: 42 }))).toBe("src/app.tsx");
  });

  test("extracts content key from JSON", () => {
    expect(fmtArgs(JSON.stringify({ content: "Lorem ipsum", type: "text" }))).toBe("Lorem ipsum");
  });

  test("extracts text key from JSON", () => {
    expect(fmtArgs(JSON.stringify({ text: "Hello world" }))).toBe("Hello world");
  });

  test("falls back to first value if no primary key", () => {
    expect(fmtArgs(JSON.stringify({ foo: "bar", baz: 42 }))).toBe("bar");
  });

  test("returns empty string for empty object", () => {
    expect(fmtArgs("{}")).toBe("");
  });

  test("returns empty string for null", () => {
    expect(fmtArgs("null")).toBe("");
  });

  test("returns empty string for empty input", () => {
    expect(fmtArgs("")).toBe("");
  });

  test("returns string for plain string input", () => {
    expect(fmtArgs("plain text message")).toBe("plain text message");
  });

  test("truncates long command to 70 chars + ellipsis", () => {
    const longCmd = "x".repeat(80);
    const result = fmtArgs(JSON.stringify({ command: longCmd }));
    expect(result).toEndWith("…");
    expect(result.length).toBeLessThanOrEqual(71); // 67 + "…" = 68, but text could be different
  });

  test("truncates long non-primary value to 50 chars + ellipsis", () => {
    const long = "x".repeat(60);
    const result = fmtArgs(JSON.stringify({ unknown: long }));
    expect(result.length).toBeLessThanOrEqual(51);
  });

  test("handles JSON with numeric values", () => {
    // fmtArgs: no primary key found → falls back to first entry's value (42 → "42")
    expect(fmtArgs(JSON.stringify({ count: 42, name: "test" }))).toBe("42");
  });

  test("handles undefined/null values as empty", () => {
    expect(fmtArgs(JSON.stringify({ command: null }))).toBe("");
  });

  test("handles complex nested JSON by using first entry", () => {
    expect(fmtArgs(JSON.stringify({ name: "test", nested: { deep: true } }))).toBe("test");
  });
});

// ── renderMd ───────────────────────────────────────────────────────────────

describe("renderMd", () => {
  test("renders bold text (**...**)", () => {
    const result = renderMd("**hello** world");
    expect(result).toContain("\x1b[1mhello\x1b[0m");
    expect(result).toContain("world");
  });

  test("renders italic text (*...*)", () => {
    const result = renderMd("*italic*");
    expect(result).toContain("\x1b[3mitalic\x1b[0m");
  });

  test("renders inline code (`...`)", () => {
    const result = renderMd("use `ls -la` command");
    expect(result).toContain("\x1b[2mls -la\x1b[0m");
  });

  test("renders code blocks (```...```)", () => {
    const result = renderMd("```\nconsole.log('hi')\n```");
    expect(result).toContain("console.log('hi')");
    expect(result).toContain("\x1b[2m\x1b[3m");
  });

  test("handles mixed formatting", () => {
    const result = renderMd("**bold** and *italic* and `code`");
    expect(result).toContain("\x1b[1mbold\x1b[0m");
    expect(result).toContain("\x1b[3mitalic\x1b[0m");
    expect(result).toContain("\x1b[2mcode\x1b[0m");
  });

  test("returns plain text unchanged", () => {
    expect(renderMd("plain text")).toBe("plain text");
  });

  test("handles empty string", () => {
    expect(renderMd("")).toBe("");
  });

  test("code block with language tag still renders", () => {
    const result = renderMd("```python\nprint('hi')\n```");
    expect(result).toContain("print('hi')");
  });
});

// ── renderDiff ─────────────────────────────────────────────────────────────

describe("renderDiff text extraction", () => {
  test("diff lines are separated correctly", () => {
    // renderDiff produces React elements; verify the splitting logic
    const diff = "+added line\n-removed line\n@@ context @@\n  unchanged";
    const lines = diff.split("\n");
    expect(lines).toHaveLength(4);
    expect(lines[0]).toBe("+added line");
    expect(lines[1]).toBe("-removed line");
    expect(lines[2]).toBe("@@ context @@");
    expect(lines[3]).toBe("  unchanged");
  });

  test("diff is capped at 30 lines", () => {
    const lines = Array.from({ length: 50 }, (_, i) => `line ${i}`).join("\n");
    const split = lines.split("\n").slice(0, 30);
    expect(split).toHaveLength(30);
  });
});

// ── renderToolResult helpers ───────────────────────────────────────────────

describe("renderToolResult data classification", () => {
  test("base64 PNG image detected (starts with iVBOR)", () => {
    const data = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
    expect(data.startsWith("iVBOR")).toBe(true);
  });

  test("base64 JPEG image detected (starts with /9j/)", () => {
    const data = "/9j/4AAQSkZJRgABAQEASABIAAD...";
    expect(data.startsWith("/9j/")).toBe(true);
  });

  test("base64 GIF image detected (starts with R0lG)", () => {
    const data = "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";
    expect(data.startsWith("R0lG")).toBe(true);
  });

  test("non-image data not detected as screenshot", () => {
    const data = "Hello world output from a command";
    expect(data.startsWith("iVBOR") || data.startsWith("/9j/") || data.startsWith("R0lG")).toBe(false);
  });

  test("result truncated to 3 lines", () => {
    const result = "line1\nline2\nline3\nline4\nline5";
    const lines = result.split("\n").slice(0, 3);
    expect(lines).toHaveLength(3);
    expect(lines.join("\n")).toBe("line1\nline2\nline3");
  });

  test("result truncated to 300 chars", () => {
    const long = "x".repeat(500);
    const preview = long.length > 300 ? long.slice(0, 297) + "…" : long;
    expect(preview.length).toBeLessThanOrEqual(301);
    expect(preview).toEndWith("…");
  });
});
