import { useCallback, useRef } from 'react';
import { usePiAgent, PiAgentState } from './usePiAgent';

interface SessionEntry {
  hook: ReturnType<typeof usePiAgent>;
  project: string;
}

/**
 * Coordinates N usePiAgent hooks — one per project.
 * Must be called at the top level of a component where `projects` is stable.
 */
export function usePiSessionManager(projects: string[]) {
  // We maintain one usePiAgent per project via refs.
  // Since React hooks can't be called dynamically, we pre-create a fixed
  // pool and map projects to hooks by index.
  const hooksRef = useRef<Map<string, ReturnType<typeof usePiAgent>>>(new Map());
  const hooksListRef = useRef<ReturnType<typeof usePiAgent>[]>([]);

  // Ensure we have a hook for each project
  // We use a stable pool approach: create hooks once, reassign projects
  for (const project of projects) {
    if (!hooksRef.current.has(project)) {
      // Create a new hook entry lazily won't work in React.
      // Instead we use a factory pattern with pre-allocated hooks.
    }
  }

  // Actually, React doesn't allow dynamic hook calls.
  // The dashboard component will instantiate usePiAgent per project at the top level
  // and pass them in. This hook just provides coordination helpers.

  const entriesRef = useRef<Map<string, SessionEntry>>(new Map());

  const registerSession = useCallback((project: string, hook: ReturnType<typeof usePiAgent>) => {
    entriesRef.current.set(project, { hook, project });
  }, []);

  const unregisterSession = useCallback((project: string) => {
    entriesRef.current.delete(project);
  }, []);

  const getSessionState = useCallback((project: string): PiAgentState | null => {
    const entry = entriesRef.current.get(project);
    return entry ? entry.hook.state : null;
  }, []);

  const getAllSessionStates = useCallback((): Map<string, PiAgentState> => {
    const map = new Map<string, PiAgentState>();
    entriesRef.current.forEach((entry, project) => {
      map.set(project, entry.hook.state);
    });
    return map;
  }, []);

  const sendPrompt = useCallback((project: string, message: string, opts?: { model?: string; thinkingLevel?: string; autoBranch?: boolean }) => {
    const entry = entriesRef.current.get(project);
    if (entry) {
      entry.hook.sendPrompt(message, project, opts);
    }
  }, []);

  const abort = useCallback((project: string) => {
    const entry = entriesRef.current.get(project);
    if (entry) {
      entry.hook.abort(project);
    }
  }, []);

  const hydrateAll = useCallback(() => {
    entriesRef.current.forEach((entry, project) => {
      entry.hook.hydrateState(project);
    });
  }, []);

  const activeCount = useCallback((): number => {
    let count = 0;
    entriesRef.current.forEach((entry) => {
      if (entry.hook.state.isStreaming) count++;
    });
    return count;
  }, []);

  return {
    registerSession,
    unregisterSession,
    getSessionState,
    getAllSessionStates,
    sendPrompt,
    abort,
    hydrateAll,
    activeCount,
  };
}
