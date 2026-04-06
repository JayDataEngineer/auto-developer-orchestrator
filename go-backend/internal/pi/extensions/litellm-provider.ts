/**
 * LiteLLM Provider Extension
 *
 * Registers a "litellm" provider that routes all model calls through
 * the LiteLLM proxy. This allows the Go orchestrator to set models
 * via SetModel("litellm", "modelId") and have them actually work.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

// Read LiteLLM config from environment (set by the Go orchestrator)
const LITELLM_URL = process.env.LITELLM_PROXY_URL || "http://localhost:4000";
const LITELLM_KEY = process.env.LITELLM_MASTER_KEY || "";

export default function (pi: ExtensionAPI) {
	pi.registerProvider("litellm", {
		baseUrl: LITELLM_URL.replace(/\/+$/, ""), // strip trailing slashes
		apiKey: LITELLM_KEY,
		api: "openai-completions",
		models: [
			// Fast / cheap models for coding
			{
				id: "econ",
				name: "Econ (GLM-4.5-Air)",
				reasoning: true,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
			{
				id: "fast",
				name: "Fast",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
			{
				id: "or-free",
				name: "OpenRouter Free",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
			// Qwen models
			{
				id: "qwen-35-27",
				name: "Qwen 35 27B",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
			{
				id: "qwen-35-27-code",
				name: "Qwen 35 27B Code",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
			{
				id: "qwen-cloud",
				name: "Qwen Cloud",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
			{
				id: "qwen-35-35b",
				name: "Qwen 35 35B",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
			{
				id: "qwen-35-35b-code",
				name: "Qwen 35 35B Code",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 32000,
				maxTokens: 8000,
			},
		],
	});
}
