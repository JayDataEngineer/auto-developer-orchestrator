import { describe, test, expect } from "bun:test";
import { fuzzyMatch, fuzzyFilter } from "../tui/fuzzy.js";

describe("fuzzyMatch", () => {
  test("empty query matches everything with score 0", () => {
    expect(fuzzyMatch("", "anything")).toEqual({ matches: true, score: 0 });
  });

  test("exact match is a match", () => {
    const result = fuzzyMatch("hello", "hello");
    expect(result.matches).toBe(true);
  });

  test("case insensitive match", () => {
    expect(fuzzyMatch("HELLO", "hello").matches).toBe(true);
    expect(fuzzyMatch("hello", "HELLO").matches).toBe(true);
  });

  test("substring match when chars in order", () => {
    expect(fuzzyMatch("hlo", "hello").matches).toBe(true);
  });

  test("no match when chars out of order", () => {
    expect(fuzzyMatch("hlleo", "hello").matches).toBe(false);
  });

  test("no match when query char not in text", () => {
    expect(fuzzyMatch("z", "hello").matches).toBe(false);
  });

  test("query longer than text does not match", () => {
    expect(fuzzyMatch("hello world", "hello").matches).toBe(false);
  });

  test("consecutive matches get better (lower) score", () => {
    const consecutive = fuzzyMatch("bcd", "abcd");
    const gapped = fuzzyMatch("bcd", "abxcd");
    expect(consecutive.score).toBeLessThan(gapped.score);
  });

  test("word boundary matches get better (lower) score", () => {
    const boundary = fuzzyMatch("api", "api-test");
    const noBoundary = fuzzyMatch("api", "tapir");
    expect(boundary.score).toBeLessThan(noBoundary.score);
  });

  test("alpha-numeric swap improves match", () => {
    const direct = fuzzyMatch("abc123", "123abc");
    expect(direct.matches).toBe(true);
  });

  test("alpha-numeric swap still matches original order", () => {
    const result1 = fuzzyMatch("abc123", "abc123");
    const result2 = fuzzyMatch("abc123", "123abc");
    expect(result1.matches).toBe(true);
    expect(result2.matches).toBe(true);
  });
});

describe("fuzzyFilter", () => {
  const items = ["apple", "banana", "apricot", "grape", "avocado"];

  test("empty query returns all items in original order", () => {
    expect(fuzzyFilter(items, "", (s) => s)).toEqual(items);
  });

  test("filters by single token", () => {
    const result = fuzzyFilter(items, "ap", (s) => s);
    expect(result).toEqual(["apple", "apricot", "grape"]);
    expect(result).not.toContain("banana");
  });

  test("multi-token requires all tokens to match", () => {
    const result = fuzzyFilter(items, "ap o", (s) => s);
    expect(result).toEqual(["apricot"]);
  });

  test("no matches returns empty array", () => {
    expect(fuzzyFilter(items, "xyz", (s) => s)).toEqual([]);
  });

  test("results sorted by best score first", () => {
    const result = fuzzyFilter(items, "an", (s) => s);
    expect(result).toEqual(["banana"]);
  });

  test("works with object accessor", () => {
    const objs = [{ name: "foo" }, { name: "bar" }, { name: "baz" }];
    const result = fuzzyFilter(objs, "ba", (o) => o.name);
    expect(result).toHaveLength(2);
    expect(result[0].name).toBe("bar");
    expect(result[1].name).toBe("baz");
  });
});
