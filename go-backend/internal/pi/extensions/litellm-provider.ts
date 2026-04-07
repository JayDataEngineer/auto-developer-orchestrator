/**
 * LiteLLM Provider Extension
 */
import type { ExtensionAPI, ProviderModelConfig } from "@mariozechner/pi-coding-agent";
import * as fs from "fs";
import * as path from "path";

export default function (pi: ExtensionAPI) {
  try {
    const modelsPath = path.join(process.env.HOME || "/home/ubuntu", ".pi/agent/models.json");
    const modelsConfig = JSON.parse(fs.readFileSync(modelsPath, "utf-8"));
    const providers = modelsConfig.providers || {};
    for (const [name, cfg] of Object.entries(providers)) {
      const provider = cfg as any;
      if (provider.baseUrl && provider.apiKey) {
        const apiType = provider.api || "openai-completions";
        
        const modelConfigs: ProviderModelConfig[] = provider.models?.map((m: any) => ({
          id: m.id,
          name: m.name || m.id,
          api: (m.api || apiType) as any,
          reasoning: m.reasoning ?? false,
          input: m.input ?? ["text"],
          cost: m.cost ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
          contextWindow: m.contextWindow ?? 32768,
          maxTokens: m.maxTokens ?? 8192,
        })) || [];
        
        pi.registerProvider(name, {
          baseUrl: provider.baseUrl,
          apiKey: provider.apiKey,
          api: apiType,
          models: modelConfigs,
          compat: provider.compat || {},
        });
        const ids = modelConfigs.map(m => m.id).join(', ');
        console.error(`[litellm-provider] Registered provider: ${name} (${apiType}) with ${modelConfigs.length} models: ${ids}`);
      }
    }
  } catch (e: any) {
    console.error(`[litellm-provider] Failed to load models.json: ${e.message}`);
  }
}
