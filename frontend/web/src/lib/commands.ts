/**
 * Slash command registry for Pux web UI.
 *
 * Commands are intercepted by the chat adapter before hitting the backend.
 * Each command runs locally (store actions, API calls) and returns a result
 * that the adapter yields as an assistant message.
 */

import { usePuxStore } from "@pux/shared";
import { getFetch } from "@pux/shared";
import { apiUrl } from "@pux/shared";

export interface WebCommand {
	name: string;
	description: string;
	/** If true, the command is silent — no assistant message bubble */
	silent?: boolean;
	handler: (args: string) => Promise<CommandResult>;
}

export type CommandResult =
	| { type: "handled"; message?: string }
	| { type: "passthrough" };

// ── Commands ──

const commands: WebCommand[] = [
	{
		name: "help",
		description: "Show available commands",
		handler: async () => ({
			type: "handled",
			message:
				"**Commands:**\n"
				+ commands
					.filter((c) => !c.silent)
					.map((c) => `  \`/${c.name.padEnd(14)}\` — ${c.description}`)
					.join("\n"),
		}),
	},
	{
		name: "clear",
		description: "Clear conversation history",
		silent: true,
		handler: async () => {
			usePuxStore.getState().clearConversation();
			return { type: "handled" };
		},
	},
	{
		name: "compact",
		description: "Compact context to free token budget",
		handler: async () => {
			try {
				const store = usePuxStore.getState();
				const fetch = getFetch();
				const resp = await fetch(apiUrl("/api/pux/compact"), {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({
						project: store.activeProject || "default",
						agentId: store.activeAgentId || "default",
					}),
				});
				if (!resp.ok) {
					return { type: "handled", message: "Compact failed." };
				}
				const data = await resp.json();
				if (data.status === "noop") {
					return { type: "handled", message: `Nothing to compact — ${data.message || "session too small"}` };
				}
				if (data.status === "error") {
					return { type: "handled", message: `Compact error: ${data.message || "unknown"}` };
				}
				const saved = data.tokensBefore && data.tokensAfter
					? ` (${data.tokensBefore} → ${data.tokensAfter} tokens)`
					: "";
				return {
					type: "handled",
					message: `Context compacted via ${data.compactionType || "summary"}${saved}. ${data.messagesCompacted || 0} messages compacted.`,
				};
			} catch {
				return { type: "handled", message: "Backend unreachable." };
			}
		},
	},
	{
		name: "new",
		description: "Start a new conversation",
		silent: true,
		handler: async () => {
			usePuxStore.getState().startNewChat();
			return { type: "handled" };
		},
	},
	{
		name: "status",
		description: "Show session status",
		handler: async () => {
			const store = usePuxStore.getState();
			const lines = [
				`**Project:** ${store.activeProject || "(none)"}`,
				`**Model:** ${store.activeModel || "default"}`,
				`**Conversation:** ${store.activeAgentId || "(new)"}`,
			];
			if (store.lastUsage) {
				lines.push(
					`**Tokens:** in:${store.lastUsage.input} out:${store.lastUsage.output}`,
				);
			}
			if (store.contextMetrics) {
				lines.push(
					`**Context:** ${Math.round(store.contextMetrics.contextUtil * 100)}%`,
				);
			}
			const agents = [...store.agents.values()];
			if (agents.length > 0) {
				const running = agents.filter((a) => a.status === "running").length;
				lines.push(`**Agents:** ${running} running, ${agents.length} total`);
			}
			return { type: "handled", message: lines.join("\n") };
		},
	},
	// ── Workbench tab shortcuts (silent — no message bubble) ──
	{
		name: "vnc",
		description: "Switch to sandbox tab",
		silent: true,
		handler: async () => {
			usePuxStore.getState().setWorkbenchTab("vnc");
			return { type: "handled" };
		},
	},
	{
		name: "editor",
		description: "Switch to editor tab",
		silent: true,
		handler: async () => {
			usePuxStore.getState().setWorkbenchTab("editor");
			return { type: "handled" };
		},
	},
	{
		name: "scheduler",
		description: "Switch to scheduler tab",
		silent: true,
		handler: async () => {
			usePuxStore.getState().setWorkbenchTab("scheduler");
			return { type: "handled" };
		},
	},
	{
		name: "agents",
		description: "Switch to agents tab",
		silent: true,
		handler: async () => {
			usePuxStore.getState().setWorkbenchTab("workers");
			return { type: "handled" };
		},
	},
	{
		name: "settings",
		description: "Switch to settings tab",
		silent: true,
		handler: async () => {
			usePuxStore.getState().setWorkbenchTab("settings");
			return { type: "handled" };
		},
	},
];

// ── Parser ──

export function parseCommand(
	input: string,
): { command: string; args: string } | null {
	const trimmed = input.trim();
	if (!trimmed.startsWith("/")) return null;
	const spaceIdx = trimmed.indexOf(" ");
	if (spaceIdx === -1) {
		return { command: trimmed.slice(1).toLowerCase(), args: "" };
	}
	return {
		command: trimmed.slice(1, spaceIdx).toLowerCase(),
		args: trimmed.slice(spaceIdx + 1),
	};
}

export async function executeWebCommand(
	input: string,
): Promise<CommandResult & { silent?: boolean }> {
	const parsed = parseCommand(input);
	if (!parsed) return { type: "passthrough" };

	const cmd = commands.find((c) => c.name === parsed.command);
	if (!cmd) {
		return {
			type: "handled",
			message: `Unknown command: \`/${parsed.command}\`\nType \`/help\` for available commands.`,
		};
	}

	const result = await cmd.handler(parsed.args);
	return { ...result, silent: cmd.silent };
}

export function getWebCommandNames(): string[] {
	return commands.map((c) => c.name);
}

export function getWebCommands(): WebCommand[] {
	return commands;
}
