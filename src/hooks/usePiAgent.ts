import { useState, useCallback, useRef, useEffect } from 'react';
import {
  PiSSEEvent,
  PiSessionState,
  ToolCall,
  PiModel,
  parseSSEEvent,
} from '../lib/pi-events';

export interface PiAgentState {
  isStreaming: boolean;
  text: string;
  thinking: string;
  toolCalls: ToolCall[];
  model: string | null;
  tokenUsage: { input: number; output: number; cache: number };
  error: string | null;
}

const initialState: PiAgentState = {
  isStreaming: false,
  text: '',
  thinking: '',
  toolCalls: [],
  model: null,
  tokenUsage: { input: 0, output: 0, cache: 0 },
  error: null,
};

export function usePiAgent() {
  const [state, setState] = useState<PiAgentState>(initialState);
  const abortRef = useRef<AbortController | null>(null);
  const projectRef = useRef<string | null>(null);

  // Hydrate state from backend on mount or when project changes.
  // This handles the case where the user refreshes mid-task:
  // the Go backend still has an active Pi subprocess streaming,
  // so we re-attach by fetching current state + messages.
  const hydrateState = useCallback(async (project: string) => {
    if (!project) return;
    projectRef.current = project;
    try {
      const stateRes = await fetch(`/api/pi/state?project=${encodeURIComponent(project)}`);
      if (!stateRes.ok) return;
      const serverState = await stateRes.json();

      if (serverState.streaming) {
        // Backend has an active Pi session - hydrate UI
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
        // Session exists but idle - restore model info
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
          };

        case 'text_delta':
          return { ...prev, text: prev.text + (event.data as { text: string }).text };

        case 'thinking_delta':
          return { ...prev, thinking: prev.thinking + (event.data as { text: string }).text };

        case 'tool_execution_start': {
          const toolData = event.data as { toolName: string; args: Record<string, unknown>; toolId: string };
          const newCall: ToolCall = {
            id: toolData.toolId || `tool-${Date.now()}`,
            name: toolData.toolName,
            args: toolData.args,
            startTime: Date.now(),
          };
          return { ...prev, toolCalls: [...prev.toolCalls, newCall] };
        }

        case 'tool_execution_end': {
          const endData = event.data as { toolId: string; result: unknown; error?: string };
          return {
            ...prev,
            toolCalls: prev.toolCalls.map(tc =>
              tc.id === endData.toolId
                ? { ...tc, result: endData.result, error: endData.error, endTime: Date.now() }
                : tc
            ),
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
          };
        }

        case 'compaction_end':
          return prev; // UI can show a notification

        case 'error':
          return { ...prev, error: (event.data as { error: string }).error, isStreaming: false };

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

        default:
          return prev;
      }
    });
  }, []);

  const sendPrompt = useCallback(
    async (message: string, project: string, opts?: { model?: string; thinkingLevel?: string }) => {
      // Abort any existing request
      if (abortRef.current) {
        abortRef.current.abort();
      }

      const controller = new AbortController();
      abortRef.current = controller;

      setState(prev => ({
        ...initialState,
        model: prev.model,
        tokenUsage: prev.tokenUsage,
      }));

      try {
        const response = await fetch('/api/pi/prompt', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            message,
            project,
            model: opts?.model,
            thinkingLevel: opts?.thinkingLevel,
          }),
          signal: controller.signal,
        });

        if (!response.ok) {
          const err = await response.json().catch(() => ({ error: 'Request failed' }));
          setState(prev => ({ ...prev, error: err.error || `HTTP ${response.status}`, isStreaming: false }));
          return;
        }

        if (!response.body) {
          setState(prev => ({ ...prev, error: 'No response body', isStreaming: false }));
          return;
        }

        // Read SSE stream
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

        // Process remaining buffer
        if (buffer.trim()) {
          const event = parseSSEEvent(buffer);
          if (event) handleEvent(event);
        }
      } catch (err: any) {
        if (err.name === 'AbortError') return;
        setState(prev => ({ ...prev, error: err.message, isStreaming: false }));
      }
    },
    [handleEvent]
  );

  const abort = useCallback(async (project: string) => {
    if (abortRef.current) {
      abortRef.current.abort();
    }
    try {
      await fetch(`/api/pi/abort?project=${encodeURIComponent(project)}`, { method: 'POST' });
    } catch {}
    setState(prev => ({ ...prev, isStreaming: false }));
  }, []);

  const compact = useCallback(async (project: string) => {
    try {
      await fetch(`/api/pi/compact?project=${encodeURIComponent(project)}`, { method: 'POST' });
    } catch {}
  }, []);

  const switchModel = useCallback(async (project: string, provider: string, modelId: string) => {
    try {
      await fetch('/api/pi/model', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ project, provider, modelId }),
      });
      setState(prev => ({ ...prev, model: modelId }));
    } catch {}
  }, []);

  const getModels = useCallback(async (project: string): Promise<PiModel[]> => {
    try {
      const res = await fetch(`/api/pi/models?project=${encodeURIComponent(project)}`);
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
    }));
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
  };
}
