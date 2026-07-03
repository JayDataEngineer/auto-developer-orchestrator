// Unit tests for mimo-provider. Stubs ExtensionAPI, drives the factory,
// asserts the cost-capped registration. No network, no LLM.

import { describe, expect, it } from "vitest";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

interface StubAPI {
  providers: Map<string, any>;
}

function makeStubAPI(): ExtensionAPI {
  const stub: StubAPI = { providers: new Map() };
  const apiMethods: Record<string, (...args: any[]) => any> = {
    registerProvider(name: string, opts: any) {
      stub.providers.set(name, opts);
    },
  };
  return Object.assign(apiMethods, { __stub: stub }) as unknown as ExtensionAPI;
}

const importFresh = () => import("./index.ts").then((m) => m.default);

describe("mimo-provider", () => {
  it("registers a single provider named 'mimo'", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    const stub = (api as any).__stub as StubAPI;
    expect(Array.from(stub.providers.keys())).toEqual(["mimo"]);
  });

  it("points at OpenCode Zen Go endpoint with $OPENCODE_API_KEY", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    const opts = (api as any).__stub.providers.get("mimo");
    expect(opts.baseUrl).toBe("https://opencode.ai/zen/go/v1");
    expect(opts.apiKey).toBe("$OPENCODE_API_KEY");
    expect(opts.api).toBe("openai-completions");
  });

  it("registers exactly one model: mimo-v2.5 with text+image input", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    const opts = (api as any).__stub.providers.get("mimo");
    expect(opts.models).toHaveLength(1);
    const m = opts.models[0];
    expect(m.id).toBe("mimo-v2.5");
    expect(m.input).toEqual(["text", "image"]);
    expect(m.api).toBe("openai-completions");
  });

  it("caps contextWindow at 200K (cost control — triggers earlier compaction)", async () => {
    const api = makeStubAPI();
    const factory = await importFresh();
    factory(api);
    const m = (api as any).__stub.providers.get("mimo").models[0];
    expect(m.contextWindow).toBe(200000);
    expect(m.maxTokens).toBeGreaterThan(0);
  });
});
