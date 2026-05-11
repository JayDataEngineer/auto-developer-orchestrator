import { describe, test, expect } from "bun:test";
import { migrateKeybindingsConfig } from "../core/keybindings.js";

describe("migrateKeybindingsConfig", () => {
  test("migrates legacy cursorUp to tui.editor.cursorUp", () => {
    const result = migrateKeybindingsConfig({ cursorUp: "ctrl+p" });
    expect(result.migrated).toBe(true);
    expect(result.config["tui.editor.cursorUp"]).toBe("ctrl+p");
    expect(result.config).not.toHaveProperty("cursorUp");
  });

  test("migrates legacy interrupt to app.interrupt", () => {
    const result = migrateKeybindingsConfig({ interrupt: "escape" });
    expect(result.migrated).toBe(true);
    expect(result.config["app.interrupt"]).toBe("escape");
  });

  test("already migrated key is not marked as migrated", () => {
    const result = migrateKeybindingsConfig({ "tui.editor.cursorUp": "ctrl+p" });
    expect(result.migrated).toBe(false);
    expect(result.config["tui.editor.cursorUp"]).toBe("ctrl+p");
  });

  test("new key takes precedence when both legacy and new exist", () => {
    const result = migrateKeybindingsConfig({
      cursorUp: "ctrl+p",
      "tui.editor.cursorUp": "ctrl+shift+p",
    });
    expect(result.migrated).toBe(true);
    expect(result.config["tui.editor.cursorUp"]).toBe("ctrl+shift+p");
  });

  test("empty config returns empty with no migration", () => {
    const result = migrateKeybindingsConfig({});
    expect(result.migrated).toBe(false);
    expect(result.config).toEqual({});
  });

  test("orders keys according to KEYBINDINGS order", () => {
    const result = migrateKeybindingsConfig({
      "app.exit": "ctrl+d",
      "app.interrupt": "escape",
    });
    const keys = Object.keys(result.config);
    expect(keys.indexOf("app.interrupt")).toBeLessThan(keys.indexOf("app.exit"));
  });

  test("extra keys not in KEYBINDINGS are appended at the end", () => {
    const result = migrateKeybindingsConfig({ custom_key: "ctrl+alt+x" });
    const keys = Object.keys(result.config);
    expect(keys[keys.length - 1]).toBe("custom_key");
  });

  test("migrates multiple legacy keys at once", () => {
    const result = migrateKeybindingsConfig({
      cursorUp: "ctrl+p",
      cursorDown: "ctrl+n",
      interrupt: "escape",
      clear: "ctrl+c",
    });
    expect(result.migrated).toBe(true);
    expect(result.config["tui.editor.cursorUp"]).toBe("ctrl+p");
    expect(result.config["tui.editor.cursorDown"]).toBe("ctrl+n");
    expect(result.config["app.interrupt"]).toBe("escape");
    expect(result.config["app.clear"]).toBe("ctrl+c");
    expect(result.config).not.toHaveProperty("cursorUp");
    expect(result.config).not.toHaveProperty("cursorDown");
  });

  test("handles array keybindings", () => {
    const result = migrateKeybindingsConfig({
      "app.exit": ["ctrl+d", "ctrl+x"],
    });
    expect(result.config["app.exit"]).toEqual(["ctrl+d", "ctrl+x"]);
  });
});
