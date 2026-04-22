/**
 * RAF-based text/thinking delta batching hook.
 * Accumulates streaming deltas in refs and flushes them once per animation frame.
 */

import { useCallback, useRef, useEffect } from 'react';
import type { PuxSSEEvent } from '../lib/pux-events';

export function useThrottledDeltas(
  onFlush: (textDelta: string, thinkingDelta: string) => void,
) {
  const pendingTextRef = useRef('');
  const pendingThinkingRef = useRef('');
  const flushRafRef = useRef<number | null>(null);
  const onFlushRef = useRef(onFlush);
  onFlushRef.current = onFlush;

  // Flush accumulated text/thinking deltas (called via RAF)
  const flush = useCallback(() => {
    flushRafRef.current = null;
    const textDelta = pendingTextRef.current;
    const thinkingDelta = pendingThinkingRef.current;
    if (!textDelta && !thinkingDelta) return;

    pendingTextRef.current = '';
    pendingThinkingRef.current = '';
    onFlushRef.current(textDelta, thinkingDelta);
  }, []);

  // Flush synchronously (for non-delta events that need up-to-date state)
  const syncFlush = useCallback(() => {
    if (flushRafRef.current !== null) {
      cancelAnimationFrame(flushRafRef.current);
      flushRafRef.current = null;
    }
    flush();
  }, [flush]);

  // If this is a text_delta or thinking_delta, buffer it and schedule a RAF flush
  const accumulate = useCallback(
    (event: PuxSSEEvent): boolean => {
      if (event.type === 'text_delta') {
        pendingTextRef.current += (event.data as { text: string }).text;
        if (flushRafRef.current === null) {
          flushRafRef.current = requestAnimationFrame(flush);
        }
        return true;
      }
      if (event.type === 'thinking_delta') {
        pendingThinkingRef.current += (event.data as { text: string }).text;
        if (flushRafRef.current === null) {
          flushRafRef.current = requestAnimationFrame(flush);
        }
        return true;
      }
      return false;
    },
    [flush],
  );

  // Cleanup RAF on unmount
  useEffect(() => {
    return () => {
      if (flushRafRef.current !== null) {
        cancelAnimationFrame(flushRafRef.current);
      }
    };
  }, []);

  return { accumulate, syncFlush };
}
