/**
 * Message queue for non-blocking prompt submission.
 *
 * Users can queue messages while the agent is streaming. When the current
 * response completes, the next queued message is auto-dispatched.
 *
 * Ported from agent-desktop's chatStore queue pattern.
 */

import { useState, useCallback, useRef } from 'react';

export interface QueuedMessage {
  id: string;
  text: string;
  model?: string;
  thinkingLevel?: string;
  autoBranch?: boolean;
  autoMerge?: boolean;
  queuedAt: number;
}

export interface MessageQueueState {
  items: QueuedMessage[];
  paused: boolean;
}

export function useMessageQueue(
  onDequeue: (msg: QueuedMessage) => Promise<void>,
  isStreaming: boolean,
) {
  const [queue, setQueue] = useState<QueuedMessage[]>([]);
  const [paused, setPaused] = useState(false);
  const processingRef = useRef(false);
  const counterRef = useRef(0);

  const enqueue = useCallback(
    (text: string, opts?: { model?: string; thinkingLevel?: string; autoBranch?: boolean; autoMerge?: boolean }) => {
      const msg: QueuedMessage = {
        id: `q-${++counterRef.current}-${Date.now()}`,
        text,
        model: opts?.model,
        thinkingLevel: opts?.thinkingLevel,
        autoBranch: opts?.autoBranch,
        autoMerge: opts?.autoMerge,
        queuedAt: Date.now(),
      };
      setQueue(prev => [...prev, msg]);
      return msg.id;
    },
    [],
  );

  const remove = useCallback((id: string) => {
    setQueue(prev => prev.filter(m => m.id !== id));
  }, []);

  const clear = useCallback(() => {
    setQueue([]);
  }, []);

  const pause = useCallback(() => setPaused(true), []);
  const resume = useCallback(() => setPaused(false), []);

  const processNext = useCallback(async () => {
    if (processingRef.current || paused || isStreaming) return;

    setQueue(prev => {
      if (prev.length === 0) return prev;

      const [next, ...rest] = prev;
      processingRef.current = true;

      // Fire and forget — the caller's sendPrompt will set isStreaming=true
      // which prevents further processing until it completes.
      onDequeue(next)
        .catch(() => {
          // Dequeue failed — processing continues regardless
        })
        .finally(() => {
          processingRef.current = false;
        });

      return rest;
    });
  }, [paused, isStreaming, onDequeue]);

  // Auto-process: when streaming stops and queue has items, dispatch next.
  // The component using this hook should call processNext in a useEffect
  // that watches isStreaming transitions from true → false.
  const tryProcessNext = useCallback(() => {
    if (!isStreaming && !paused && !processingRef.current) {
      processNext();
    }
  }, [isStreaming, paused, processNext]);

  return {
    queue,
    paused,
    enqueue,
    remove,
    clear,
    pause,
    resume,
    tryProcessNext,
  };
}
