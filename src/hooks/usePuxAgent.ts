import { useState, useCallback, useRef, useEffect } from 'react';
import {
  PuxSSEEvent,
  ToolCall,
  PuxModel,
  ConversationMessage,
  AssistantMessage,
} from '../lib/pux-events';
import { SubAgentInfo } from '../lib/api';
import { readSSEStream } from './useSSEStream';
import { api } from '../lib/api';
import { showToast } from '../lib/toast';
import {
  PuxAgentState,
  initialAgentState,
  updateLastAssistant,
  agentReducer,
} from './agentReducer';
import { useThrottledDeltas } from './useThrottledDeltas';
import { useMessageQueue, QueuedMessage } from './useMessageQueue';

// Re-exports for backward compatibility
export type { SubAgentInfo } from '../lib/api';
export type { PuxAgentState } from './agentReducer';

// Instance-scoped ID generators via refs
let instanceCounter = 0;

export function usePuxAgent(initialAgentId: string = 'default') {
  const [state, setState] = useState<PuxAgentState>({ ...initialAgentState, agentId: initialAgentId });
  const abortRef = useRef<AbortController | null>(null);
  const projectRef = useRef<string | null>(null);
  const agentIdRef = useRef<string>(initialAgentId);
  const idPrefixRef = useRef<string>(`i${++instanceCounter}`);
  const msgCounterRef = useRef(0);
  const toolCounterRef = useRef(0);

  // Generation counter to prevent loadHistory from overwriting active messages
  const promptGenRef = useRef(0);

  const nextMsgId = useCallback(() =>
    `msg-${idPrefixRef.current}-${++msgCounterRef.current}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
  []);
  const nextToolFallbackId = useCallback(() =>
    `tool-${idPrefixRef.current}-${++toolCounterRef.current}-${Date.now()}`,
  []);

  // Throttled delta accumulation
  const { accumulate, syncFlush } = useThrottledDeltas(
    useCallback((textDelta: string, thinkingDelta: string) => {
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
    }, []),
  );

  const handleEvent = useCallback((event: PuxSSEEvent) => {
    if (accumulate(event)) return;
    syncFlush();
    setState(prev => agentReducer(prev, event, nextMsgId, nextToolFallbackId));
  }, [accumulate, syncFlush, nextMsgId, nextToolFallbackId]);

  useEffect(() => {
    agentIdRef.current = initialAgentId;
  }, [initialAgentId]);

  const hydrateState = useCallback(async (project: string, agentId?: string) => {
    if (!project) return;
    const aid = agentId || agentIdRef.current;
    projectRef.current = project;
    agentIdRef.current = aid;
    const genAtCall = promptGenRef.current;
    try {
      const serverState = await api.pux.getState(project, aid);

      // If the user sent a prompt while we were fetching, don't overwrite their state
      if (promptGenRef.current !== genAtCall) return;

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

      // Bump generation so late-arriving loadHistory won't overwrite our messages
      promptGenRef.current++;

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
        const response = await api.pux.prompt(message, project, aid, {
          model: opts?.model,
          thinkingLevel: opts?.thinkingLevel,
          autoBranch: opts?.autoBranch,
        });

        if (!response.ok) {
          const err = await response.json().catch(() => ({ error: 'Request failed' }));
          const msg = err.error || `HTTP ${response.status}`;
          setState(prev => ({
            ...prev,
            error: msg,
            isStreaming: false,
            messages: updateLastAssistant(prev.messages, msg => ({ ...msg, streaming: false })),
          }));
          showToast('error', msg);
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

        await readSSEStream(response, handleEvent);

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
        showToast('error', err.message || 'Prompt failed');
      }
    },
    [handleEvent, syncFlush, nextMsgId]
  );

  // Message queue — lets users queue prompts while the agent is streaming.
  // When streaming finishes, the next queued message auto-dispatches.
  const handleDequeue = useCallback(
    async (msg: QueuedMessage) => {
      const project = projectRef.current;
      if (!project) return;
      // Use sendPrompt directly — the queue handles ordering
      sendPrompt(msg.text, project, {
        model: msg.model,
        thinkingLevel: msg.thinkingLevel,
        autoBranch: msg.autoBranch,
        autoMerge: msg.autoMerge,
        agentId: agentIdRef.current,
      });
    },
    [sendPrompt],
  );

  const messageQueue = useMessageQueue(handleDequeue, state.isStreaming);

  // Auto-process queue when streaming finishes
  useEffect(() => {
    if (!state.isStreaming) {
      messageQueue.tryProcessNext();
    }
  }, [state.isStreaming, messageQueue.tryProcessNext]);

  const abort = useCallback(async (project: string, agentId?: string) => {
    if (abortRef.current) {
      abortRef.current.abort();
    }
    const aid = agentId || agentIdRef.current;
    try {
      await api.pux.abort(project, aid);
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
      await api.pux.compact(project, aid);
    } catch {}
  }, []);

  const switchModel = useCallback(async (project: string, provider: string, modelId: string, agentId?: string) => {
    const aid = agentId || agentIdRef.current;
    try {
      await api.pux.setModel(project, provider, modelId, aid);
      setState(prev => ({ ...prev, model: modelId }));
    } catch {
      showToast('error', `Failed to switch model to ${modelId}`);
    }
  }, []);

  const getModels = useCallback(async (project: string, agentId?: string): Promise<PuxModel[]> => {
    const aid = agentId || agentIdRef.current;
    try {
      const data = await api.pux.getModels(project, aid);
      return data.models || [];
    } catch {
      return [];
    }
  }, []);

  const reset = useCallback(() => {
    setState(prev => ({
      ...initialAgentState,
      model: prev.model,
      agentId: prev.agentId,
    }));
  }, []);

  const loadHistory = useCallback(async (project: string, agentId?: string) => {
    const aid = agentId || agentIdRef.current;
    // Capture generation before async fetch — if sendPrompt was called while
    // we were waiting, discard the stale history to avoid overwriting active messages
    const genAtCall = promptGenRef.current;
    try {
      const msgs = await api.pux.getMessages(project, aid);
      if (!Array.isArray(msgs) || msgs.length === 0) return;

      // If the user sent a prompt while we were fetching, don't overwrite their messages
      if (promptGenRef.current !== genAtCall) return;

      // Convert DB messages to ConversationMessage format
      const messages: ConversationMessage[] = msgs.map((m: any, idx: number) => {
        if (m.role === 'user') {
          return {
            id: `db-${idx}-${m.id}`,
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
          id: `db-${idx}-${m.id}`,
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

  const respondToApproval = useCallback(async (
    project: string,
    agentId: string,
    requestId: string,
    action: 'approve' | 'deny' | 'answer',
    message?: string,
  ) => {
    try {
      await api.pux.respond(project, agentId, requestId, action, message);
    } catch {
      showToast('error', 'Failed to send approval response');
    }
    setState(prev => ({ ...prev, pendingApproval: null }));
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
    respondToApproval,
    messageQueue,
  };
}
