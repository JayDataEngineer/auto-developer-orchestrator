/**
 * Tests for ts-tui-pi config and CLI utilities.
 *
 * Run: bun test src/__tests__/config.test.ts
 */

import { describe, test, expect } from "bun:test";

describe("config", () => {
  test("APP_NAME is defined", async () => {
    const { APP_NAME } = await import("../config.js");
    expect(APP_NAME).toBeString();
    expect(APP_NAME).toBe("pi");
  });

  test("VERSION is defined", async () => {
    const { VERSION } = await import("../config.js");
    expect(VERSION).toBeString();
    expect(VERSION).toMatch(/^\d+\.\d+\.\d+/);
  });

  test("CONFIG_DIR_NAME is .pi", async () => {
    const { CONFIG_DIR_NAME } = await import("../config.js");
    expect(CONFIG_DIR_NAME).toBe(".pi");
  });

  test("detectInstallMethod returns a known value", async () => {
    const { detectInstallMethod } = await import("../config.js");
    const method = detectInstallMethod();
    expect(["bun-binary", "npm", "pnpm", "yarn", "bun", "unknown"]).toContain(method);
  });

  test("getUpdateInstruction returns a string", async () => {
    const { getUpdateInstruction } = await import("../config.js");
    const inst = getUpdateInstruction("test-pkg");
    expect(inst).toBeString();
    expect(inst.length).toBeGreaterThan(0);
  });

  test("getPackageDir returns a string", async () => {
    const { getPackageDir } = await import("../config.js");
    const dir = getPackageDir();
    expect(dir).toBeString();
    expect(dir).toContain("ts-tui-pi");
  });

  test("getThemesDir returns a valid path", async () => {
    const { getThemesDir } = await import("../config.js");
    const dir = getThemesDir();
    expect(dir).toBeString();
    expect(dir).toContain("theme");
  });

  test("getAgentDir returns .pi path in home", async () => {
    const { getAgentDir, CONFIG_DIR_NAME } = await import("../config.js");
    const home = process.env.HOME || "/tmp";
    const agentDir = getAgentDir();
    expect(agentDir).toBeString();
    expect(agentDir).toContain(CONFIG_DIR_NAME);
  });

  test("getShareViewerUrl returns URL", async () => {
    const { getShareViewerUrl } = await import("../config.js");
    const url = getShareViewerUrl("test-gist");
    expect(url).toContain("test-gist");
  });
});

describe("CLI argument parsing", () => {
  test("parseArgs defaults are correct", async () => {
    const { parseArgs } = await import("node:util");

    const { values } = parseArgs({
      options: {
        server: { type: "string", default: "http://localhost:3847" },
        project: { type: "string", default: "ts-tui-pi" },
        model: { type: "string", default: "deepseek/deepseek-v4-flash" },
        cwd: { type: "string", default: process.cwd() },
      },
      args: [],
    });

    expect(values.server).toBe("http://localhost:3847");
    expect(values.project).toBe("ts-tui-pi");
    expect(values.model).toBe("deepseek/deepseek-v4-flash");
  });

  test("parseArgs overrides from CLI args", async () => {
    const { parseArgs } = await import("node:util");

    const { values } = parseArgs({
      options: {
        server: { type: "string", default: "http://localhost:3847" },
        project: { type: "string", default: "ts-tui-pi" },
        model: { type: "string", default: "deepseek/deepseek-v4-flash" },
        cwd: { type: "string", default: process.cwd() },
      },
      args: ["--server", "http://other:9999", "--project", "myproj"],
    });

    expect(values.server).toBe("http://other:9999");
    expect(values.project).toBe("myproj");
    expect(values.model).toBe("deepseek/deepseek-v4-flash"); // default
  });
});
