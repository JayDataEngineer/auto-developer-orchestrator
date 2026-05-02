import { test, expect, describe } from "bun:test";
import {
  imageProtocol,
  renderImage,
  imagePlaceholder,
  hasImageSupport,
} from "./terminal-image";

describe("terminal-image", () => {
  test("imageProtocol returns a value without throwing", () => {
    const proto = imageProtocol();
    expect(proto === "kitty" || proto === "iterm2" || proto === null).toBe(true);
  });

  test("hasImageSupport matches protocol", () => {
    const proto = imageProtocol();
    expect(hasImageSupport()).toBe(proto !== null);
  });

  test("renderImage returns null for empty data", () => {
    expect(renderImage("")).toBeNull();
  });

  test("imagePlaceholder works with valid PNG header", () => {
    // Minimal 1x1 red PNG as base64
    const fakePng = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==";
    const result = imagePlaceholder(fakePng, "test");
    expect(result).toContain("test");
    expect(result).toContain("1x1");
  });

  test("imagePlaceholder works with non-image data", () => {
    const result = imagePlaceholder("not-an-image", "label");
    expect(result).toContain("label");
  });

  test("renderImage works on any terminal (returns null or sequence)", () => {
    const fakePng = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==";
    const result = renderImage(fakePng);
    // Either returns null (no image support) or a string with escape sequences
    if (result !== null) {
      expect(typeof result).toBe("string");
      // Should contain kitty or iterm2 prefix
      expect(result.includes("\x1b_G") || result.includes("\x1b]1337")).toBe(true);
    }
  });
});
