import { describe, test, expect } from "bun:test";
import { parseArgs, isValidThinkingLevel } from "../cli/args.js";

describe("isValidThinkingLevel", () => {
  test("valid levels return true", () => {
    expect(isValidThinkingLevel("off")).toBe(true);
    expect(isValidThinkingLevel("minimal")).toBe(true);
    expect(isValidThinkingLevel("low")).toBe(true);
    expect(isValidThinkingLevel("medium")).toBe(true);
    expect(isValidThinkingLevel("high")).toBe(true);
    expect(isValidThinkingLevel("xhigh")).toBe(true);
  });

  test("invalid levels return false", () => {
    expect(isValidThinkingLevel("none")).toBe(false);
    expect(isValidThinkingLevel("extreme")).toBe(false);
    expect(isValidThinkingLevel("")).toBe(false);
    expect(isValidThinkingLevel("ultra")).toBe(false);
  });
});

describe("parseArgs", () => {
  test("no args returns defaults", () => {
    const result = parseArgs([]);
    expect(result.help).toBeUndefined();
    expect(result.version).toBeUndefined();
    expect(result.mode).toBeUndefined();
    expect(result.messages).toEqual([]);
    expect(result.fileArgs).toEqual([]);
    expect(result.diagnostics).toEqual([]);
    expect(result.unknownFlags.size).toBe(0);
  });

  test("--help", () => {
    expect(parseArgs(["--help"]).help).toBe(true);
    expect(parseArgs(["-h"]).help).toBe(true);
  });

  test("--version", () => {
    expect(parseArgs(["--version"]).version).toBe(true);
    expect(parseArgs(["-v"]).version).toBe(true);
  });

  test("--mode text/json/rpc", () => {
    expect(parseArgs(["--mode", "text"]).mode).toBe("text");
    expect(parseArgs(["--mode", "json"]).mode).toBe("json");
    expect(parseArgs(["--mode", "rpc"]).mode).toBe("rpc");
  });

  test("--mode with invalid value is ignored", () => {
    expect(parseArgs(["--mode", "invalid"]).mode).toBeUndefined();
  });

  test("--continue and -c", () => {
    expect(parseArgs(["--continue"]).continue).toBe(true);
    expect(parseArgs(["-c"]).continue).toBe(true);
  });

  test("--resume and -r", () => {
    expect(parseArgs(["--resume"]).resume).toBe(true);
    expect(parseArgs(["-r"]).resume).toBe(true);
  });

  test("--provider", () => {
    expect(parseArgs(["--provider", "anthropic"]).provider).toBe("anthropic");
  });

  test("--model", () => {
    expect(parseArgs(["--model", "claude-4"]).model).toBe("claude-4");
  });

  test("--api-key", () => {
    expect(parseArgs(["--api-key", "sk-xxx"]).apiKey).toBe("sk-xxx");
  });

  test("--thinking with valid level", () => {
    expect(parseArgs(["--thinking", "high"]).thinking).toBe("high");
  });

  test("--thinking with invalid level adds diagnostic", () => {
    const result = parseArgs(["--thinking", "bogus"]);
    expect(result.thinking).toBeUndefined();
    expect(result.diagnostics.length).toBeGreaterThan(0);
    expect(result.diagnostics[0].type).toBe("warning");
    expect(result.diagnostics[0].message).toContain("bogus");
  });

  test("--no-tools", () => {
    expect(parseArgs(["--no-tools"]).noTools).toBe(true);
  });

  test("--tools with valid names", () => {
    const result = parseArgs(["--tools", "read,bash"]);
    expect(result.tools).toEqual(["read", "bash"]);
  });

  test("--tools with invalid name adds diagnostic", () => {
    const result = parseArgs(["--tools", "read,bogus,bash"]);
    expect(result.tools).toEqual(["read", "bash"]);
    expect(result.diagnostics.length).toBeGreaterThan(0);
  });

  test("--print and -p", () => {
    expect(parseArgs(["--print"]).print).toBe(true);
    expect(parseArgs(["-p"]).print).toBe(true);
  });

  test("--export", () => {
    expect(parseArgs(["--export", "output.html"]).export).toBe("output.html");
  });

  test("--extension and -e", () => {
    expect(parseArgs(["-e", "ext1", "--extension", "ext2"]).extensions).toEqual(["ext1", "ext2"]);
  });

  test("--no-extensions and -ne", () => {
    expect(parseArgs(["--no-extensions"]).noExtensions).toBe(true);
    expect(parseArgs(["-ne"]).noExtensions).toBe(true);
  });

  test("--skill", () => {
    expect(parseArgs(["--skill", "path/to/skill"]).skills).toEqual(["path/to/skill"]);
  });

  test("--no-skills and -ns", () => {
    expect(parseArgs(["--no-skills"]).noSkills).toBe(true);
    expect(parseArgs(["-ns"]).noSkills).toBe(true);
  });

  test("--verbose", () => {
    expect(parseArgs(["--verbose"]).verbose).toBe(true);
  });

  test("--offline", () => {
    expect(parseArgs(["--offline"]).offline).toBe(true);
  });

  test("@file args", () => {
    const result = parseArgs(["@file1.md", "@file2.ts"]);
    expect(result.fileArgs).toEqual(["file1.md", "file2.ts"]);
  });

  test("positional messages", () => {
    const result = parseArgs(["hello", "world"]);
    expect(result.messages).toEqual(["hello", "world"]);
  });

  test("mixes messages, files, and flags", () => {
    const result = parseArgs(["--verbose", "@context.md", "do", "the", "thing"]);
    expect(result.verbose).toBe(true);
    expect(result.fileArgs).toEqual(["context.md"]);
    expect(result.messages).toEqual(["do", "the", "thing"]);
  });

  test("--list-models without args", () => {
    expect(parseArgs(["--list-models"]).listModels).toBe(true);
  });

  test("--list-models with search term", () => {
    expect(parseArgs(["--list-models", "sonnet"]).listModels).toBe("sonnet");
  });

  test("--session", () => {
    expect(parseArgs(["--session", "session-123"]).session).toBe("session-123");
  });

  test("--fork", () => {
    expect(parseArgs(["--fork", "session-abc"]).fork).toBe("session-abc");
  });

  test("--no-session", () => {
    expect(parseArgs(["--no-session"]).noSession).toBe(true);
  });

  test("unknown --flag with value", () => {
    const result = parseArgs(["--custom-flag", "val"]);
    expect(result.unknownFlags.get("custom-flag")).toBe("val");
  });

  test("unknown --flag without value", () => {
    const result = parseArgs(["--boolean-flag"]);
    expect(result.unknownFlags.get("boolean-flag")).toBe(true);
  });

  test("unknown --flag=value syntax", () => {
    const result = parseArgs(["--custom=val"]);
    expect(result.unknownFlags.get("custom")).toBe("val");
  });

  test("--session-dir", () => {
    expect(parseArgs(["--session-dir", "/tmp/sessions"]).sessionDir).toBe("/tmp/sessions");
  });

  test("--models", () => {
    expect(parseArgs(["--models", "sonnet,haiku"]).models).toEqual(["sonnet", "haiku"]);
  });

  test("--no-themes", () => {
    expect(parseArgs(["--no-themes"]).noThemes).toBe(true);
  });

  test("--prompt-template", () => {
    expect(parseArgs(["--prompt-template", "my-template.md"]).promptTemplates).toEqual(["my-template.md"]);
  });

  test("--theme", () => {
    expect(parseArgs(["--theme", "my-theme.json"]).themes).toEqual(["my-theme.json"]);
  });

  test("--system-prompt", () => {
    expect(parseArgs(["--system-prompt", "you are a bot"]).systemPrompt).toBe("you are a bot");
  });

  test("--append-system-prompt", () => {
    expect(parseArgs(["--append-system-prompt", "be nice"]).appendSystemPrompt).toBe("be nice");
  });
});
