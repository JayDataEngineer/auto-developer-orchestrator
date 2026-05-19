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
				const fetch = getFetch();
				const resp = await fetch(apiUrl("/api/pux/compact"), {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({
						conversation_id: usePuxStore.getState().activeConversationId,
					}),
				});
				return {
					type: "handled",
					message: resp.ok ? "Context compacted." : "Compact failed.",
				};
			} catch {
				return { type: "handled", message: "Backend unreachable." };
			}
		},
	},
	{
		name: "new",
		description: "Start a new conversation",
		handler: async () => {
			usePuxStore.getState().startNewChat();
			return { type: "handled" };
		},
	},
	{
		name: "model",
		description: "Open model picker",
		handler: async () => {
			usePuxStore.getState().toggleModelPicker();
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
				`**Conversation:** ${store.activeConversationId || "(new)"}`,
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
	{
		name: "history",
		description: "List recent conversations",
		handler: async () => {
			const convos = usePuxStore.getState().conversations;
			if (convos.length === 0) {
				return { type: "handled", message: "No conversations found." };
			}
			const lines = convos
				.slice(0, 10)
				.map(
					(c, i) =>
						`${String(i + 1).padStart(2)}. ${c.title || c.agentId.slice(0, 8)}`,
				)
				.join("\n");
			return {
				type: "handled",
				message: `**Recent conversations:**\n\`\`\`\n${lines}\n\`\`\``,
			};
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
	{
		name: "providers",
		description: "Open provider browser",
		silent: true,
		handler: async () => {
			usePuxStore.getState().toggleProvidersOverlay();
			return { type: "handled" };
		},
	},
	{
		name: "mcp",
		description: "View MCP server status",
		handler: async () => {
			usePuxStore.getState().toggleMCPOverlay();
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
