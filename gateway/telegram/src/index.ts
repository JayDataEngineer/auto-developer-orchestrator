/**
 * Pux Telegram Gateway — bridges Telegram messages to the Pux agent.
 *
 * Receives messages via Telegram Bot API long polling, forwards them
 * to the Go backend via /api/pux/prompt, and sends the response back.
 *
 * Usage:
 *   TELEGRAM_BOT_TOKEN=xxx bun run src/index.ts
 */

import { PuxClient } from "../../shared/pux-client";

// ── Config ──

const BOT_TOKEN = process.env.TELEGRAM_BOT_TOKEN ?? "";
const BACKEND_URL = process.env.PUX_BACKEND_URL ?? "http://localhost:3847";
const PROJECT = process.env.PUX_PROJECT ?? "default";
const ORG = process.env.PUX_ORG ?? "";
const MAX_RESPONSE_LENGTH = 4096; // Telegram message limit

if (!BOT_TOKEN) {
	console.error("TELEGRAM_BOT_TOKEN is required");
	process.exit(1);
}

const API = `https://api.telegram.org/bot${BOT_TOKEN}`;
const client = new PuxClient({ backendUrl: BACKEND_URL, project: PROJECT, org: ORG });

// ── Session tracking ──

// Track last message per chat for reply threading
const lastBotMessage = new Map<number, number>();

// ── Telegram API helpers ──

interface TelegramUpdate {
	update_id: number;
	message?: {
		message_id: number;
		chat: { id: number; type: string };
		text?: string;
		from?: { id: number; first_name: string; username?: string };
	};
}

async function getUpdates(offset?: number): Promise<TelegramUpdate[]> {
	const params = new URLSearchParams({ timeout: "30" });
	if (offset) params.set("offset", String(offset));
	if (!offset) params.set("allowed_updates", JSON.stringify(["message"]));

	const resp = await fetch(`${API}/getUpdates?${params}`);
	const data = (await resp.json()) as { ok: boolean; result: TelegramUpdate[] };
	if (!data.ok) {
		console.error("getUpdates failed:", data);
		return [];
	}
	return data.result;
}

async function sendMessage(chatId: number, text: string, replyTo?: number): Promise<void> {
	// Split long messages
	const chunks = splitMessage(text);

	for (const chunk of chunks) {
		const body: Record<string, any> = {
			chat_id: chatId,
			text: chunk,
			parse_mode: "Markdown",
		};
		if (replyTo) body.reply_to_message_id = replyTo;

		try {
			const resp = await fetch(`${API}/sendMessage`, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify(body),
			});
			const data = (await resp.json()) as { ok: boolean; result?: { message_id: number } };
			if (data.ok && data.result) {
				lastBotMessage.set(chatId, data.result.message_id);
			}
		} catch (err) {
			console.error("sendMessage failed:", err);
		}
	}
}

async function sendTyping(chatId: number): Promise<void> {
	try {
		await fetch(`${API}/sendChatAction`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ chat_id: chatId, action: "typing" }),
		});
	} catch {
		// best effort
	}
}

function splitMessage(text: string): string[] {
	if (text.length <= MAX_RESPONSE_LENGTH) return [text];

	const chunks: string[] = [];
	let remaining = text;
	while (remaining.length > 0) {
		let cut = MAX_RESPONSE_LENGTH;
		if (remaining.length > cut) {
			// Try to cut at a newline
			const nl = remaining.lastIndexOf("\n", cut);
			if (nl > cut * 0.5) cut = nl;
		}
		chunks.push(remaining.slice(0, cut));
		remaining = remaining.slice(cut);
	}
	return chunks;
}

// ── Typing indicator loop ──

const activeChats = new Set<number>();

function startTypingLoop(chatId: number) {
	if (activeChats.has(chatId)) return;
	activeChats.add(chatId);

	const interval = setInterval(async () => {
		if (!activeChats.has(chatId)) {
			clearInterval(interval);
			return;
		}
		await sendTyping(chatId);
	}, 4000); // Telegram typing lasts ~5s, refresh every 4s
}

function stopTypingLoop(chatId: number) {
	activeChats.delete(chatId);
}

// ── Main poll loop ──

async function handleUpdate(update: TelegramUpdate): Promise<void> {
	const msg = update.message;
	if (!msg?.text) return;

	// Ignore commands (let Telegram handle /start, /help, etc.)
	if (msg.text.startsWith("/")) {
		if (msg.text === "/start" || msg.text === "/help") {
			await sendMessage(
				msg.chat.id,
				"Hello! I'm Pux, your AI assistant. Send me any message and I'll help you out.",
			);
		}
		return;
	}

	const chatId = msg.chat.id;
	const userName = msg.from?.first_name ?? "User";
	console.log(`[telegram] ${userName}: ${msg.text}`);

	// Show typing indicator while agent processes
	startTypingLoop(chatId);

	try {
		const result = await client.prompt(msg.text);
		stopTypingLoop(chatId);

		if (result.error) {
			await sendMessage(chatId, `Error: ${result.error}`, msg.message_id);
			return;
		}

		const responseText = result.text || "(no response)";
		const replyTo = msg.message_id;
		await sendMessage(chatId, responseText, replyTo);

		console.log(`[telegram] responded in ${result.durationMs}ms (${result.toolCalls} tool calls)`);
	} catch (err) {
		stopTypingLoop(chatId);
		console.error("[telegram] prompt error:", err);
		await sendMessage(chatId, "Sorry, I encountered an error processing your request.", msg.message_id);
	}
}

async function main() {
	console.log(`[telegram] starting gateway → ${BACKEND_URL}`);

	// Verify bot token
	const me = await fetch(`${API}/getMe`);
	const meData = (await me.json()) as { ok: boolean; result?: { username: string } };
	if (!meData.ok) {
		console.error("Invalid bot token");
		process.exit(1);
	}
	console.log(`[telegram] connected as @${meData.result?.username}`);

	let offset: number | undefined;

	// Long polling loop
	while (true) {
		try {
			const updates = await getUpdates(offset);
			for (const update of updates) {
				offset = update.update_id + 1;
				// Handle each update concurrently (don't block the poll loop)
				handleUpdate(update).catch((err) => console.error("[telegram] handler error:", err));
			}
		} catch (err) {
			console.error("[telegram] poll error:", err);
			// Back off briefly on errors
			await new Promise((r) => setTimeout(r, 5000));
		}
	}
}

main();
