// Large Result Eviction
// When tool results exceed the token limit, they are saved to the filesystem
// and replaced with a path reference + preview.
//
// Similar to Deep Agents' FilesystemMiddleware large result eviction.
// Pi truncates individual tool output (bash, read) but does NOT automatically
// evict large tool results to files.

import { estimateTokenCount } from './token-utils';

const DEFAULT_TOOL_RESULT_TOKEN_LIMIT = 20000; // ~80KB of text
const DEFAULT_PREVIEW_CHARS = 500; // Show first 500 chars as preview
const LARGE_RESULTS_DIR = '/large-tool-results/';

export interface EvictionConfig {
  maxTokens: number;
  previewChars: number;
  outputDir: string;
}

export const DEFAULT_EVICTION_CONFIG: EvictionConfig = {
  maxTokens: DEFAULT_TOOL_RESULT_TOKEN_LIMIT,
  previewChars: DEFAULT_PREVIEW_CHARS,
  outputDir: LARGE_RESULTS_DIR,
};

export interface EvictionResult {
  evicted: boolean;
  filePath?: string;
  preview?: string;
  originalLength: number;
}

// Evict a large tool result to a file, returning a path reference
export async function evictLargeResult(
  toolCallId: string,
  toolName: string,
  content: string,
  config: EvictionConfig = DEFAULT_EVICTION_CONFIG,
): Promise<EvictionResult> {
  // Quick estimate - if under limit, no eviction needed
  const tokenEstimate = estimateTokenCount(content);
  if (tokenEstimate <= config.maxTokens) {
    return { evicted: false, originalLength: content.length };
  }

  // Create the output directory
  const fs = await import('fs/promises');
  const dir = config.outputDir;
  await fs.mkdir(dir, { recursive: true });

  // Save to file
  const filePath = `${dir}${toolCallId}.md`;
  const header = `# Tool Result: ${toolName}\n# Tool Call ID: ${toolCallId}\n# Original length: ${content.length} chars\n---\n\n`;
  await fs.writeFile(filePath, header + content);

  // Create preview
  const preview = content.slice(0, config.previewChars);
  const truncated = preview.length < content.length;

  return {
    evicted: true,
    filePath,
    preview: truncated ? preview + '\n\n[... truncated, full result saved to file ...]' : preview,
    originalLength: content.length,
  };
}

// Replace tool result content with eviction notice
export function buildEvictedContent(toolName: string, filePath: string, preview: string): string {
  return `Tool result too large. The output has been saved to: ${filePath}

Preview (first ${preview.length} chars):
\`\`\`
${preview}
\`\`\`

Use the read tool to inspect the full result at: ${filePath}`;
}

// Check if a tool result should be evicted (fast check without full token count)
export function shouldEvict(content: string, maxTokens: number = DEFAULT_TOOL_RESULT_TOKEN_LIMIT): boolean {
  // Rough estimate: ~4 chars per token for English text
  return content.length > maxTokens * 4;
}
