/**
 * Web chat adapter — wraps @pux/shared adapter with slash command interception.
 *
 * Messages starting with / are handled locally (store actions, API calls)
 * without hitting the Go backend. Everything else delegates to the shared adapter.
 */

import { puxChatAdapter } from "@pux/shared";
import type { ChatModelAdapter } from "@assistant-ui/react";
import { executeWebCommand, parseCommand } from "./commands";

function extractUserText(
	content: unknown,
): string {
	if (typeof content === "string") return content;
	if (Array.isArray(content)) {
		return content
			.filter((p: { type: string }) => p.type === "text")
			.map((p: { text: string }) => p.text)
			.join("");
	}
	return "";
}

export const webChatAdapter: ChatModelAdapter = {
	async *run(params) {
		const messages = params.messages;
		const lastMsg = messages[messages.length - 1];

		// ── Slash command interception ──
		if (lastMsg?.role === "user") {
			const text = extractUserText(lastMsg.content).trim();
			if (text.startsWith("/")) {
				const result = await executeWebCommand(text);
				if (result.type === "handled") {
					if (result.silent) {
						// Silent commands (tab switches) — no message bubble.
						// Yield empty complete so the runtime finishes cleanly.
						yield {
							content: [],
							status: { type: "complete" as const, reason: "stop" as const },
						} as any;
					} else {
						yield {
							content: [
								{
									type: "text" as const,
									text:
										result.message
										|| `Ran /${parseCommand(text)?.command || "?"}`,
								},
							],
							status: {
								type: "complete" as const,
								reason: "stop" as const,
							},
						};
					}
					return;
				}
				// passthrough — send to backend as normal
			}
		}

		// Delegate to shared adapter
		const gen = puxChatAdapter.run(params) as AsyncGenerator<import("@assistant-ui/react").ChatModelRunResult>;
		for await (const chunk of gen) {
			yield chunk;
		}
	},
};
