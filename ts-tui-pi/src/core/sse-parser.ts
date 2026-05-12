/**
 * SSE parser — shared between TUI (PuxAgentSession) and Web (chat-panel).
 *
 * Takes raw SSE text chunks and yields structured {event, data} pairs.
 * Both TUI and Web use the same parsing logic, same event names.
 */

export interface SSEEvent {
	event: string;
	data: any;
}

/**
 * Parse a raw SSE text buffer into structured events.
 * Handles incremental parsing (partial lines across chunks).
 *
 * Usage:
 *   const parser = new SSEParser();
 *   while (true) {
 *     const { done, value } = await reader.read();
 *     if (done) break;
 *     for (const evt of parser.feed(decoder.decode(value, { stream: true }))) {
 *       handleEvent(evt);
 *     }
 *   }
 */
export class SSEParser {
	private buffer = "";

	feed(chunk: string): SSEEvent[] {
		this.buffer += chunk;
		const events: SSEEvent[] = [];
		const lines = this.buffer.split("\n");
		this.buffer = lines.pop() || "";

		let currentEvent = "";

		for (const line of lines) {
			const t = line.trimEnd();
			if (t.startsWith("event: ")) {
				currentEvent = t.slice(7).trim();
			} else if (t.startsWith("event:")) {
				currentEvent = t.slice(6).trim();
			} else if (t.startsWith("data: ")) {
				const raw = t.slice(6).trim();
				if (raw === "[DONE]") {
					currentEvent = "";
					continue;
				}
				if (currentEvent) {
					try {
						events.push({ event: currentEvent, data: JSON.parse(raw) });
					} catch {
						// malformed JSON — skip
					}
				}
				currentEvent = "";
			}
			if (t === "") currentEvent = "";
		}

		return events;
	}
}
