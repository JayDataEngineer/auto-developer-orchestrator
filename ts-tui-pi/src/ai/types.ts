// Custom types for pi-ai compatibility — minimal subset needed by the TUI components

export interface TextContent {
  type: "text";
  text: string;
}

export interface ImageContent {
  type: "image";
  base64: string;
  mediaType: string;
}

export interface ThinkingContent {
  type: "thinking";
  text: string;
  thinkingLevel?: string;
}

export interface ToolCall {
  type: "tool_use";
  toolCallId: string;
  toolName: string;
  args: Record<string, unknown>;
}

export interface AssistantMessage {
  role: "assistant";
  content: (TextContent | ThinkingContent | ToolCall)[];
  api?: string;
  provider?: string;
  model?: string;
  responseId?: string;
  usage?: Usage;
  stopReason?: StopReason;
  errorMessage?: string;
  timestamp: number;
}

export interface UserMessage {
  role: "user";
  content: string | (TextContent | ImageContent)[];
  timestamp: number;
}

export interface ToolResultMessage<TDetails = unknown> {
  role: "toolResult";
  toolCallId: string;
  toolName: string;
  content: (TextContent | ImageContent)[];
  details?: TDetails;
  isError: boolean;
  timestamp: number;
}

export type Message = UserMessage | AssistantMessage | ToolResultMessage;

export interface Usage {
  inputTokens: number;
  outputTokens: number;
  cacheReadInputTokens?: number;
  cacheCreationInputTokens?: number;
  cacheWriteInputTokens?: number;
  costCents?: number;
}

export type StopReason =
  | "stop"
  | "length"
  | "toolUse"
  | "contentFilter"
  | "error"
  | "aborted"
  | "compacted"
  | string;

export type Api = string;
export type Provider = string;

// Event stream
export interface AssistantMessageEventStream extends AsyncIterable<AssistantMessageEvent> {}

export type AssistantMessageEvent =
  | { type: "start"; partial: AssistantMessage }
  | { type: "text_start"; contentIndex: number; partial: AssistantMessage }
  | { type: "text_delta"; contentIndex: number; delta: string; partial: AssistantMessage }
  | { type: "text_end"; contentIndex: number; content: string; partial: AssistantMessage }
  | { type: "thinking_start"; contentIndex: number; partial: AssistantMessage }
  | { type: "thinking_delta"; contentIndex: number; delta: string; partial: AssistantMessage }
  | { type: "thinking_end"; contentIndex: number; content: string; partial: AssistantMessage }
  | { type: "toolcall_start"; contentIndex: number; partial: AssistantMessage }
  | { type: "toolcall_delta"; contentIndex: number; delta: string; partial: AssistantMessage }
  | { type: "toolcall_end"; contentIndex: number; toolCall: ToolCall; partial: AssistantMessage }
  | { type: "done"; reason: StopReason; message: AssistantMessage }
  | { type: "error"; reason: "aborted" | "error"; error: AssistantMessage };
