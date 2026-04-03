/**
 * Pi Agent SSE Event Types
 * Mirrors the Go handler's SSE output for the frontend.
 */

// SSE event types sent from the Go backend
export type PiEventType =
  | 'text_delta'
  | 'thinking_delta'
  | 'tool_execution_start'
  | 'tool_execution_end'
  | 'agent_start'
  | 'agent_end'
  | 'agent_spawned'
  | 'compaction_start'
  | 'compaction_end'
  | 'error'
  | 'state_update'
  | 'branch_created'
  | 'commit_created'
  | 'push_complete'
  | 'pr_created';

// Base event data
export interface PiTextDelta {
  text: string;
}

export interface PiThinkingDelta {
  text: string;
}

export interface PiToolStart {
  toolName: string;
  args: Record<string, unknown>;
  toolId: string;
}

export interface PiToolEnd {
  toolName: string;
  toolId: string;
  result: unknown;
  error?: string;
}

export interface PiAgentEnd {
  input: number;
  output: number;
  cache: number;
}

export interface PiCompactionEnd {
  compactedMessages: number;
  keptMessages: number;
}

export interface PiError {
  error: string;
}

export interface PiStateUpdate {
  model: string;
  input: number;
  output: number;
  cache: number;
}

export interface PiBranchCreated {
  branch: string;
}

export interface PiAgentSpawned {
  agentId: string;
}

export interface PiCommitCreated {
  message: string;
  branch: string;
}

export interface PiPushComplete {
  branch: string;
}

export interface PiPRCreated {
  url: string;
  number: number;
  title: string;
}

// Discriminated union of all Pi events
export type PiSSEEvent =
  | { type: 'text_delta'; data: PiTextDelta }
  | { type: 'thinking_delta'; data: PiThinkingDelta }
  | { type: 'tool_execution_start'; data: PiToolStart }
  | { type: 'tool_execution_end'; data: PiToolEnd }
  | { type: 'agent_start'; data: Record<string, never> }
  | { type: 'agent_end'; data: PiAgentEnd }
  | { type: 'agent_spawned'; data: PiAgentSpawned }
  | { type: 'compaction_start'; data: Record<string, never> }
  | { type: 'compaction_end'; data: PiCompactionEnd }
  | { type: 'error'; data: PiError }
  | { type: 'state_update'; data: PiStateUpdate }
  | { type: 'branch_created'; data: PiBranchCreated }
  | { type: 'commit_created'; data: PiCommitCreated }
  | { type: 'push_complete'; data: PiPushComplete }
  | { type: 'pr_created'; data: PiPRCreated };

// Tool call tracking
export interface ToolCall {
  id: string;
  name: string;
  args: Record<string, unknown>;
  result?: unknown;
  error?: string;
  startTime: number;
  endTime?: number;
}

// Conversation message types
export interface UserMessage {
  id: string;
  role: 'user';
  content: string;
  timestamp: number;
}

export interface AssistantMessage {
  id: string;
  role: 'assistant';
  text: string;
  thinking: string;
  toolCalls: ToolCall[];
  timestamp: number;
  streaming?: boolean;
}

export type ConversationMessage = UserMessage | AssistantMessage;

// Session state
export interface PiSessionState {
  model: string | null;
  streaming: boolean;
  input: number;
  output: number;
  cache: number;
}

// Model info
export interface PiModel {
  provider: string;
  id: string;
  name: string;
}

// Parse an SSE event from raw text
export function parseSSEEvent(raw: string): PiSSEEvent | null {
  const lines = raw.split('\n');
  let eventType: string | null = null;
  let eventData: string | null = null;

  for (const line of lines) {
    if (line.startsWith('event: ')) {
      eventType = line.slice(7).trim();
    } else if (line.startsWith('data: ')) {
      eventData = line.slice(6);
    }
  }

  if (!eventType || !eventData) return null;

  let data: any;
  try {
    data = JSON.parse(eventData);
  } catch {
    return null;
  }

  return { type: eventType as PiEventType, data } as PiSSEEvent;
}
