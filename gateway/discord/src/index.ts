/**
 * Pux Discord Gateway — bridges Discord messages to the Pux agent.
 *
 * Receives messages via Discord Gateway (WebSocket), forwards them
 * to the Go backend via /api/pux/prompt, and sends the response back.
 *
 * Usage:
 *   DISCORD_BOT_TOKEN=xxx bun run src/index.ts
 */

import { PuxClient } from "../../shared/pux-client";

// ── Config ──

const BOT_TOKEN = process.env.DISCORD_BOT_TOKEN ?? "";
const BACKEND_URL = process.env.PUX_BACKEND_URL ?? "http://localhost:3847";
const PROJECT = process.env.PUX_PROJECT ?? "default";
const ORG = process.env.PUX_ORG ?? "";

if (!BOT_TOKEN) {
	console.error("DISCORD_BOT_TOKEN is required");
	process.exit(1);
}

const client = new PuxClient({ backendUrl: BACKEND_URL, project: PROJECT, org: ORG });

// ── Discord Gateway types ──

interface DiscordMessage {
	id: string;
	channel_id: string;
	content: string;
	author: { id: string; username: string; bot?: boolean };
	guild_id?: string;
}

interface DiscordReady {
	user: { id: string; username: string };
	session_id: string;
}

// ── Discord API helpers ──

const API_BASE = "https://discord.com/api/v10";

async function getGatewayUrl(): Promise<string> {
	const resp = await fetch(`${API_BASE}/gateway`);
	const data = (await resp.json()) as { url: string };
	return data.url;
}

async function sendMessage(channelId: string, content: string, messageRef?: string): Promise<void> {
	// Discord limit is 2000 chars — split if needed
	const chunks = splitMessage(content, 2000);

	for (const chunk of chunks) {
		const body: Record<string, any> = { content: chunk };
		if (messageRef) body.message_reference = { message_id: messageRef };

		try {
			await fetch(`${API_BASE}/channels/${channelId}/messages`, {
				method: "POST",
				headers: {
					"Content-Type": "application/json",
					Authorization: `Bot ${BOT_TOKEN}`,
				},
				body: JSON.stringify(body),
			});
		} catch (err) {
			console.error("[discord] sendMessage failed:", err);
		}
	}
}

async function triggerTyping(channelId: string): Promise<void> {
	try {
		await fetch(`${API_BASE}/channels/${channelId}/typing`, {
			method: "POST",
			headers: { Authorization: `Bot ${BOT_TOKEN}` },
		});
	} catch {
		// best effort
	}
}

function splitMessage(text: string, maxLen: number): string[] {
	if (text.length <= maxLen) return [text];
	const chunks: string[] = [];
	let remaining = text;
	while (remaining.length > 0) {
		let cut = maxLen;
		if (remaining.length > cut) {
			const nl = remaining.lastIndexOf("\n", cut);
			if (nl > cut * 0.5) cut = nl;
		}
		chunks.push(remaining.slice(0, cut));
		remaining = remaining.slice(cut);
	}
	return chunks;
}

// ── Typing indicator ──

const typingChannels = new Set<string>();

function startTypingLoop(channelId: string) {
	if (typingChannels.has(channelId)) return;
	typingChannels.add(channelId);
	const interval = setInterval(async () => {
		if (!typingChannels.has(channelId)) {
			clearInterval(interval);
			return;
		}
		await triggerTyping(channelId);
	}, 8000); // Discord typing lasts 10s, refresh every 8s
}

function stopTypingLoop(channelId: string) {
	typingChannels.delete(channelId);
}

// ── Gateway connection ──

let sessionId: string | undefined;
let resumeUrl: string | undefined;
let heartbeatInterval: number | undefined;
let myUserId: string | undefined;
let seq: number | null = null;
let ws: WebSocket | undefined;

async function connect(): Promise<void> {
	const gatewayUrl = await getGatewayUrl();
	const url = resumeUrl
		? `${resumeUrl}?session_id=${sessionId}&resume=${seq}`
		: `${gatewayUrl}/?v=10&encoding=json`;

	ws = new WebSocket(url);

	ws.addEventListener("message", (event) => {
		const payload = JSON.parse(event.data as string) as { op: number; t?: string; d?: any; s?: number };
		handleGatewayPayload(payload).catch((err) => console.error("[discord] handler error:", err));
	});

	ws.addEventListener("close", (event) => {
		console.log(`[discord] connection closed: ${event.code} ${event.reason}`);
		// Clear heartbeat
		if (heartbeatInterval) clearInterval(heartbeatInterval);

		// Reconnect after delay
		setTimeout(() => connect(), 5000);
	});

	ws.addEventListener("error", (event) => {
		console.error("[discord] WebSocket error:", event);
	});
}

async function handleGatewayPayload(payload: { op: number; t?: string; d?: any; s?: number }): Promise<void> {
	if (payload.s) seq = payload.s;

	switch (payload.op) {
		case 10: // Hello
			heartbeatInterval = payload.d.heartbeat_interval;
			startHeartbeat();
			identify();
			break;

		case 11: // Heartbeat ACK
			break;

		case 0: // Dispatch
			await handleDispatch(payload.t ?? "", payload.d);
			break;

		case 7: // Reconnect
			console.log("[discord] server requested reconnect");
			ws?.close(4000);
			break;

		case 9: // Invalid session
			console.log("[discord] invalid session, re-identifying");
			sessionId = undefined;
			resumeUrl = undefined;
			setTimeout(() => identify(), 3000);
			break;
	}
}

function startHeartbeat(): void {
	if (heartbeatInterval) clearInterval(heartbeatInterval);
	const interval = heartbeatInterval!;

	setInterval(() => {
		if (ws?.readyState === WebSocket.OPEN) {
			ws.send(JSON.stringify({ op: 1, d: seq }));
		}
	}, interval);
}

function identify(): void {
	const payload = {
		op: 2,
		d: {
			token: BOT_TOKEN,
			intents: 512, // GUILD_MESSAGES (1 << 9) + DIRECT_MESSAGES (1 << 12)
			properties: { os: "linux", browser: "pux-gateway", device: "pux-gateway" },
		},
	};
	ws?.send(JSON.stringify(payload));
}

async function handleDispatch(eventType: string, data: any): Promise<void> {
	switch (eventType) {
		case "READY": {
			const ready = data as DiscordReady;
			sessionId = ready.session_id;
			myUserId = ready.user.id;
			resumeUrl = undefined; // will be set from resume_gateway_url if available
			console.log(`[discord] connected as ${ready.user.username}#${ready.user.discriminator}`);
			break;
		}

		case "MESSAGE_CREATE": {
			const msg = data as DiscordMessage;

			// Ignore own messages and bot messages
			if (msg.author.bot || msg.author.id === myUserId) return;

			// Only respond to DMs or @mentions in guilds
			if (msg.guild_id) {
				if (!msg.content.includes(`<@${myUserId}>`)) return;
				// Strip the @mention from the message
				msg.content = msg.content.replace(`<@${myUserId}>`, "").trim();
			}

			if (!msg.content) return;

			console.log(`[discord] ${msg.author.username}: ${msg.content}`);

			startTypingLoop(msg.channel_id);

			try {
				const result = await client.prompt(msg.content);
				stopTypingLoop(msg.channel_id);

				if (result.error) {
					await sendMessage(msg.channel_id, `Error: ${result.error}`, msg.id);
					return;
				}

				const responseText = result.text || "(no response)";
				await sendMessage(msg.channel_id, responseText, msg.id);

				console.log(`[discord] responded in ${result.durationMs}ms (${result.toolCalls} tool calls)`);
			} catch (err) {
				stopTypingLoop(msg.channel_id);
				console.error("[discord] prompt error:", err);
				await sendMessage(msg.channel_id, "Sorry, I encountered an error.", msg.id);
			}
			break;
		}
	}
}

// ── Main ──

async function main() {
	console.log(`[discord] starting gateway → ${BACKEND_URL}`);
	await connect();
}

main();
