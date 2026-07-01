// Unit tests for pux-org-loader. Drives the extension factory with a stub
// ExtensionAPI and exercises the registered hooks against the shipped
// orgs/_demo/AGENTS.md substrate. No LLM, no Docker, no network.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

type Hook = (event: any, ctx: any) => Promise<any | undefined>;

interface StubAPI {
  flags: Record<string, unknown>;
  hooks: Map<string, Hook[]>;
  flagRegistrations: { name: string; opts: any }[];
}

function makeStubAPI(): ExtensionAPI {
  const stub: StubAPI = {
    flags: {},
    hooks: new Map(),
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
  };

  return Object.assign(apiMethods, { __stub: stub }) as unknown as ExtensionAPI;
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

async function writeAgentsMd(root: string, orgName: string, body: string) {
  const orgDir = join(root, "orgs", orgName);
  await mkdir(orgDir, { recursive: true });
  await writeFile(join(orgDir, "AGENTS.md"), body);
}

const importFresh = () => import("./index.ts").then((m) => m.default);

beforeEach(() => {
  // Project root is 4 levels up from .pi/extensions/pux-org-loader/index.test.ts.
  process.chdir(join(__dirname, "..", "..", ".."));
});

afterEach(() => {
  vi.restoreAllMocks();
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

  it("before_agent_start: appends orgs/_demo/AGENTS.md body when --org=_demo", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    (api as any).__stub.flags.org = "_demo";

    const ev = { systemPrompt: "BASE", prompt: "say hi" };
    const result = await fire(
      (api as any).__stub,
      "before_agent_start",
      ev,
      { ui: { notify() {} } },
    );
    expect(result.systemPrompt).toContain("BASE");
    expect(result.systemPrompt).toContain("Org: _demo");
    expect(result.systemPrompt).toContain("Demo Org");
    expect(result.systemPrompt).toContain("CTO Overlay");
  });

  it("before_agent_start: no-op when --org is empty", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    (api as any).__stub.flags.org = "";

    const ev = { systemPrompt: "BASE", prompt: "hi" };
    const result = await fire(
      (api as any).__stub,
      "before_agent_start",
      ev,
      { ui: { notify() {} } },
    );
    expect(result.systemPrompt).toBe("BASE");
  });

  it("before_agent_start: no-op + logs error when orgs/<name>/AGENTS.md is missing", async () => {
    await withTempProject(async (root) => {
      process.chdir(root);
      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "nonexistent";

      const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
      const ev = { systemPrompt: "BASE", prompt: "hi" };
      const result = await fire(
        (api as any).__stub,
        "before_agent_start",
        ev,
        { ui: { notify() {} } },
      );
      expect(result.systemPrompt).toBe("BASE");
      expect(errSpy).toHaveBeenCalled();
      const msgs = errSpy.mock.calls.map((c) => String(c[0]));
      expect(msgs.some((m) => m.includes("AGENTS.md not found"))).toBe(true);
    });
  });

  it("before_agent_start: appends body from a custom org's AGENTS.md", async () => {
    await withTempProject(async (root) => {
      await writeAgentsMd(root, "custom", "# Custom Org\n\nSpecialist body.");
      process.chdir(root);

      const api = makeStubAPI();
      const factory = await importFresh();
      factory(api);
      (api as any).__stub.flags.org = "custom";

      const result = await fire(
        (api as any).__stub,
        "before_agent_start",
        { systemPrompt: "BASE", prompt: "hi" },
        { ui: { notify() {} } },
      );
      expect(result.systemPrompt).toContain("Org: custom");
      expect(result.systemPrompt).toContain("Specialist body.");
    });
  });
});
