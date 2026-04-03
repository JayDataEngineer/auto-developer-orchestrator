import { useState, useCallback, useRef, useEffect } from 'react';
import { api, Artifact } from '../lib/api';

interface ArtifactsState {
  artifacts: Artifact[];
  loading: boolean;
  error: string | null;
}

export function useArtifacts(agentId: string | null) {
  const [state, setState] = useState<ArtifactsState>({
    artifacts: [],
    loading: false,
    error: null,
  });
  const intervalRef = useRef<ReturnType<typeof setInterval>>(undefined);

  const fetchArtifacts = useCallback(async () => {
    if (!agentId) return;
    try {
      const res = await api.artifacts.list(agentId);
      setState(prev => ({ ...prev, artifacts: res.artifacts, loading: false, error: null }));
    } catch (err) {
      setState(prev => ({ ...prev, loading: false, error: String(err) }));
    }
  }, [agentId]);

  // Poll for artifact updates every 5 seconds when agentId is set
  useEffect(() => {
    if (!agentId) return;
    fetchArtifacts();
    intervalRef.current = setInterval(fetchArtifacts, 5000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [agentId, fetchArtifacts]);

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
