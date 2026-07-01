// Unit tests for pux-org-loader. Drives the extension factory with a stub
// ExtensionAPI and exercises the registered hooks against the shipped
// orgs/_demo/ substrate. No LLM, no Docker, no network.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { Type } from "typebox";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { rm, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";

type Hook = (event: any, ctx: any) => Promise<any | undefined>;

interface StubAPI {
  flags: Record<string, unknown>;
  hooks: Map<string, Hook[]>;
  activeTools: string[] | null;
  flagRegistrations: { name: string; opts: any }[];
}

function makeStubAPI(): ExtensionAPI {
  const stub: StubAPI = {
    flags: {},
    hooks: new Map(),
    activeTools: null,
    flagRegistrations: [],
  };

  const apiMethods: Record<string, (...args: any[]) => any> = {
    registerFlag(name: string, opts: any) {
      stub.flagRegistrations.push({ name, opts });
      if (opts && "default" in opts) stub.flags[name] = opts.default;
    },
    getFlag(name: string) {
      return stub.flags[name];
    },
    on(event: string, handler: Hook) {
      const list = stub.hooks.get(event) ?? [];
      list.push(handler);
      stub.hooks.set(event, list);
    },
    getActiveTools() {
      return stub.activeTools ?? ["bash", "file_read", "file_write", "python", "describe_image", "browser_navigate"];
    },
    setActiveTools(names: string[]) {
      stub.activeTools = names;
    },
    registerTool() {
      // Phase 4
    },
  };

  const api = Object.assign(apiMethods, { __stub: stub }) as unknown as ExtensionAPI;
  return api;
}

async function fire(stub: any, event: string, ev: any, ctx: any) {
  const hooks = stub.hooks.get(event) ?? [];
  let acc = ev;
  for (const h of hooks) {
    const ret = await h(acc, ctx);
    if (ret && typeof ret === "object") acc = { ...acc, ...ret };
  }
  return acc;
}

async function withTempProject<T>(fn: (root: string) => Promise<T>): Promise<T> {
  const root = await mkdtemp(join(tmpdir(), "pux-org-test-"));
  try {
    return await fn(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

async function writeOrg(
  root: string,
  name: string,
  opts: { ctoTools?: string[]; ctoBody?: string; omitCtoBlock?: boolean } = {},
) {
  const orgDir = join(root, "orgs", name);
  await mkdir(orgDir, { recursive: true });
  await mkdir(join(orgDir, "roles"), { recursive: true });
  const tools = (opts.ctoTools ?? ["bash", "file_read"]).map((t) => '"' + t + '"').join(", ");
  const lines = [
    'name = "' + name + '"',
    'description = "test org"',
    "",
  ];
  if (!opts.omitCtoBlock) {
    lines.push("[cto]");
    lines.push('prompt = "cto.md"');
    lines.push("max_rounds = 30");
    lines.push("tools = [" + tools + "]");
  }
  await writeFile(join(orgDir, "org.toml"), lines.join("\n") + "\n");
  await writeFile(join(orgDir, "cto.md"), opts.ctoBody ?? "# CTO for " + name + "\n\nTest body.");
}

const importFresh = async () => import("./index.ts").then((m) => m.default);

beforeEach(() => {
  // Tests resolve orgs/<name>/ relative to cwd; project root is 4 levels up
  // from .pi/extensions/pux-org-loader/index.test.ts.
  process.chdir(join(__dirname, "..", "..", ".."));
});

afterEach(() => {
  delete process.env.PUX_ORG_TEST_ROOT;
});

describe("pux-org-loader", () => {
  it("registers --org flag with description + empty default", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    const reg = (api as any).__stub.flagRegistrations.find((r: any) => r.name === "org");
    expect(reg).toBeDefined();
    expect(reg.opts.type).toBe("string");
    expect(reg.opts.default).toBe("");
    expect(reg.opts.description).toMatch(/org mode/i);
  });

  it("before_agent_start: appends CTO body from orgs/_demo/cto.md when --org=_demo", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    (api as any).__stub.flags.org = "_demo";

    const ev = { systemPrompt: "BASE", prompt: "say hi" };
    const result = await fire((api as any).__stub, "before_agent_start", ev, { ui: { notify() {} } });
    expect(result.systemPrompt).toContain("BASE");
    expect(result.systemPrompt).toContain("Demo CTO");
    expect(result.systemPrompt).toContain("Org: _demo");
  });

  it("before_agent_start: no-op when --org is empty", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    (api as any).__stub.flags.org = "";

    const ev = { systemPrompt: "BASE", prompt: "hi" };
    const result = await fire((api as any).__stub, "before_agent_start", ev, { ui: { notify() {} } });
    expect(result.systemPrompt).toBe("BASE");
  });

  it("before_agent_start: no-op + logs error when org TOML is missing", async () => {
    await withTempProject(async (root) => {
      process.chdir(root);
      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "nonexistent";

      const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
      const ev = { systemPrompt: "BASE", prompt: "hi" };
      const result = await fire((api as any).__stub, "before_agent_start", ev, { ui: { notify() {} } });
      expect(result.systemPrompt).toBe("BASE");
      expect(errSpy).toHaveBeenCalled();
      errSpy.mockRestore();
    });
  });

  it("session_start: applies the CTO whitelist via setActiveTools", async () => {
    await withTempProject(async (root) => {
      await writeOrg(root, "wl-test", { ctoTools: ["bash", "file_read", "python"] });
      process.chdir(root);

      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "wl-test";

      await fire((api as any).__stub, "session_start", { reason: "startup" }, { ui: { notify() {} } });
      expect((api as any).__stub.activeTools).toEqual(["bash", "file_read", "python"]);
    });
  });

  it("session_start: filters 'delegate_to' from the whitelist (Phase 4 tool not yet registered)", async () => {
    await withTempProject(async (root) => {
      await writeOrg(root, "del-test", { ctoTools: ["bash", "delegate_to"] });
      process.chdir(root);

      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "del-test";

      await fire((api as any).__stub, "session_start", { reason: "startup" }, { ui: { notify() {} } });
      expect((api as any).__stub.activeTools).toEqual(["bash"]);
    });
  });

  it("tool_call: blocks a tool outside the whitelist", async () => {
    await withTempProject(async (root) => {
      await writeOrg(root, "block-test", { ctoTools: ["bash"] });
      process.chdir(root);

      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "block-test";

      const result = await fire((api as any).__stub, "tool_call", {
        toolName: "file_write",
        toolCallId: "1",
        input: {},
      }, { ui: { notify() {} } });
      expect(result.block).toBe(true);
      expect(result.reason).toMatch(/not in the block-test CTO whitelist/);
    });
  });

  it("tool_call: allows a whitelisted tool", async () => {
    await withTempProject(async (root) => {
      await writeOrg(root, "allow-test", { ctoTools: ["bash", "file_read"] });
      process.chdir(root);

      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "allow-test";

      const result = await fire((api as any).__stub, "tool_call", {
        toolName: "bash",
        toolCallId: "1",
        input: { command: "ls" },
      }, { ui: { notify() {} } });
      expect(result.block).toBeUndefined();
    });
  });

  it("before_agent_start: errors loudly when [cto] block is missing", async () => {
    await withTempProject(async (root) => {
      await writeOrg(root, "no-cto", { omitCtoBlock: true });
      process.chdir(root);

      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "no-cto";

      const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
      const result = await fire((api as any).__stub, "before_agent_start", { systemPrompt: "BASE", prompt: "hi" }, { ui: { notify() {} } });
      expect(result.systemPrompt).toBe("BASE");
      const msgs = errSpy.mock.calls.map((c) => String(c[0]));
      expect(msgs.some((m) => m.includes("missing [cto] block"))).toBe(true);
      errSpy.mockRestore();
    });
  });
});

// Type-box runtime check sanity (ensures typebox dep is wired)
describe("typebox dep", () => {
  it("Type.Object constructs schemas", () => {
    const s = Type.Object({ name: Type.String() });
    expect(s.type).toBe("object");
  });
});

// Import vitest globals used inside withTempProject blocks
import { vi } from "vitest";
