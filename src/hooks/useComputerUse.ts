import { useState, useCallback } from 'react';
import { api, LabeledElement } from '../lib/api';
import { showToast } from '../lib/toast';

interface ComputerUseState {
  enabled: boolean;
  sandboxId: string | null;
  screenshot: string | null;  // base64 PNG
  description: string | null;
  elements: LabeledElement[];
  url: string;
  title: string;
  loading: boolean;
  error: string | null;
  cdpPort: number | null;
}

const initialState: ComputerUseState = {
  enabled: false,
  sandboxId: null,
  screenshot: null,
  description: null,
  elements: [],
  url: '',
  title: '',
  loading: false,
  error: null,
  cdpPort: null,
};

export function useComputerUse() {
  const [state, setState] = useState<ComputerUseState>(initialState);

  const enableComputerUse = useCallback(async (sandboxId: string) => {
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      // 30s timeout — the backend responds immediately (sends JSON before Docker ops).
      // If this still fails, retry once (the fast path kicks in on second attempt).
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 30_000);
      const res = await api.computerUse.enable(sandboxId, { signal: controller.signal });
      clearTimeout(timeout);
      setState(prev => ({
        ...prev,
        enabled: true,
        sandboxId,
        cdpPort: res.cdpPort,
        loading: false,
      }));
    } catch (err) {
      // Retry once — backend may have created the container, second attempt is fast
      try {
        const controller2 = new AbortController();
        const timeout2 = setTimeout(() => controller2.abort(), 15_000);
        const res = await api.computerUse.enable(sandboxId, { signal: controller2.signal });
        clearTimeout(timeout2);
        setState(prev => ({
          ...prev,
          enabled: true,
          sandboxId,
          cdpPort: res.cdpPort,
          loading: false,
        }));
        return;
      } catch { /* fall through to error */ }

      const msg = err instanceof DOMException && err.name === 'AbortError'
        ? 'Computer use enable timed out. The sandbox may still be starting — try again.'
        : String(err);
      setState(prev => ({ ...prev, loading: false, error: msg }));
      showToast('error', msg);
    }
  }, []);

  const disableComputerUse = useCallback(async () => {
    if (!state.sandboxId) return;
    try {
      await api.computerUse.disable(state.sandboxId);
      setState({ ...initialState });
    } catch (err) {
      const msg = String(err);
      setState(prev => ({ ...prev, error: msg }));
      showToast('error', msg);
    }
  }, [state.sandboxId]);

  const takeScreenshot = useCallback(async () => {
    if (!state.sandboxId) return;
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      const res = await api.computerUse.screenshot(state.sandboxId, true);
      setState(prev => ({
        ...prev,
        screenshot: res.image,
        description: res.description || null,
        url: res.url || prev.url,
        title: res.title || prev.title,
        loading: false,
      }));
    } catch (err) {
      const msg = String(err);
      setState(prev => ({ ...prev, loading: false, error: msg }));
      showToast('error', msg);
    }
  }, [state.sandboxId]);

  const getSnapshot = useCallback(async () => {
    if (!state.sandboxId) return;
    try {
      const res = await api.computerUse.snapshot(state.sandboxId);
      setState(prev => ({
        ...prev,
        elements: res.elements,
        url: res.url,
        title: res.title,
      }));
    } catch (err) {
      const msg = String(err);
      setState(prev => ({ ...prev, error: msg }));
      showToast('error', msg);
    }
  }, [state.sandboxId]);

  const act = useCallback(async (action: Parameters<typeof api.computerUse.act>[1]) => {
    if (!state.sandboxId) return;
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      const pageInfo = await api.computerUse.act(state.sandboxId, action);
      setState(prev => ({
        ...prev,
        elements: pageInfo.elements,
        url: pageInfo.url,
        title: pageInfo.title,
        loading: false,
      }));
      // Auto-take screenshot after action
      takeScreenshot();
    } catch (err) {
      const msg = String(err);
      setState(prev => ({ ...prev, loading: false, error: msg }));
      showToast('error', msg);
    }
  }, [state.sandboxId, takeScreenshot]);

  const navigate = useCallback((url: string) => {
    act({ action: 'navigate', url });
  }, [act]);

  const clickElement = useCallback((element: number) => {
    act({ action: 'click', element });
  }, [act]);

  const typeText = useCallback((element: number, text: string, submit = false) => {
    act({ action: 'type', element, text, submit });
  }, [act]);

  const scroll = useCallback((direction: string, amount = 300) => {
    act({ action: 'scroll', direction, amount });
  }, [act]);

  return {
    ...state,
    enableComputerUse,
    disableComputerUse,
    takeScreenshot,
    getSnapshot,
    act,
    navigate,
    clickElement,
    typeText,
    scroll,
  };
}
