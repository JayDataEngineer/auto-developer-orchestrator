import { useState, useCallback, useRef, useEffect } from 'react';
import {
  PiSSEEvent,
  ToolCall,
  PiModel,
  ConversationMessage,
  AssistantMessage,
  parseSSEEvent,
} from '../lib/pi-events';

let msgIdCounter = 0;
function nextMsgId() { return `msg-${++msgIdCounter}-${Date.now()}`; }

export interface SubAgentInfo {
  id: string;
  type: string;
  status: 'spawning' | 'running' | 'complete' | 'failed';
  output?: string;
  toolCalls?: number;
  parentId: string;
  spawnedAt: number;
}

export interface PiAgentState {
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
}

const initialState: PiAgentState = {
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
};

// Helper: update the last assistant message in the messages array
function updateLastAssistant(
  messages: ConversationMessage[],
  updater: (msg: AssistantMessage) => AssistantMessage
): ConversationMessage[] {
  const msgs = [...messages];
  const lastIdx = msgs.length - 1;
  if (lastIdx >= 0 && msgs[lastIdx].role === 'assistant') {
    msgs[lastIdx] = updater(msgs[lastIdx] as AssistantMessage);
  }
  return msgs;
}

export function usePiAgent(initialAgentId: string = 'default') {
  const [state, setState] = useState<PiAgentState>({ ...initialState, agentId: initialAgentId });
  const abortRef = useRef<AbortController | null>(null);
  const projectRef = useRef<string | null>(null);
  const agentIdRef = useRef<string>(initialAgentId);

  // Throttled delta accumulation — avoids re-renders per token
  const pendingTextRef = useRef('');
  const pendingThinkingRef = useRef('');
  const flushRafRef = useRef<number | null>(null);

  // Flush accumulated text/thinking deltas to state (called via RAF)
  const flushPendingDeltas = useCallback(() => {
    flushRafRef.current = null;
    const textDelta = pendingTextRef.current;
    const thinkingDelta = pendingThinkingRef.current;
    if (!textDelta && !thinkingDelta) return;

    pendingTextRef.current = '';
    pendingThinkingRef.current = '';

    setState(prev => {
      let text = prev.text;
      let thinking = prev.thinking;
      if (textDelta) text = prev.text + textDelta;
      if (thinkingDelta) thinking = prev.thinking + thinkingDelta;
      return {
        ...prev,
        text,
        thinking,
        messages: updateLastAssistant(prev.messages, msg => ({
          ...msg,
          ...(textDelta ? { text: msg.text + textDelta } : {}),
          ...(thinkingDelta ? { thinking: msg.thinking + thinkingDelta } : {}),
        })),
      };
    });
  }, []);

  // Flush pending deltas synchronously (for non-delta events that need up-to-date state)
  const syncFlush = useCallback(() => {
    if (flushRafRef.current !== null) {
      cancelAnimationFrame(flushRafRef.current);
      flushRafRef.current = null;
    }
    flushPendingDeltas();
  }, [flushPendingDeltas]);

  // Schedule a RAF flush
  const scheduleFlush = useCallback(() => {
    if (flushRafRef.current === null) {
      flushRafRef.current = requestAnimationFrame(flushPendingDeltas);
    }
  }, [flushPendingDeltas]);

  // Clean up RAF on unmount
  useEffect(() => {
    return () => {
      if (flushRafRef.current !== null) {
        cancelAnimationFrame(flushRafRef.current);
      }
    };
  }, []);

  useEffect(() => {
    agentIdRef.current = initialAgentId;
  }, [initialAgentId]);

  const hydrateState = useCallback(async (project: string, agentId?: string) => {
    if (!project) return;
    const aid = agentId || agentIdRef.current;
    projectRef.current = project;
    agentIdRef.current = aid;
    try {
      const stateRes = await fetch(`/api/pi/state?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(aid)}`);
      if (!stateRes.ok) return;
      const serverState = await stateRes.json();

      if (serverState.streaming) {
        setState(prev => ({
          ...prev,
          isStreaming: true,
          model: serverState.model || prev.model,
          tokenUsage: {
            input: serverState.input || 0,
            output: serverState.output || 0,
            cache: serverState.cache || 0,
          },
        }));
      } else if (serverState.model) {
        setState(prev => ({
          ...prev,
          model: serverState.model,
          tokenUsage: {
            input: serverState.input || 0,
            output: serverState.output || 0,
            cache: serverState.cache || 0,
          },
        }));
      }
    } catch {
      // Silently fail - hydration is best-effort
    }
  }, []);

  const handleEvent = useCallback((event: PiSSEEvent) => {
    // Fast path: accumulate text/thinking deltas in refs, flush via RAF
    if (event.type === 'text_delta') {
      pendingTextRef.current += (event.data as { text: string }).text;
      scheduleFlush();
      return;
    }
    if (event.type === 'thinking_delta') {
      pendingThinkingRef.current += (event.data as { text: string }).text;
      scheduleFlush();
      return;
    }

    // All other events: flush pending deltas first, then process
    syncFlush();

    setState(prev => {
      switch (event.type) {
        case 'agent_start':
          return {
            ...prev,
            isStreaming: true,
            text: '',
            thinking: '',
            toolCalls: [],
            error: null,
            // Add empty assistant message if not already present from sendPrompt
            messages: prev.messages.length > 0 && prev.messages[prev.messages.length - 1].role === 'assistant' && (prev.messages[prev.messages.length - 1] as AssistantMessage).streaming
              ? prev.messages
              : [...prev.messages, {
                  id: nextMsgId(),
                  role: 'assistant' as const,
                  text: '',
                  thinking: '',
                  toolCalls: [],
                  timestamp: Date.now(),
                  streaming: true,
                }],
          };

        case 'agent_spawned':
          return { ...prev, agentId: (event.data as { agentId: string }).agentId };

        case 'tool_execution_start': {
          const toolData = event.data as { toolName: string; args: Record<string, unknown>; toolId: string };
          const newCall: ToolCall = {
            id: toolData.toolId || `tool-${Date.now()}`,
            name: toolData.toolName,
            args: toolData.args,
            startTime: Date.now(),
          };
          return {
            ...prev,
            toolCalls: [...prev.toolCalls, newCall],
            messages: updateLastAssistant(prev.messages, msg => ({
              ...msg,
              toolCalls: [...msg.toolCalls, newCall],
            })),
          };
        }

        case 'tool_execution_end': {
          const endData = event.data as { toolId: string; result: unknown; error?: string };
          const updatedToolCalls = prev.toolCalls.map(tc =>
            tc.id === endData.toolId
              ? { ...tc, result: endData.result, error: endData.error, endTime: Date.now() }
              : tc
          );

          // Detect sub-agent spawning from bash tool result
          let newSubAgents = prev.subAgents;
          const endedTool = updatedToolCalls.find(tc => tc.id === endData.toolId);
          if (endedTool?.name === 'bash' && typeof endedTool.args?.command === 'string') {
            const cmd = endedTool.args.command;
            if (cmd.includes('/subagent/spawn') && typeof endData.result === 'string') {
              try {
                // Try to parse the spawn response from the command output
                const lines = (endData.result as string).split('\n');
                const jsonLine = lines.find(l => l.includes('"subAgentId"'));
                if (jsonLine) {
                  const parsed = JSON.parse(jsonLine);
                  if (parsed.subAgentId && !prev.subAgents.find(sa => sa.id === parsed.subAgentId)) {
                    // Extract type from the subAgentId (format: sub-{type}-{timestamp})
                    const typeMatch = parsed.subAgentId.match(/sub-(\w+)-/);
                    const subAgentType = typeMatch ? typeMatch[1] : 'unknown';
                    newSubAgents = [...prev.subAgents, {
                      id: parsed.subAgentId,
                      type: subAgentType,
                      status: 'running',
                      parentId: prev.agentId,
                      spawnedAt: Date.now(),
                    }];
                  }
                }
              } catch {
                // Not valid JSON, skip
              }
            }
          }

          return {
            ...prev,
            toolCalls: updatedToolCalls,
            subAgents: newSubAgents,
            messages: updateLastAssistant(prev.messages, msg => ({ ...msg, toolCalls: updatedToolCalls })),
          };
        }

        case 'agent_end': {
          const endState = event.data as { input: number; output: number; cache: number };
          return {
            ...prev,
            isStreaming: false,
            tokenUsage: {
              input: prev.tokenUsage.input + (endState.input || 0),
              output: prev.tokenUsage.output + (endState.output || 0),
              cache: prev.tokenUsage.cache + (endState.cache || 0),
            },
            // Finalize the last assistant message
            messages: updateLastAssistant(prev.messages, msg => ({
              ...msg,
              streaming: false,
            })),
          };
        }

        case 'compaction_end':
          return prev;

        case 'error':
          return {
            ...prev,
            error: (event.data as { error: string }).error,
            isStreaming: false,
            messages: updateLastAssistant(prev.messages, msg => ({ ...msg, streaming: false })),
          };

        case 'state_update': {
          const stateData = event.data as { model: string; input: number; output: number; cache: number };
          return {
            ...prev,
            model: stateData.model || prev.model,
            tokenUsage: {
              input: stateData.input || prev.tokenUsage.input,
              output: stateData.output || prev.tokenUsage.output,
              cache: stateData.cache || prev.tokenUsage.cache,
            },
          };
        }

        case 'branch_created':
          return { ...prev, branchName: (event.data as { branch: string }).branch };

        case 'commit_created':
          return prev;

        case 'push_complete':
          return prev;

        case 'pr_created': {
          const prData = event.data as { url: string; number: number; title: string };
          return {
            ...prev,
            prUrl: prData.url,
            prNumber: prData.number,
            messages: updateLastAssistant(prev.messages, msg => ({ ...msg })),
          };
        }

        default:
          return prev;
      }
    });
  }, [syncFlush, scheduleFlush]);

  const sendPrompt = useCallback(
    async (message: string, project: string, opts?: { model?: string; thinkingLevel?: string; autoBranch?: boolean; autoMerge?: boolean; agentId?: string }) => {
      if (abortRef.current) {
        abortRef.current.abort();
      }

      const controller = new AbortController();
      abortRef.current = controller;

      const aid = opts?.agentId || agentIdRef.current;
      agentIdRef.current = aid;
      projectRef.current = project;

      // Add user message + placeholder assistant message, keep history
      const userMsg: ConversationMessage = {
        id: nextMsgId(),
        role: 'user',
        content: message,
        timestamp: Date.now(),
      };
      const assistantMsg: ConversationMessage = {
        id: nextMsgId(),
        role: 'assistant',
        text: '',
        thinking: '',
        toolCalls: [],
        timestamp: Date.now(),
        streaming: true,
      };

      setState(prev => ({
        ...prev,
        messages: [...prev.messages, userMsg, assistantMsg],
        text: '',
        thinking: '',
        toolCalls: [],
        error: null,
        isStreaming: true,
        lastPrompt: message,
        agentId: aid,
        prUrl: null,
        prNumber: null,
      }));

      try {
        const response = await fetch('/api/pi/prompt', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            message,
            project,
            agentId: aid,
            model: opts?.model,
            thinkingLevel: opts?.thinkingLevel,
            autoBranch: opts?.autoBranch,
            autoMerge: opts?.autoMerge,
          }),
          signal: controller.signal,
        });

        if (!response.ok) {
          const err = await response.json().catch(() => ({ error: 'Request failed' }));
          setState(prev => ({
            ...prev,
            error: err.error || `HTTP ${response.status}`,
            isStreaming: false,
            messages: updateLastAssistant(prev.messages, msg => ({ ...msg, streaming: false })),
          }));
          return;
        }

        if (!response.body) {
          setState(prev => ({
            ...prev,
            error: 'No response body',
            isStreaming: false,
            messages: updateLastAssistant(prev.messages, msg => ({ ...msg, streaming: false })),
          }));
          return;
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
          const { value, done } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const parts = buffer.split('\n\n');
          buffer = parts.pop() || '';

          for (const part of parts) {
            if (!part.trim()) continue;
            const event = parseSSEEvent(part);
            if (event) handleEvent(event);
          }
        }

        if (buffer.trim()) {
          const event = parseSSEEvent(buffer);
          if (event) handleEvent(event);
        }

        // Ensure any remaining pending deltas are flushed
        syncFlush();
      } catch (err: any) {
        if (err.name === 'AbortError') return;
        setState(prev => ({
          ...prev,
          error: err.message,
          isStreaming: false,
          messages: updateLastAssistant(prev.messages, msg => ({ ...msg, streaming: false })),
        }));
      }
    },
    [handleEvent, syncFlush]
  );

  const abort = useCallback(async (project: string, agentId?: string) => {
    if (abortRef.current) {
      abortRef.current.abort();
    }
    const aid = agentId || agentIdRef.current;
    try {
      await fetch(`/api/pi/abort?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(aid)}`, { method: 'POST' });
    } catch {}
    setState(prev => ({
      ...prev,
      isStreaming: false,
      messages: updateLastAssistant(prev.messages, msg => ({ ...msg, streaming: false })),
    }));
  }, []);

  const compact = useCallback(async (project: string, agentId?: string) => {
    const aid = agentId || agentIdRef.current;
    try {
      await fetch(`/api/pi/compact?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(aid)}`, { method: 'POST' });
    } catch {}
  }, []);

  const switchModel = useCallback(async (project: string, provider: string, modelId: string, agentId?: string) => {
    const aid = agentId || agentIdRef.current;
    try {
      await fetch('/api/pi/model', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ project, provider, modelId, agentId: aid }),
      });
      setState(prev => ({ ...prev, model: modelId }));
    } catch {}
  }, []);

  const getModels = useCallback(async (project: string, agentId?: string): Promise<PiModel[]> => {
    const aid = agentId || agentIdRef.current;
    try {
      const res = await fetch(`/api/pi/models?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(aid)}`);
      const data = await res.json();
      return data.models || [];
    } catch {
      return [];
    }
  }, []);

  const reset = useCallback(() => {
    setState(prev => ({
      ...initialState,
      model: prev.model,
      agentId: prev.agentId,
    }));
  }, []);

  const loadHistory = useCallback(async (project: string, agentId?: string) => {
    const aid = agentId || agentIdRef.current;
    try {
      const res = await fetch(`/api/pi/messages?project=${encodeURIComponent(project)}&agentId=${encodeURIComponent(aid)}`);
      if (!res.ok) return;
      const msgs = await res.json();
      if (!Array.isArray(msgs) || msgs.length === 0) return;

      // Convert DB messages to ConversationMessage format
      const messages: ConversationMessage[] = msgs.map((m: any) => {
        if (m.role === 'user') {
          return {
            id: `db-${m.id}`,
            role: 'user' as const,
            content: m.content || '',
            timestamp: new Date(m.createdAt).getTime(),
          };
        }
        let toolCalls: ToolCall[] = [];
        try {
          toolCalls = JSON.parse(m.toolCalls || '[]');
        } catch {}
        return {
          id: `db-${m.id}`,
          role: 'assistant' as const,
          text: m.text || '',
          thinking: m.thinking || '',
          toolCalls,
          timestamp: new Date(m.createdAt).getTime(),
          streaming: false,
        };
      });

      // Get last assistant message for live state
      const lastAssistant = [...messages].reverse().find(m => m.role === 'assistant') as AssistantMessage | undefined;

      setState(prev => ({
        ...prev,
        messages,
        text: lastAssistant?.text || '',
        thinking: lastAssistant?.thinking || '',
        toolCalls: lastAssistant?.toolCalls || [],
      }));
    } catch {
      // Silently fail - history loading is best-effort
    }
  }, []);

  return {
    state,
    sendPrompt,
    abort,
    compact,
    switchModel,
    getModels,
    reset,
    hydrateState,
    loadHistory,
  };
}
