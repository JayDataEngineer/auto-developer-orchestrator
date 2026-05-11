import { describe, test, expect } from "bun:test";
import {
  bashExecutionToText,
  createBranchSummaryMessage,
  createCompactionSummaryMessage,
  createCustomMessage,
  convertToLlm,
  COMPACTION_SUMMARY_PREFIX,
  COMPACTION_SUMMARY_SUFFIX,
  BRANCH_SUMMARY_PREFIX,
  BRANCH_SUMMARY_SUFFIX,
  type BashExecutionMessage,
} from "../core/messages.js";

describe("bashExecutionToText", () => {
  test("formats command with output", () => {
    const msg: BashExecutionMessage = {
      role: "bashExecution",
      command: "ls",
      output: "file1\nfile2",
      exitCode: 0,
      cancelled: false,
      truncated: false,
      timestamp: 1000,
    };
    expect(bashExecutionToText(msg)).toBe("Ran `ls`\n```\nfile1\nfile2\n```");
  });

  test("shows (no output) when output is empty", () => {
    const msg: BashExecutionMessage = {
      role: "bashExecution",
      command: "echo hi",
      output: "",
      exitCode: 0,
      cancelled: false,
      truncated: false,
      timestamp: 1000,
    };
    const text = bashExecutionToText(msg);
    expect(text).toContain("(no output)");
    expect(text).not.toContain("```");
  });

  test("appends cancelled note", () => {
    const msg: BashExecutionMessage = {
      role: "bashExecution",
      command: "sleep 10",
      output: "",
      exitCode: undefined,
      cancelled: true,
      truncated: false,
      timestamp: 1000,
    };
    expect(bashExecutionToText(msg)).toContain("(command cancelled)");
  });

  test("appends non-zero exit code", () => {
    const msg: BashExecutionMessage = {
      role: "bashExecution",
      command: "bad-command",
      output: "error output",
      exitCode: 1,
      cancelled: false,
      truncated: false,
      timestamp: 1000,
    };
    const text = bashExecutionToText(msg);
    expect(text).toContain("Command exited with code 1");
  });

  test("appends truncation notice with fullOutputPath", () => {
    const msg: BashExecutionMessage = {
      role: "bashExecution",
      command: "make",
      output: "lots of output...",
      exitCode: 0,
      cancelled: false,
      truncated: true,
      fullOutputPath: "/tmp/full-output.log",
      timestamp: 1000,
    };
    const text = bashExecutionToText(msg);
    expect(text).toContain("[Output truncated. Full output: /tmp/full-output.log]");
  });

  test("does not append truncation notice when path missing", () => {
    const msg: BashExecutionMessage = {
      role: "bashExecution",
      command: "make",
      output: "lots of output...",
      exitCode: 0,
      cancelled: false,
      truncated: true,
      timestamp: 1000,
    };
    const text = bashExecutionToText(msg);
    expect(text).not.toContain("Output truncated");
  });
});

describe("createBranchSummaryMessage", () => {
  test("returns correct shape", () => {
    const msg = createBranchSummaryMessage("branch summary text", "branch-123", "2025-01-15T10:00:00Z");
    expect(msg.role).toBe("branchSummary");
    expect(msg.summary).toBe("branch summary text");
    expect(msg.fromId).toBe("branch-123");
    expect(typeof msg.timestamp).toBe("number");
  });
});

describe("createCompactionSummaryMessage", () => {
  test("returns correct shape", () => {
    const msg = createCompactionSummaryMessage("compacted text", 50000, "2025-01-15T10:00:00Z");
    expect(msg.role).toBe("compactionSummary");
    expect(msg.summary).toBe("compacted text");
    expect(msg.tokensBefore).toBe(50000);
    expect(typeof msg.timestamp).toBe("number");
  });
});

describe("createCustomMessage", () => {
  test("with string content", () => {
    const msg = createCustomMessage("my-type", "hello", true, { key: "val" }, "2025-01-15T10:00:00Z");
    expect(msg.role).toBe("custom");
    expect(msg.customType).toBe("my-type");
    expect(msg.content).toBe("hello");
    expect(msg.display).toBe(true);
    expect(msg.details).toEqual({ key: "val" });
  });

  test("with array content", () => {
    const content = [{ type: "text" as const, text: "hi" }];
    const msg = createCustomMessage("my-type", content, false, undefined, "2025-01-15T10:00:00Z");
    expect(msg.content).toBe(content);
    expect(msg.display).toBe(false);
    expect(msg.details).toBeUndefined();
  });
});

describe("convertToLlm", () => {
  test("converts bashExecution to user message", () => {
    const result = convertToLlm([
      { role: "bashExecution", command: "ls", output: "file1", exitCode: 0, cancelled: false, truncated: false, timestamp: 1 },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0].role).toBe("user");
    expect(result[0].content).toEqual([{ type: "text", text: "Ran `ls`\n```\nfile1\n```" }]);
  });

  test("filters out bashExecution with excludeFromContext", () => {
    const result = convertToLlm([
      { role: "bashExecution", command: "secret", output: "", exitCode: 0, cancelled: false, truncated: false, timestamp: 1, excludeFromContext: true },
    ]);
    expect(result).toHaveLength(0);
  });

  test("converts custom message with string content", () => {
    const result = convertToLlm([
      { role: "custom", customType: "note", content: "my note", display: true, timestamp: 1 },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0].role).toBe("user");
    expect(result[0].content).toEqual([{ type: "text", text: "my note" }]);
  });

  test("converts branchSummary message", () => {
    const result = convertToLlm([
      { role: "branchSummary", summary: "branch work", fromId: "abc", timestamp: 1 },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0].content[0].text).toContain("branch work");
    expect(result[0].content[0].text).toContain(BRANCH_SUMMARY_PREFIX);
    expect(result[0].content[0].text).toContain(BRANCH_SUMMARY_SUFFIX);
  });

  test("converts compactionSummary message", () => {
    const result = convertToLlm([
      { role: "compactionSummary", summary: "compacted", tokensBefore: 100, timestamp: 1 },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0].content[0].text).toContain("compacted");
    expect(result[0].content[0].text).toContain(COMPACTION_SUMMARY_PREFIX);
    expect(result[0].content[0].text).toContain(COMPACTION_SUMMARY_SUFFIX);
  });

  test("passes through user, assistant, toolResult as-is", () => {
    const result = convertToLlm([
      { role: "user", content: [{ type: "text", text: "hi" }] },
      { role: "assistant", content: [{ type: "text", text: "hello" }] },
    ]);
    expect(result).toHaveLength(2);
    expect(result[0].role).toBe("user");
    expect(result[1].role).toBe("assistant");
  });

  test("empty array returns empty", () => {
    expect(convertToLlm([])).toEqual([]);
  });
});
