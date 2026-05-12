/**
 * Pux Agent SSE Event Types
 * Mirrors the Go handler's SSE output for the frontend.
 */

import type { LabeledElement } from './api';

// SSE event types sent from the Go backend
export type PuxEventType =
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
  | 'pr_created'
  | 'web_update'
  | 'approval_request'
  | 'question_asked'
  | 'artifact_created'
  | 'artifact_updated'
  | 'plan_created'
  | 'plan_updated'
  | 'subagent_start'
  | 'subagent_end';

// Base event data
export interface PuxTextDelta {
  text: string;
}

export interface PuxThinkingDelta {
  text: string;
}

export interface PuxToolStart {
  toolName: string;
  args: Record<string, unknown>;
  toolId: string;
}

export interface PuxToolEnd {
  toolName: string;
  toolId: string;
  result: unknown;
  error?: string;
}

export interface PuxAgentEnd {
  input: number;
  output: number;
  cache: number;
}

export interface PuxCompactionEnd {
  compactedMessages: number;
  keptMessages: number;
  compactionType?: string;  // "micro" or "full"
  contextTokens?: number;
  contextSize?: number;
  contextUtil?: number;     // 0-1 utilization ratio
}

export interface PuxError {
  error: string;
}

export interface PuxStateUpdate {
  model: string;
  input: number;
  output: number;
  cache: number;
}

export interface PuxBranchCreated {
  branch: string;
}

export interface PuxAgentSpawned {
  agentId: string;
}

export interface PuxCommitCreated {
  message: string;
  branch: string;
}

export interface PuxPushComplete {
  branch: string;
}

export interface PuxPRCreated {
  url: string;
  number: number;
  title: string;
}

// Web browser automation events
export type { LabeledElement };

export interface PuxWebUpdate {
  url: string;
  title: string;
  screenshot: string; // base64
  elements: LabeledElement[];
}

// Orchestrator artifact events
export interface PuxArtifact {
  id: string;
  parentId?: string;
  sourceId: string;
  persona: string;
  type: string; // 'data' | 'code' | 'summary' | 'file' | 'plan'
  title: string;
  content: string;
  metadata?: Record<string, string>;
}

export interface PuxArtifactCreated {
  artifactId: string;
  artifact: PuxArtifact;
}

export interface PuxArtifactUpdated {
  artifactId: string;
  content: string;
}

// Orchestrator plan events
export interface PuxPlanStep {
  index: number;
  desc: string;
  status: 'pending' | 'in_progress' | 'done' | 'failed' | 'skipped';
  note?: string;
  artifactId?: string;
}

export interface PuxPlanCreated {
  artifactId: string;
  steps: PuxPlanStep[];
}

export interface PuxPlanUpdated {
  stepIndex: number;
  status: string;
  note?: string;
}

// Orchestrator sub-agent events
export interface PuxSubAgentStart {
  subAgentId: string;
  persona: string;
  task: string;
}

export interface PuxSubAgentEnd {
  subAgentId: string;
  status: 'complete' | 'failed';
  artifactId?: string;
}

// Approval/question events (human-in-the-loop)
export interface PuxApprovalRequest {
  requestId: string;
  type: 'tool_confirm' | 'plan' | 'question';
  toolName?: string;
  toolArgs?: Record<string, unknown>;
  message: string;
  risk: 'low' | 'medium' | 'high';
}

// Discriminated union of all Pux events
export type PuxSSEEvent =
  | { type: 'text_delta'; data: PuxTextDelta }
  | { type: 'thinking_delta'; data: PuxThinkingDelta }
  | { type: 'tool_execution_start'; data: PuxToolStart }
  | { type: 'tool_execution_end'; data: PuxToolEnd }
  | { type: 'agent_start'; data: Record<string, never> }
  | { type: 'agent_end'; data: PuxAgentEnd }
  | { type: 'agent_spawned'; data: PuxAgentSpawned }
  | { type: 'compaction_start'; data: Record<string, never> }
  | { type: 'compaction_end'; data: PuxCompactionEnd }
  | { type: 'error'; data: PuxError }
  | { type: 'state_update'; data: PuxStateUpdate }
  | { type: 'branch_created'; data: PuxBranchCreated }
  | { type: 'commit_created'; data: PuxCommitCreated }
  | { type: 'push_complete'; data: PuxPushComplete }
  | { type: 'pr_created'; data: PuxPRCreated }
  | { type: 'web_update'; data: PuxWebUpdate }
  | { type: 'approval_request'; data: PuxApprovalRequest }
  | { type: 'question_asked'; data: PuxApprovalRequest }
  | { type: 'artifact_created'; data: PuxArtifactCreated }
  | { type: 'artifact_updated'; data: PuxArtifactUpdated }
  | { type: 'plan_created'; data: PuxPlanCreated }
  | { type: 'plan_updated'; data: PuxPlanUpdated }
  | { type: 'subagent_start'; data: PuxSubAgentStart }
  | { type: 'subagent_end'; data: PuxSubAgentEnd };

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
export interface PuxSessionState {
  model: string | null;
  streaming: boolean;
  input: number;
  output: number;
  cache: number;
}

// Model info
export interface PuxModel {
  provider: string;
  id: string;
  name: string;
}

// Parse an SSE event from raw text
export function parseSSEEvent(raw: string): PuxSSEEvent | null {
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

  return { type: eventType as PuxEventType, data } as PuxSSEEvent;
}
