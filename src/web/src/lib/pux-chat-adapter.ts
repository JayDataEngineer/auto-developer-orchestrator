/**
 * Web chat adapter — wraps @pux/shared adapter with slash command interception.
 *
 * Messages starting with / are handled locally (store actions, API calls)
 * without hitting the Go backend. Everything else delegates to the shared adapter.
 */

import { puxChatAdapter } from "@pux/shared";
import type { ChatModelAdapter, ChatModelRunResult } from "@assistant-ui/react";
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
				try {
					const result = await executeWebCommand(text);
					if (result.type === "handled") {
						if (result.silent) {
							// Silent commands (tab switches, /clear, /new) — no message bubble.
							// Omit content so no ghost assistant message renders.
							const done: ChatModelRunResult = {
								status: { type: "complete", reason: "stop" },
							};
							yield done;
						} else {
							const response: ChatModelRunResult = {
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
							yield response;
						}
						return;
					}
					// passthrough — send to backend as normal
				} catch (err) {
					// Command handler threw — show error instead of breaking the stream
					const errorResult: ChatModelRunResult = {
						content: [
							{
								type: "text" as const,
								text: `Command error: ${err instanceof Error ? err.message : String(err)}`,
							},
						],
						status: { type: "complete" as const, reason: "stop" as const },
					};
					yield errorResult;
					return;
				}
			}
		}

		// Delegate to shared adapter
		const gen = puxChatAdapter.run(params) as AsyncGenerator<import("@assistant-ui/react").ChatModelRunResult>;
		for await (const chunk of gen) {
			yield chunk;
		}
	},
};
