import { useCallback, useRef } from 'react';
import { usePiAgent, PiAgentState } from './usePiAgent';
import { api } from '../lib/api';

interface SessionEntry {
  hook: ReturnType<typeof usePiAgent>;
  project: string;
  agentId: string;
}

function compositeKey(project: string, agentId: string) {
  return `${project}::${agentId}`;
}

/**
 * Coordinates N usePiAgent hooks — multiple agents per project.
 */
export function usePiSessionManager(projects: string[]) {
  const entriesRef = useRef<Map<string, SessionEntry>>(new Map());

  const registerSession = useCallback((project: string, agentId: string, hook: ReturnType<typeof usePiAgent>) => {
    entriesRef.current.set(compositeKey(project, agentId), { hook, project, agentId });
  }, []);

  const unregisterSession = useCallback((project: string, agentId: string) => {
    entriesRef.current.delete(compositeKey(project, agentId));
  }, []);

  const getSessionState = useCallback((project: string, agentId: string = 'default'): PiAgentState | null => {
    const entry = entriesRef.current.get(compositeKey(project, agentId));
    return entry ? entry.hook.state : null;
  }, []);

  const getAgentsForProject = useCallback((project: string): Array<{ agentId: string; state: PiAgentState }> => {
    const agents: Array<{ agentId: string; state: PiAgentState }> = [];
    entriesRef.current.forEach((entry, key) => {
      if (entry.project === project) {
        agents.push({ agentId: entry.agentId, state: entry.hook.state });
      }
    });
    return agents;
  }, []);

  const getAllSessionStates = useCallback((): Map<string, PiAgentState> => {
    const map = new Map<string, PiAgentState>();
    entriesRef.current.forEach((entry) => {
      map.set(compositeKey(entry.project, entry.agentId), entry.hook.state);
    });
    return map;
  }, []);

  const sendPrompt = useCallback((project: string, agentId: string, message: string, opts?: { model?: string; thinkingLevel?: string; autoBranch?: boolean }) => {
    const entry = entriesRef.current.get(compositeKey(project, agentId));
    if (entry) {
      entry.hook.sendPrompt(message, project, { ...opts, agentId });
    }
  }, []);

  const abort = useCallback((project: string, agentId: string) => {
    const entry = entriesRef.current.get(compositeKey(project, agentId));
    if (entry) {
      entry.hook.abort(project, agentId);
    }
  }, []);

  const hydrateAll = useCallback(() => {
    entriesRef.current.forEach((entry) => {
      entry.hook.hydrateState(entry.project, entry.agentId);
    });
  }, []);

  const activeCount = useCallback((): number => {
    let count = 0;
    entriesRef.current.forEach((entry) => {
      if (entry.hook.state.isStreaming) count++;
    });
    return count;
  }, []);

  const spawnAgent = useCallback(async (project: string): Promise<string> => {
    const res = await api.pi.spawnAgent(project);
    return res.agentId;
  }, []);

  const destroyAgent = useCallback(async (project: string, agentId: string) => {
    // Abort if streaming
    const entry = entriesRef.current.get(compositeKey(project, agentId));
    if (entry) {
      entry.hook.abort(project, agentId);
      entriesRef.current.delete(compositeKey(project, agentId));
    }
    await api.pi.destroyAgent(project, agentId);
  }, []);

  return {
    registerSession,
    unregisterSession,
    getSessionState,
    getAgentsForProject,
    getAllSessionStates,
    sendPrompt,
    abort,
    hydrateAll,
    activeCount,
    spawnAgent,
    destroyAgent,
  };
}
