import { useState, useCallback, useRef, useEffect } from 'react';
import { api, LabeledElement } from '../lib/api';

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
  const mountedRef = useRef(true);

  useEffect(() => {
    return () => { mountedRef.current = false; };
  }, []);

  const enableComputerUse = useCallback(async (sandboxId: string) => {
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      // 30s timeout — sandbox creation + Chrome startup can be slow but shouldn't hang forever
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 30000);
      const res = await api.computerUse.enable(sandboxId, { signal: controller.signal });
      clearTimeout(timeout);
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          enabled: true,
          sandboxId,
          cdpPort: res.cdpPort,
          loading: false,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        const msg = err instanceof DOMException && err.name === 'AbortError'
          ? 'Computer use enable timed out (30s). The sandbox may still be starting — try again.'
          : String(err);
        setState(prev => ({ ...prev, loading: false, error: msg }));
      }
    }
  }, []);

  const disableComputerUse = useCallback(async () => {
    if (!state.sandboxId) return;
    try {
      await api.computerUse.disable(state.sandboxId);
      if (mountedRef.current) {
        setState({ ...initialState });
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, error: String(err) }));
      }
    }
  }, [state.sandboxId]);

  const takeScreenshot = useCallback(async () => {
    if (!state.sandboxId) return;
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      const res = await api.computerUse.screenshot(state.sandboxId, true);
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          screenshot: res.image,
          description: res.description || null,
          url: res.url || prev.url,
          title: res.title || prev.title,
          loading: false,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
    }
  }, [state.sandboxId]);

  const getSnapshot = useCallback(async () => {
    if (!state.sandboxId) return;
    try {
      const res = await api.computerUse.snapshot(state.sandboxId);
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          elements: res.elements,
          url: res.url,
          title: res.title,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, error: String(err) }));
      }
    }
  }, [state.sandboxId]);

  const act = useCallback(async (action: Parameters<typeof api.computerUse.act>[1]) => {
    if (!state.sandboxId) return;
    setState(prev => ({ ...prev, loading: true, error: null }));
    try {
      const pageInfo = await api.computerUse.act(state.sandboxId, action);
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          elements: pageInfo.elements,
          url: pageInfo.url,
          title: pageInfo.title,
          loading: false,
        }));
        // Auto-take screenshot after action
        takeScreenshot();
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
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
