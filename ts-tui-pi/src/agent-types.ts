// Bridge types for @mariozechner/pi-agent-core — minimal subset we need
// Replaced by our SSE-backed session, but keeps the type contract for components.

export interface AgentMessage {
  id: string;
  role: "user" | "assistant" | "system";
  content: ContentBlock[];
  timestamp: number;
  tokens?: { input: number; output: number };
  toolResults?: ToolResultMessage[];
}

export type ContentBlock =
  | { type: "text"; text: string }
  | { type: "thinking"; text: string; thinkingLevel: string }
  | { type: "tool_use"; toolCallId: string; toolName: string; args: Record<string, unknown> };

export interface ToolResultMessage {
  toolCallId: string;
  toolName: string;
  content: unknown;
  isError: boolean;
}

// AgentSessionEvent — our extended version includes approval + artifacts
export type AgentSessionEvent =
  | { type: "agent_start" }
  | { type: "agent_end"; messages: AgentMessage[] }
  | { type: "turn_start"; index: number }
  | { type: "turn_end"; message: AgentMessage; toolResults: ToolResultMessage[] }
  | { type: "message_start"; message: AgentMessage }
  | { type: "message_update"; message: AgentMessage; delta: { type: string; text?: string; contentIndex?: number } }
  | { type: "message_end"; message: AgentMessage }
  | { type: "tool_execution_start"; toolCallId: string; toolName: string; args: Record<string, unknown> }
  | { type: "tool_execution_update"; toolCallId: string; toolName: string; partialResult: unknown }
  | { type: "tool_execution_end"; toolCallId: string; toolName: string; result: unknown; isError: boolean }
  | { type: "compaction_start"; reason: string }
  | { type: "compaction_end"; result?: string; aborted?: boolean }
  | { type: "error"; message: string }
  | { type: "approval_request"; requestId: string; toolName: string; toolArgs: string; message: string; risk: string }
  | { type: "artifact_created"; id: string; title: string; contentType: string }
  | { type: "subagent_start"; name: string }
  | { type: "subagent_end"; name: string; result?: string };

// Minimal AssistantMessage type for pi-ai compatibility
export interface AssistantMessage {
  role: "assistant";
  content: ContentBlock[];
}

// Thinking level enum
export type ThinkingLevel = "off" | "minimal" | "low" | "medium" | "high" | "xhigh";
