import { PiSSEEvent, parseSSEEvent } from '../lib/pi-events';

/**
 * Read an SSE stream from a fetch Response, split on `\n\n`,
 * parse via parseSSEEvent, and call the callback for each event.
 * Returns a promise that resolves when the stream completes.
 */
export async function readSSEStream(
  response: Response,
  onEvent: (event: PiSSEEvent) => void,
): Promise<void> {
  if (!response.body) return;
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const parts = buffer.split('\n\n');
      buffer = parts.pop() || '';

      for (const part of parts) {
        if (!part.trim()) continue;
        const event = parseSSEEvent(part);
        if (event) onEvent(event);
      }
    }

    // Flush any remaining buffer
    if (buffer.trim()) {
      const event = parseSSEEvent(buffer);
      if (event) onEvent(event);
    }
  } finally {
    reader.releaseLock();
  }
}
