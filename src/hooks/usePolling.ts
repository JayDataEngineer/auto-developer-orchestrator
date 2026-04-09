import { useState, useEffect, useRef, useCallback } from 'react';

/**
 * Shared polling hook. Calls fetchFn immediately, then every intervalMs.
 * Cleans up interval on unmount or when enabled becomes false.
 */
export function usePolling(
  fetchFn: () => Promise<void>,
  intervalMs: number,
  enabled: boolean,
) {
  const [loading, setLoading] = useState(false);
  const fetchRef = useRef(fetchFn);
  fetchRef.current = fetchFn;

  const wrappedFetch = useCallback(async () => {
    try {
      setLoading(true);
      await fetchRef.current();
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;
    wrappedFetch();
    const id = setInterval(wrappedFetch, intervalMs);
    return () => clearInterval(id);
  }, [enabled, intervalMs, wrappedFetch]);

  return { loading };
}
