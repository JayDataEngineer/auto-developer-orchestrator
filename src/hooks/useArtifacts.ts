import { useState, useCallback } from 'react';
import { api, Artifact } from '../lib/api';
import { usePolling } from './usePolling';

interface ArtifactsState {
  artifacts: Artifact[];
  error: string | null;
}

export function useArtifacts(agentId: string | null) {
  const [state, setState] = useState<ArtifactsState>({
    artifacts: [],
    error: null,
  });

  const fetchArtifacts = useCallback(async () => {
    if (!agentId) return;
    try {
      const res = await api.artifacts.list(agentId);
      setState(prev => ({ ...prev, artifacts: res.artifacts, error: null }));
    } catch (err) {
      setState(prev => ({ ...prev, error: String(err) }));
    }
  }, [agentId]);

  const { loading } = usePolling(fetchArtifacts, 5000, !!agentId);

  const getArtifactsByType = useCallback((type: Artifact['type']) => {
    return state.artifacts.filter(a => a.type === type);
  }, [state.artifacts]);

  const getLatestArtifact = useCallback((type: Artifact['type']) => {
    const typed = getArtifactsByType(type);
    return typed.length > 0 ? typed[typed.length - 1] : null;
  }, [getArtifactsByType]);

  return {
    ...state,
    fetchArtifacts,
    getArtifactsByType,
    getLatestArtifact,
    hasPlan: state.artifacts.some(a => a.type === 'plan'),
    hasTodo: state.artifacts.some(a => a.type === 'todo'),
    hasNotes: state.artifacts.some(a => a.type === 'notes'),
  };
}
