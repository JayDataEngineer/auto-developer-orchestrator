// API types matching Go backend

export interface PromptRequest {
  message: string;
  project: string;
  agentId?: string;
  model?: string;
  thinkingLevel?: string;
  autoBranch?: boolean;
}

export interface ApprovalRequestBody {
  project: string;
  agentId: string;
  requestId: string;
  action: "approve" | "deny" | "answer";
  message?: string;
}

// SSE event types
export type SSEEventType =
  | "agent_spawned"
  | "text_delta"
  | "thinking_delta"
  | "tool_execution_start"
  | "tool_execution_end"
  | "approval_request"
  | "artifact_created"
  | "artifact_updated"
  | "agent_end"
  | "error"
  | "state_update"
  | "subagent_start"
  | "subagent_end";

export interface SSEEvent {
  type: SSEEventType;
  data: Record<string, unknown>;
}

export interface TextDeltaData {
  text: string;
}

export interface ToolStartData {
  toolName: string;
  toolId: string;
  args: string;
}

export interface ToolEndData {
  toolName: string;
  toolId: string;
  result: string;
  error: string;
}

export interface ApprovalData {
  requestId: string;
  type: string;
  toolName: string;
  toolArgs: string;
  message: string;
  risk: string;
}

export interface AgentEndData {
  inputTokens: number;
  outputTokens: number;
}

export interface ArtifactData {
  artifactId: string;
  type: string;
  title: string;
  content: string;
}

// Scheduler
export interface SchedulerJob {
  id: string;
  name: string;
  description?: string;
  project: string;
  agentId?: string;
  message: string;
  scheduleType: string;
  cronExpr?: string;
  everySeconds?: number;
  atTime?: string;
  enabled: boolean;
  status: string;
  lastRunAt?: string;
  lastRunStatus?: string;
  consecutiveErrors: number;
  webhookToken?: string;
  createdAt: string;
  updatedAt: string;
}
