/**
 * Provider catalog — static metadata for known LLM providers.
 *
 * Used by the ProvidersOverlay to show descriptions, types, and
 * default config when browsing or adding providers.
 */

export type ProviderType = "local" | "cloud" | "aggregator";

export interface CatalogEntry {
	name: string;
	description: string;
	type: ProviderType;
	defaultBaseUrl: string;
	requiresApiKey: boolean;
	docsUrl?: string;
}

export const PROVIDER_CATALOG: Record<string, CatalogEntry> = {
	llamacpp: {
		name: "llama.cpp",
		description: "Local GPU inference",
		type: "local",
		defaultBaseUrl: "http://localhost:8001/v1",
		requiresApiKey: false,
		docsUrl: "https://github.com/ggml-org/llama.cpp",
	},
	ollama: {
		name: "Ollama",
		description: "Run models locally",
		type: "local",
		defaultBaseUrl: "http://localhost:11434/v1",
		requiresApiKey: false,
		docsUrl: "https://ollama.com",
	},
	gemini: {
		name: "Google Gemini",
		description: "Google Cloud AI (Gemini models)",
		type: "cloud",
		defaultBaseUrl: "https://generativelanguage.googleapis.com/v1beta/openai",
		requiresApiKey: true,
		docsUrl: "https://ai.google.dev",
	},
	openai: {
		name: "OpenAI",
		description: "GPT-4o, o1, o3, o4-mini models",
		type: "cloud",
		defaultBaseUrl: "https://api.openai.com/v1",
		requiresApiKey: true,
		docsUrl: "https://platform.openai.com",
	},
	anthropic: {
		name: "Anthropic",
		description: "Claude models (safety-focused)",
		type: "cloud",
		defaultBaseUrl: "https://api.anthropic.com/v1",
		requiresApiKey: true,
		docsUrl: "https://docs.anthropic.com",
	},
	deepseek: {
		name: "DeepSeek",
		description: "DeepSeek models (direct API)",
		type: "cloud",
		defaultBaseUrl: "https://api.deepseek.com/v1",
		requiresApiKey: true,
		docsUrl: "https://platform.deepseek.com",
	},
	groq: {
		name: "Groq",
		description: "Ultra-fast inference (LPU)",
		type: "cloud",
		defaultBaseUrl: "https://api.groq.com/openai/v1",
		requiresApiKey: true,
		docsUrl: "https://console.groq.com",
	},
	together: {
		name: "Together AI",
		description: "Open-source model hosting",
		type: "cloud",
		defaultBaseUrl: "https://api.together.xyz/v1",
		requiresApiKey: true,
		docsUrl: "https://docs.together.ai",
	},
	mistral: {
		name: "Mistral AI",
		description: "Mistral and Codestral models",
		type: "cloud",
		defaultBaseUrl: "https://api.mistral.ai/v1",
		requiresApiKey: true,
		docsUrl: "https://docs.mistral.ai",
	},
	cerebras: {
		name: "Cerebras",
		description: "Fastest inference (wafer-scale)",
		type: "cloud",
		defaultBaseUrl: "https://api.cerebras.ai/v1",
		requiresApiKey: true,
		docsUrl: "https://cloud.cerebras.ai",
	},
	openrouter: {
		name: "OpenRouter",
		description: "Access 200+ models via single API",
		type: "aggregator",
		defaultBaseUrl: "https://openrouter.ai/api",
		requiresApiKey: true,
		docsUrl: "https://openrouter.ai/docs",
	},
};

export const TYPE_COLORS: Record<ProviderType, string> = {
	local: "green",
	cloud: "blue",
	aggregator: "yellow",
};

export const TYPE_LABELS: Record<ProviderType, string> = {
	local: "Local",
	cloud: "Cloud",
	aggregator: "Aggregator",
};
