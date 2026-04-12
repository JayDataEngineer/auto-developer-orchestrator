import { useState, useCallback, useRef, useEffect } from 'react';
import { api, SubAgentInfo } from '../lib/api';
import { usePolling } from './usePolling';
import { readSSEStream } from './useSSEStream';

interface UseSubAgentsReturn {
  subAgents: SubAgentInfo[];
  loading: boolean;
  error: string | null;
  spawn: (type: SubAgentInfo['type'], projectDir?: string, message?: string) => Promise<SubAgentInfo>;
  abort: (subAgentId: string) => Promise<void>;
  refreshStatus: (subAgentId: string) => Promise<SubAgentInfo>;
  fetchList: () => Promise<void>;
  watchResult: (subAgentId: string) => void;
  watchedOutput: string | null;
  watchedId: string | null;
  clearWatch: () => void;
}

export function useSubAgents(parentAgentId: string | null): UseSubAgentsReturn {
  const [subAgents, setSubAgents] = useState<SubAgentInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [watchedOutput, setWatchedOutput] = useState<string | null>(null);
  const [watchedId, setWatchedId] = useState<string | null>(null);
  const abortCtrlRef = useRef<AbortController | null>(null);

  const fetchList = useCallback(async () => {
    if (!parentAgentId) return;
    try {
      const res = await api.subAgents.list(parentAgentId);
      setSubAgents(res.subAgents || []);
      setError(null);
    } catch (err) {
      setError(String(err));
    }
  }, [parentAgentId]);

  const { loading } = usePolling(fetchList, 3000, !!parentAgentId);

  const spawn = useCallback(async (type: SubAgentInfo['type'] = 'code', projectDir?: string, message?: string) => {
    if (!parentAgentId) throw new Error('No parent agent');
    const result = await api.subAgents.spawn(parentAgentId, type, projectDir, message);
    setSubAgents(prev => [result, ...prev]);
    return result;
  }, [parentAgentId]);

  const abort = useCallback(async (subAgentId: string) => {
    await api.subAgents.abort(subAgentId);
    await fetchList();
  }, [fetchList]);

  const refreshStatus = useCallback(async (subAgentId: string) => {
    const info = await api.subAgents.status(subAgentId);
    setSubAgents(prev => prev.map(sa => sa.subAgentId === subAgentId ? info : sa));
    return info;
  }, []);

  const watchResult = useCallback((subAgentId: string) => {
    // Cancel any existing watch
    if (abortCtrlRef.current) {
      abortCtrlRef.current.abort();
    }
    setWatchedId(subAgentId);
    setWatchedOutput('');

    const ctrl = new AbortController();
    abortCtrlRef.current = ctrl;

    // Use SSE endpoint for streaming result
    fetch(`/api/pi/subagent/result?subAgentId=${encodeURIComponent(subAgentId)}`, {
      signal: ctrl.signal,
    })
      .then(async res => {
        if (!res.ok || !res.body) {
          const info = await api.subAgents.result(subAgentId);
          setWatchedOutput(info.output || info.error || 'No output');
          return;
        }
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let accumulated = '';
        let buffer = '';
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const parts = buffer.split('\n\n');
          buffer = parts.pop() || '';
          for (const part of parts) {
            for (const line of part.split('\n')) {
              if (line.startsWith('data: ')) {
                try {
                  const data = JSON.parse(line.slice(6));
                  if (data.output) accumulated += data.output;
                  if (data.delta) accumulated += data.delta;
                } catch {
                  accumulated += line.slice(6);
                }
              }
            }
          }
          setWatchedOutput(accumulated);
        }
        // Flush remaining buffer
        if (buffer.trim()) {
          for (const line of buffer.split('\n')) {
            if (line.startsWith('data: ')) {
              try {
                const data = JSON.parse(line.slice(6));
                if (data.output) accumulated += data.output;
                if (data.delta) accumulated += data.delta;
              } catch {
                accumulated += line.slice(6);
              }
            }
          }
          setWatchedOutput(accumulated);
        }
        // If nothing was streamed, try REST fallback
        if (!accumulated) {
          const info = await api.subAgents.result(subAgentId);
          setWatchedOutput(info.output || info.error || 'No output');
        }
      })
      .catch(err => {
        if (err.name !== 'AbortError') {
          setWatchedOutput(`Error: ${err.message}`);
        }
      });
  }, []);

  const clearWatch = useCallback(() => {
    if (abortCtrlRef.current) {
      abortCtrlRef.current.abort();
      abortCtrlRef.current = null;
    }
    setWatchedId(null);
    setWatchedOutput(null);
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (abortCtrlRef.current) abortCtrlRef.current.abort();
    };
  }, []);

  return {
    subAgents,
    loading,
    error,
    spawn,
    abort,
    refreshStatus,
    fetchList,
    watchResult,
    watchedOutput,
    watchedId,
    clearWatch,
  };
}
