// mimo-provider — cost-capped MiMo-V2.5 registration.
//
// Pi's built-in opencode-go provider already declares mimo-v2.5 with a 1M
// context window. That's nice in theory but expensive in practice — pi won't
// trigger context compaction until the session nears 1M tokens, so a long
// session just accumulates input-token cost. Capping contextWindow at 200K
// forces compaction earlier and caps per-request spend.
//
// Implementation note: pi's registerProvider replaces the provider's model
// list when `models` is provided, so we can't just patch the built-in
// opencode-go entry without wiping deepseek-v4-flash / glm-5 / kimi / etc.
// Instead we register a sibling provider named `mimo` pointing at the same
// endpoint with the same auth, exposing only the cost-capped mimo-v2.5.
//
// Use this provider when running long sessions where cost matters:
//   pux --provider mimo --model mimo-v2.5 ...
//
// Use the built-in opencode-go/mimo-v2.5 (1M ctx) only when you actually
// need the full context window for a specific task.

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
  pi.registerProvider("mimo", {
    name: "MiMo V2.5 (cost-capped at 200K)",
    baseUrl: "https://opencode.ai/zen/go/v1",
    apiKey: "$OPENCODE_API_KEY",
    api: "openai-completions",
    models: [
      {
        id: "mimo-v2.5",
        name: "MiMo V2.5 (200K cap)",
        api: "openai-completions",
        reasoning: false,
        input: ["text", "image"],
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
        contextWindow: 200000,
        maxTokens: 8192,
      },
    ],
  });
}
