/**
 * Pux SSE Client — shared between all messaging gateway adapters.
 *
 * Sends a prompt to the Go backend and streams the SSE response,
 * collecting the full text for delivery to the messaging platform.
 */

export interface PuxClientConfig {
	backendUrl: string; // e.g. "http://localhost:3847"
	project?: string;
	org?: string;
	model?: string;
}

export interface PuxResponse {
	text: string;
	thinking: string;
	toolCalls: number;
	durationMs: number;
	error?: string;
}

export class PuxClient {
	private config: PuxClientConfig;

	constructor(config: PuxClientConfig) {
		this.config = config;
	}

	/**
	 * Send a message to the Pux backend and stream the response.
	 * Returns the full response text once the agent finishes.
	 */
	async prompt(
		message: string,
		opts?: { project?: string; sessionId?: string },
	): Promise<PuxResponse> {
		const payload: Record<string, any> = {
			message,
			project: opts?.project ?? this.config.project ?? "default",
		};
		if (this.config.model) payload.model = this.config.model;
		if (this.config.org) payload.org = this.config.org;

		const resp = await fetch(`${this.config.backendUrl}/api/pux/prompt`, {
			method: "POST",
			headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
			body: JSON.stringify(payload),
		});

		if (!resp.ok) {
			const body = await resp.text();
			return { text: "", thinking: "", toolCalls: 0, durationMs: 0, error: `HTTP ${resp.status}: ${body}` };
		}

		return this.parseSSE(resp);
	}

	/**
	 * Parse the SSE stream from the Go backend, collecting text deltas
	 * and waiting for the agent_end event.
	 */
	private async parseSSE(resp: Response): Promise<PuxResponse> {
		const result: PuxResponse = { text: "", thinking: "", toolCalls: 0, durationMs: 0 };
		const reader = resp.body?.getReader();
		if (!reader) return { ...result, error: "No response body" };

		const decoder = new TextDecoder();
		let buffer = "";
		const startTime = Date.now();

		try {
			while (true) {
				const { done, value } = await reader.read();
				if (done) break;

				buffer += decoder.decode(value, { stream: true });
				const lines = buffer.split("\n");
				buffer = lines.pop() ?? ""; // keep incomplete line

				let eventType = "";
				for (const line of lines) {
					if (line.startsWith("event: ")) {
						eventType = line.slice(7).trim();
					} else if (line.startsWith("data: ")) {
						const dataStr = line.slice(6);
						try {
							const data = JSON.parse(dataStr);
							this.handleEvent(eventType, data, result);
						} catch {
							// skip malformed JSON
						}
						eventType = "";
					}
				}
			}
		} finally {
			reader.releaseLock();
			result.durationMs = Date.now() - startTime;
		}

		return result;
	}

	private handleEvent(eventType: string, data: any, result: PuxResponse): void {
		switch (eventType) {
			case "text_delta":
				result.text += data.text ?? "";
				break;
			case "thinking_delta":
				result.thinking += data.text ?? "";
				break;
			case "tool_execution_start":
				result.toolCalls++;
				break;
			case "agent_end":
				// Agent finished — use the output text if we didn't collect deltas
				if (!result.text && data.output) {
					result.text = data.output;
				}
				break;
			case "error":
				result.error = data.error ?? "unknown error";
				break;
		}
	}
}
