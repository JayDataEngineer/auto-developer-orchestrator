import { useState, useEffect, useRef, useCallback } from 'react';

/**
 * Shared polling hook. Calls fetchFn immediately, then every intervalMs.
 * Cleans up interval on unmount or when enabled becomes false.
 *
 * Only sets loading=true on the initial fetch — background polls run silently
 * to avoid triggering re-renders that reset scroll position, animations, etc.
 */
export function usePolling(
  fetchFn: () => Promise<void>,
  intervalMs: number,
  enabled: boolean,
) {
  const [loading, setLoading] = useState(false);
  const initialDone = useRef(false);
  const fetchRef = useRef(fetchFn);
  fetchRef.current = fetchFn;

  const doFetch = useCallback(async (isInitial: boolean) => {
    try {
      if (isInitial) setLoading(true);
      await fetchRef.current();
    } finally {
      if (isInitial) {
        setLoading(false);
        initialDone.current = true;
      }
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;
    initialDone.current = false;
    doFetch(true);
    const id = setInterval(() => doFetch(false), intervalMs);
    return () => clearInterval(id);
  }, [enabled, intervalMs, doFetch]);

  return { loading };
}
