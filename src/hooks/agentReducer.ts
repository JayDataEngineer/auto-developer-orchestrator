/**
 * Pure state transitions for Pux agent events.
 * No React, no hooks, no side effects — fully testable.
 */

import type {
  PuxSSEEvent,
  ToolCall,
  ConversationMessage,
  AssistantMessage,
  PuxApprovalRequest,
  PuxArtifact,
  PuxPlanStep,
} from '../lib/pux-events';
import type { SubAgentInfo } from '../lib/api';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export interface PuxAgentState {
  messages: ConversationMessage[];
  isStreaming: boolean;
  text: string;
  thinking: string;
  toolCalls: ToolCall[];
  model: string | null;
  tokenUsage: { input: number; output: number; cache: number };
  error: string | null;
  branchName: string | null;
  lastPrompt: string;
  agentId: string;
  prUrl: string | null;
  prNumber: number | null;
  subAgents: SubAgentInfo[];
  artifacts: PuxArtifact[];
  plan: { artifactId: string; steps: PuxPlanStep[] } | null;
  lastCommit: { message: string; branch: string } | null;
  lastPush: { branch: string } | null;
  pendingApproval: PuxApprovalRequest | null;
  contextMetrics: {
    tokens: number;
    contextSize: number;
    utilization: number;
    compactionType: string | null;
  } | null;
}

export const initialAgentState: PuxAgentState = {
  messages: [],
  isStreaming: false,
  text: '',
  thinking: '',
  toolCalls: [],
  model: null,
  tokenUsage: { input: 0, output: 0, cache: 0 },
  error: null,
  branchName: null,
  lastPrompt: '',
  agentId: 'default',
  prUrl: null,
  prNumber: null,
  subAgents: [],
  artifacts: [],
  plan: null,
  lastCommit: null,
  lastPush: null,
  pendingApproval: null,
  contextMetrics: null,
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Update the last assistant message in the messages array. */
export function updateLastAssistant(
  messages: ConversationMessage[],
  updater: (msg: AssistantMessage) => AssistantMessage,
): ConversationMessage[] {
  const msgs = [...messages];
  const lastIdx = msgs.length - 1;
  if (lastIdx >= 0 && msgs[lastIdx].role === 'assistant') {
    msgs[lastIdx] = updater(msgs[lastIdx] as AssistantMessage);
  }
  return msgs;
}

// ---------------------------------------------------------------------------
// Reducer
// ---------------------------------------------------------------------------

/**
 * Pure state transition for a single SSE event.
 *
 * `genMsgId` and `genToolId` are injected so the function stays pure and
 * testable — callers pass module-level counters or test stubs.
 */
export function agentReducer(
  state: PuxAgentState,
  event: PuxSSEEvent,
  genMsgId: () => string,
  genToolId: () => string,
): PuxAgentState {
  switch (event.type) {
    // Delta types are handled upstream by the throttler; listed for exhaustiveness.
    case 'text_delta':
    case 'thinking_delta':
      return state;

    case 'agent_start':
      return {
        ...state,
        isStreaming: true,
        text: '',
        thinking: '',
        toolCalls: [],
        error: null,
        messages:
          state.messages.length > 0 &&
          state.messages[state.messages.length - 1].role === 'assistant' &&
          (state.messages[state.messages.length - 1] as AssistantMessage).streaming
            ? state.messages
            : [
                ...state.messages,
                {
                  id: genMsgId(),
                  role: 'assistant' as const,
                  text: '',
                  thinking: '',
                  toolCalls: [],
                  timestamp: Date.now(),
                  streaming: true,
                },
              ],
      };

    case 'agent_spawned':
      return { ...state, agentId: (event.data as { agentId: string }).agentId };

    case 'tool_execution_start': {
      const toolData = event.data as {
        toolName: string;
        args: Record<string, unknown>;
        toolId: string;
      };
      const newCall: ToolCall = {
        id: toolData.toolId || genToolId(),
        name: toolData.toolName,
        args: toolData.args,
        startTime: Date.now(),
      };
      return {
        ...state,
        toolCalls: [...state.toolCalls, newCall],
        messages: updateLastAssistant(state.messages, msg => ({
          ...msg,
          toolCalls: [...msg.toolCalls, newCall],
        })),
      };
    }

    case 'tool_execution_end': {
      const endData = event.data as {
        toolId: string;
        result: unknown;
        error?: string;
      };
      const updatedToolCalls = state.toolCalls.map(tc =>
        tc.id === endData.toolId
          ? { ...tc, result: endData.result, error: endData.error, endTime: Date.now() }
          : tc,
      );

      // Detect sub-agent spawning from bash tool result
      let newSubAgents = state.subAgents;
      const endedTool = updatedToolCalls.find(tc => tc.id === endData.toolId);
      if (
        endedTool?.name === 'bash' &&
        typeof endedTool.args?.command === 'string'
      ) {
        const cmd = endedTool.args.command;
        if (
          cmd.includes('/subagent/spawn') &&
          typeof endData.result === 'string'
        ) {
          try {
            const lines = (endData.result as string).split('\n');
            const jsonLine = lines.find(l => l.includes('"subAgentId"'));
            if (jsonLine) {
              const parsed = JSON.parse(jsonLine);
              if (
                parsed.subAgentId &&
                !state.subAgents.find(
                  sa => sa.subAgentId === parsed.subAgentId,
                )
              ) {
                const typeMatch = parsed.subAgentId.match(/sub-(\w+)-/);
                const subAgentType = typeMatch ? typeMatch[1] : 'code';
                newSubAgents = [
                  ...state.subAgents,
                  {
                    subAgentId: parsed.subAgentId,
                    type: subAgentType as SubAgentInfo['type'],
                    status: 'running' as const,
                    output: '',
                    inputTokens: 0,
                    outputTokens: 0,
                    cacheTokens: 0,
                    durationMs: 0,
                    toolCalls: 0,
                  },
                ];
              }
            }
          } catch {
            // Not valid JSON, skip
          }
        }
      }

      return {
        ...state,
        toolCalls: updatedToolCalls,
        subAgents: newSubAgents,
        messages: updateLastAssistant(state.messages, msg => ({
          ...msg,
          toolCalls: updatedToolCalls,
        })),
      };
    }

    case 'agent_end': {
      const endState = event.data as {
        input: number;
        output: number;
        cache: number;
        thinking?: string;
      };
      return {
        ...state,
        isStreaming: false,
        thinking: endState.thinking || state.thinking,
        tokenUsage: {
          input: state.tokenUsage.input + (endState.input || 0),
          output: state.tokenUsage.output + (endState.output || 0),
          cache: state.tokenUsage.cache + (endState.cache || 0),
        },
        messages: updateLastAssistant(state.messages, msg => ({
          ...msg,
          streaming: false,
          // Ensure thinking is set from agent_end if streaming deltas didn't capture it
          thinking: endState.thinking || msg.thinking,
        })),
      };
    }

    case 'compaction_start':
      return state;

    case 'compaction_end':
      return {
        ...state,
        contextMetrics: {
          tokens: event.data.contextTokens ?? 0,
          contextSize: event.data.contextSize ?? 0,
          utilization: event.data.contextUtil ?? 0,
          compactionType: event.data.compactionType ?? null,
        },
      };

    case 'error':
      return {
        ...state,
        error: (event.data as { error: string }).error,
        isStreaming: false,
        messages: updateLastAssistant(state.messages, msg => ({
          ...msg,
          streaming: false,
        })),
      };

    case 'state_update': {
      const stateData = event.data as {
        model: string;
        input: number;
        output: number;
        cache: number;
      };
      return {
        ...state,
        model: stateData.model || state.model,
        tokenUsage: {
          input: stateData.input || state.tokenUsage.input,
          output: stateData.output || state.tokenUsage.output,
          cache: stateData.cache || state.tokenUsage.cache,
        },
      };
    }

    case 'branch_created':
      return {
        ...state,
        branchName: (event.data as { branch: string }).branch,
      };

    case 'commit_created': {
      const commitData = event.data as { message: string; branch: string };
      return {
        ...state,
        lastCommit: {
          message: commitData.message,
          branch: commitData.branch,
        },
        branchName: commitData.branch || state.branchName,
      };
    }

    case 'push_complete': {
      const pushData = event.data as { branch: string };
      return {
        ...state,
        lastPush: { branch: pushData.branch },
      };
    }

    case 'pr_created': {
      const prData = event.data as {
        url: string;
        number: number;
        title: string;
      };
      return {
        ...state,
        prUrl: prData.url,
        prNumber: prData.number,
        messages: updateLastAssistant(state.messages, msg => ({ ...msg })),
      };
    }

    case 'approval_request':
    case 'question_asked': {
      const approvalData = event.data as PuxApprovalRequest;
      return { ...state, pendingApproval: approvalData };
    }

    case 'artifact_created': {
      const artData = event.data as { artifactId: string; artifact?: PuxArtifact };
      if (artData.artifact) {
        return {
          ...state,
          artifacts: [...state.artifacts, artData.artifact],
        };
      }
      return state;
    }

    case 'artifact_updated': {
      const updData = event.data as { artifactId: string; content: string };
      return {
        ...state,
        artifacts: state.artifacts.map(a =>
          a.id === updData.artifactId ? { ...a, content: updData.content } : a,
        ),
      };
    }

    case 'plan_created': {
      const planData = event.data as {
        artifactId: string;
        steps: PuxPlanStep[];
      };
      return {
        ...state,
        plan: { artifactId: planData.artifactId, steps: planData.steps },
      };
    }

    case 'plan_updated': {
      const planUpd = event.data as {
        stepIndex: number;
        status: string;
        note?: string;
      };
      if (!state.plan) return state;
      return {
        ...state,
        plan: {
          ...state.plan,
          steps: state.plan.steps.map(s =>
            s.index === planUpd.stepIndex
              ? { ...s, status: planUpd.status as PuxPlanStep['status'], note: planUpd.note }
              : s,
          ),
        },
      };
    }

    case 'subagent_start': {
      const subData = event.data as {
        subAgentId: string;
        persona: string;
        task: string;
      };
      const exists = state.subAgents.find(
        sa => sa.subAgentId === subData.subAgentId,
      );
      if (exists) return state;
      return {
        ...state,
        subAgents: [
          ...state.subAgents,
          {
            subAgentId: subData.subAgentId,
            type: subData.persona as SubAgentInfo['type'],
            status: 'running' as const,
            output: '',
            inputTokens: 0,
            outputTokens: 0,
            cacheTokens: 0,
            durationMs: 0,
            toolCalls: 0,
          },
        ],
      };
    }

    case 'subagent_end': {
      const subEnd = event.data as {
        subAgentId: string;
        status: 'complete' | 'failed';
        artifactId?: string;
      };
      return {
        ...state,
        subAgents: state.subAgents.map(sa =>
          sa.subAgentId === subEnd.subAgentId
            ? { ...sa, status: subEnd.status === 'complete' ? 'complete' as const : 'failed' as const }
            : sa,
        ),
      };
    }

    default:
      return state;
  }
}
