// Stub: tool types
export interface ToolDefinition<TParams, TDetails, TState> {
  name: string;
  label: string;
  description: string;
  parameters: TParams;
  execute?: (toolCallId: string, params: unknown, signal?: AbortSignal, onUpdate?: (d: unknown) => void, ctx?: unknown) => Promise<{ content: unknown[]; details?: unknown }>;
  renderCall?: (args: unknown, theme: unknown, context: unknown) => unknown;
  renderResult?: (result: unknown, options: unknown, theme: unknown, context: unknown) => unknown;
}
export interface ToolRenderContext {
  tui: unknown;
  hasUI: boolean;
  cwd: string;
  signal: AbortSignal;
}
