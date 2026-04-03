import { useState, useCallback, useRef, useEffect } from 'react';

export interface LabeledElement {
  id: number;
  tag: string;
  text: string;
  role?: string;
  selector: string;
}

export interface PageInfo {
  url: string;
  title: string;
  elements: LabeledElement[];
  screenshot?: string; // base64 PNG
}

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
        // Fire-and-forget close
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

  const navigate = useCallback(async (url: string) => {
    if (!state.sessionId) return;
    setState(prev => ({ ...prev, loading: true, error: null, description: null }));

    try {
      const res = await fetch('/api/pi/web/navigate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url, sessionId: state.sessionId }),
      });
      if (!res.ok) throw new Error(`Navigate failed: ${res.statusText}`);
      const info: PageInfo = await res.json();
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          url: info.url,
          title: info.title,
          elements: info.elements,
          screenshot: info.screenshot || null,
          loading: false,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
    }
  }, [state.sessionId]);

  const click = useCallback(async (elementId: number) => {
    if (!state.sessionId) return;
    setState(prev => ({ ...prev, loading: true, error: null, description: null }));

    try {
      const res = await fetch('/api/pi/web/click', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ elementId, sessionId: state.sessionId }),
      });
      if (!res.ok) throw new Error(`Click failed: ${res.statusText}`);
      const info: PageInfo = await res.json();
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          url: info.url,
          title: info.title,
          elements: info.elements,
          screenshot: info.screenshot || null,
          loading: false,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
    }
  }, [state.sessionId]);

  const type = useCallback(async (elementId: number, text: string, submit = false) => {
    if (!state.sessionId) return;
    setState(prev => ({ ...prev, loading: true, error: null, description: null }));

    try {
      const res = await fetch('/api/pi/web/type', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ elementId, text, submit, sessionId: state.sessionId }),
      });
      if (!res.ok) throw new Error(`Type failed: ${res.statusText}`);
      const info: PageInfo = await res.json();
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          url: info.url,
          title: info.title,
          elements: info.elements,
          screenshot: info.screenshot || null,
          loading: false,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
    }
  }, [state.sessionId]);

  const scroll = useCallback(async (direction: 'up' | 'down', amount = 300) => {
    if (!state.sessionId) return;
    setState(prev => ({ ...prev, loading: true, error: null }));

    try {
      const res = await fetch('/api/pi/web/scroll', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ direction, amount, sessionId: state.sessionId }),
      });
      if (!res.ok) throw new Error(`Scroll failed: ${res.statusText}`);
      const info: PageInfo = await res.json();
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          url: info.url,
          title: info.title,
          elements: info.elements,
          screenshot: info.screenshot || null,
          loading: false,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
    }
  }, [state.sessionId]);

  const describe = useCallback(async () => {
    if (!state.sessionId) return;
    setState(prev => ({ ...prev, loading: true, error: null }));

    try {
      const res = await fetch('/api/pi/web/describe', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId: state.sessionId }),
      });
      if (!res.ok) throw new Error(`Describe failed: ${res.statusText}`);
      const data = await res.json();
      if (mountedRef.current) {
        setState(prev => ({
          ...prev,
          description: data.description,
          loading: false,
        }));
      }
    } catch (err) {
      if (mountedRef.current) {
        setState(prev => ({ ...prev, loading: false, error: String(err) }));
      }
    }
  }, [state.sessionId]);

  return {
    ...state,
    navigate,
    click,
    type,
    scroll,
    describe,
  };
}
