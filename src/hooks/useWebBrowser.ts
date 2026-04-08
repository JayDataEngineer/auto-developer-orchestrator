import { useState, useCallback, useRef, useEffect } from 'react';
import type { LabeledElement, PageInfo } from '../lib/api';

export type { LabeledElement, PageInfo };

interface WebBrowserState {
  sessionId: string | null;
  url: string;
  title: string;
  screenshot: string | null;
  elements: LabeledElement[];
  loading: boolean;
  error: string | null;
  description: string | null;
}

const initialState: WebBrowserState = {
  sessionId: null,
  url: '',
  title: '',
  screenshot: null,
  elements: [],
  loading: false,
  error: null,
  description: null,
};

function pageInfoToState(info: PageInfo): Partial<WebBrowserState> {
  return {
    url: info.url,
    title: info.title,
    elements: info.elements,
    screenshot: info.screenshot || null,
  };
}

export function useWebBrowser() {
  const [state, setState] = useState<WebBrowserState>(initialState);
  const mountedRef = useRef(true);

  // Auto-create session on mount, auto-close on unmount
  useEffect(() => {
    mountedRef.current = true;
    createSession();

    return () => {
      mountedRef.current = false;
      if (state.sessionId) {
        fetch('/api/pi/web/session', {
          method: 'DELETE',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ sessionId: state.sessionId }),
        }).catch(() => {});
      }
    };
  }, []);

  const createSession = useCallback(async () => {
    try {
      const res = await fetch('/api/pi/web/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });
      if (!res.ok) throw new Error('Failed to create session');
      const data = await res.json();
      if (mountedRef.current) {
        setState(prev => ({ ...prev, sessionId: data.sessionId }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, error: String(err) }));
      }
    }
  }, []);

  const webFetch = useCallback(async <T>(
    endpoint: string,
    body: Record<string, unknown>,
    mapResult: (data: T) => Partial<WebBrowserState>,
    clearDescription = true,
  ) => {
    if (!state.sessionId) return;
    setState(prev => ({
      ...prev,
      loading: true,
      error: null,
      ...(clearDescription ? { description: null } : {}),
    }));

    try {
      const res = await fetch(`/api/pi/web/${endpoint}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...body, sessionId: state.sessionId }),
      });
      if (!res.ok) throw new Error(`${endpoint} failed: ${res.statusText}`);
      const data: T = await res.json();
      if (mountedRef.current) {
        setState(prev => ({ ...prev, ...mapResult(data), loading: false }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
    }
  }, [state.sessionId]);

  const navigate = useCallback((url: string) =>
    webFetch<PageInfo>('navigate', { url }, pageInfoToState), [webFetch]);

  const click = useCallback((elementId: number) =>
    webFetch<PageInfo>('click', { elementId }, pageInfoToState), [webFetch]);

  const type = useCallback((elementId: number, text: string, submit = false) =>
    webFetch<PageInfo>('type', { elementId, text, submit }, pageInfoToState), [webFetch]);

  const scroll = useCallback((direction: 'up' | 'down', amount = 300) =>
    webFetch<PageInfo>('scroll', { direction, amount }, pageInfoToState, false), [webFetch]);

  const describe = useCallback(() =>
    webFetch<{ description: string }>('describe', {}, data => ({
      description: data.description,
    }), false), [webFetch]);

  return {
    ...state,
    navigate,
    click,
    type,
    scroll,
    describe,
  };
}
