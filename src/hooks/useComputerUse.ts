import { useState, useCallback } from 'react';
import { api } from '../lib/api';
import { showToast } from '../lib/toast';

interface ComputerUseState {
  enabled: boolean;
  sandboxId: string | null;
  loading: boolean;
  error: string | null;
}

const initialState: ComputerUseState = {
  enabled: false,
  sandboxId: null,
  loading: false,
  error: null,
};

export function useComputerUse() {
  const [state, setState] = useState<ComputerUseState>(initialState);

  const enableComputerUse = useCallback(async (sandboxId: string) => {
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 30_000);
      await api.computerUse.enable(sandboxId, { signal: controller.signal });
      clearTimeout(timeout);
      setState(prev => ({
        ...prev,
        enabled: true,
        sandboxId,
        loading: false,
      }));
    } catch {
      // Retry once
      try {
        const controller2 = new AbortController();
        const timeout2 = setTimeout(() => controller2.abort(), 15_000);
        await api.computerUse.enable(sandboxId, { signal: controller2.signal });
        clearTimeout(timeout2);
        setState(prev => ({
          ...prev,
          enabled: true,
          sandboxId,
          loading: false,
        }));
        return;
      } catch { /* fall through to error */ }

      setState(prev => ({ ...prev, loading: false, error: 'Computer use enable timed out. Try again.' }));
      showToast('error', 'Computer use enable timed out');
    }
  }, []);

  const disableComputerUse = useCallback(async () => {
    if (!state.sandboxId) return;
    try {
      await api.computerUse.disable(state.sandboxId);
      setState({ ...initialState });
    } catch (err) {
      setState(prev => ({ ...prev, error: String(err) }));
    }
  }, [state.sandboxId]);

  return {
    ...state,
    enableComputerUse,
    disableComputerUse,
  };
}
