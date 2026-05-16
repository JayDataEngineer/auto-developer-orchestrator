import { describe, expect, test } from "bun:test";
import { formatToolResult } from "./format-tool-result";

describe("formatToolResult", () => {
  test("returns empty array for undefined", () => {
    expect(formatToolResult(undefined)).toEqual([]);
  });

  test("returns empty array for null", () => {
    expect(formatToolResult(null)).toEqual([]);
  });

  test("returns empty array for empty string", () => {
    expect(formatToolResult("")).toEqual([]);
  });

  test("returns empty array for whitespace-only string", () => {
    expect(formatToolResult("   \n  ")).toEqual([]);
  });

  test("returns lines from a simple string", () => {
    expect(formatToolResult("hello world")).toEqual(["hello world"]);
  });

  test("splits multiline string into lines", () => {
    expect(formatToolResult("line1\nline2\nline3")).toEqual([
      "line1",
      "line2",
      "line3",
    ]);
  });

  test("filters blank lines", () => {
    expect(formatToolResult("a\n\nb\n\nc")).toEqual(["a", "b", "c"]);
  });

  test("truncates to maxLines with +N indicator", () => {
    const input = "a\nb\nc\nd\ne";
    expect(formatToolResult(input, 3)).toEqual([
      "a",
      "b",
      "c",
      "... +2 more lines",
    ]);
  });

  test("respects custom maxLines", () => {
    const input = "a\nb\nc\nd\ne\nf\ng\nh";
    expect(formatToolResult(input, 5)).toEqual([
      "a",
      "b",
      "c",
      "d",
      "e",
      "... +3 more lines",
    ]);
  });

  test("does not truncate when lines <= maxLines", () => {
    expect(formatToolResult("a\nb\n", 3)).toEqual(["a", "b"]);
  });

  test("normalizes CRLF to LF", () => {
    expect(formatToolResult("a\r\nb\r\nc")).toEqual(["a", "b", "c"]);
  });

  test("normalizes bare CR to LF", () => {
    expect(formatToolResult("a\rb\rc")).toEqual(["a", "b", "c"]);
  });

  test("extracts obj.output", () => {
    expect(formatToolResult({ output: "stdout text" })).toEqual([
      "stdout text",
    ]);
  });

  test("extracts obj.content", () => {
    expect(formatToolResult({ content: "content text" })).toEqual([
      "content text",
    ]);
  });

  test("extracts obj.text", () => {
    expect(formatToolResult({ text: "text value" })).toEqual(["text value"]);
  });

  test("extracts obj.result", () => {
    expect(formatToolResult({ result: "result value" })).toEqual([
      "result value",
    ]);
  });

  test("prefers output over content, text, result", () => {
    expect(
      formatToolResult({ output: "first", text: "second", result: "third" }),
    ).toEqual(["first"]);
  });

  test("falls back to JSON.stringify for unknown objects", () => {
    const result = formatToolResult({ foo: "bar", baz: 42 });
    expect(result).toHaveLength(1);
    expect(result[0]).toContain('"foo"');
  });

  test("stringifies numbers", () => {
    expect(formatToolResult(42)).toEqual(["42"]);
  });

  test("stringifies booleans", () => {
    expect(formatToolResult(true)).toEqual(["true"]);
  });

  test("handles multiline output field", () => {
    expect(formatToolResult({ output: "a\nb\nc\nd" }, 2)).toEqual([
      "a",
      "b",
      "... +2 more lines",
    ]);
  });

  test("falls back to JSON.stringify for object with empty known fields", () => {
    const result = formatToolResult({ output: "", text: "", content: "" });
    expect(result).toHaveLength(1);
    expect(result[0]).toContain('"output"');
  });
});
