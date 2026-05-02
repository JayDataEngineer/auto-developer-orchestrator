import { test, expect, describe } from "bun:test";

// Test paste compression logic (pure function, no React needed)
describe("Paste compression", () => {
  const shouldCollapse = (input: string): boolean => {
    return input.split("\n").length > 3;
  };

  const lineCount = (input: string): number => {
    return input.split("\n").length;
  };

  test("does not collapse 1-3 lines", () => {
    expect(shouldCollapse("hello")).toBe(false);
    expect(shouldCollapse("line1\nline2")).toBe(false);
    expect(shouldCollapse("line1\nline2\nline3")).toBe(false);
  });

  test("collapses 4+ lines", () => {
    expect(shouldCollapse("a\nb\nc\nd")).toBe(true);
    expect(shouldCollapse("a\nb\nc\nd\ne")).toBe(true);
    expect(shouldCollapse("a\nb\nc\nd\ne\nf\ng\nh\ni\nj")).toBe(true);
  });

  test("counts lines correctly", () => {
    expect(lineCount("hello")).toBe(1);
    expect(lineCount("a\nb\nc")).toBe(3);
    expect(lineCount("a\nb\nc\nd")).toBe(4);
    expect(lineCount("")).toBe(1); // empty string = 1 line
  });

  test("64-line paste shows 64", () => {
    const paste = Array(64).fill("line").join("\n");
    expect(lineCount(paste)).toBe(64);
    expect(shouldCollapse(paste)).toBe(true);
  });
});
