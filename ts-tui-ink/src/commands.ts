/**
 * Slash command registry for Pux TUI.
 *
 * Commands are intercepted from the composer input before being
 * sent to the chat adapter. Each command has a handler that
 * performs a local action or calls the backend.
 */

import { usePuxStore } from "@pux/shared";
import { apiUrl } from "@pux/shared";
import { getFetch } from "@pux/shared";

export interface Command {
	name: string;
	description: string;
	usage?: string;
	handler: (args: string, ctx: CommandContext) => Promise<CommandResult>;
}

export interface CommandContext {
	model: string;
	project: string;
	exit: () => void;
	setModel: (m: string) => void;
}

export type CommandResult =
	| { type: "handled"; message?: string }
	| { type: "passthrough" }; // not a command, send to chat

// ── Commands ──

const commands: Command[] = [
	{
		name: "help",
		description: "Show available commands",
		handler: async () => {
			usePuxStore.getState().toggleHelpOverlay();
			return { type: "handled" };
		},
	},
	{
		name: "quit",
		description: "Exit Pux",
		handler: async (_, ctx) => {
			ctx.exit();
			return { type: "handled" };
		},
	},
	{
		name: "clear",
		description: "Clear conversation history",
		handler: async () => {
			usePuxStore.getState().clearConversation();
			return { type: "handled", message: "Conversation cleared." };
		},
	},
	{
		name: "compact",
		description: "Compact context to free token budget",
		handler: async () => {
			const store = usePuxStore.getState();
			try {
				const fetch = getFetch();
				const resp = await fetch(apiUrl(`/api/pux/compact`), {
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ conversation_id: store.activeConversationId }),
				});
				if (resp.ok) {
					return { type: "handled", message: "Context compacted." };
				}
				return { type: "handled", message: "Compact failed." };
			} catch {
				return { type: "handled", message: "Backend unreachable." };
			}
		},
	},
	{
		name: "new",
		description: "Start a new conversation",
		handler: async () => {
			usePuxStore.getState().clearConversation();
			return { type: "handled", message: "New conversation started." };
		},
	},
	{
		name: "search",
		description: "Search through conversation messages",
		handler: async () => {
			usePuxStore.getState().toggleSearchOverlay();
			return { type: "handled" };
		},
	},
	{
		name: "files",
		description: "Open file picker to browse and attach files",
		handler: async () => {
			usePuxStore.getState().toggleFilePicker();
			return { type: "handled" };
		},
	},
	{
		name: "logs",
		description: "Open diagnostics log viewer",
		handler: async () => {
			usePuxStore.getState().toggleLogViewer();
			return { type: "handled" };
		},
	},
	{
		name: "sessions",
		description: "Quick switch between conversations",
		handler: async () => {
			usePuxStore.getState().toggleSessionSwitcher();
			return { type: "handled" };
		},
	},
	{
		name: "settings",
		description: "Open settings panel (model, theme, system info)",
		handler: async () => {
			usePuxStore.getState().toggleSettingsOverlay();
			return { type: "handled" };
		},
	},
	{
		name: "mcp",
		description: "View MCP server status and registered tools",
		handler: async () => {
			usePuxStore.getState().toggleMCPOverlay();
			return { type: "handled" };
		},
	},
	{
		name: "model",
		description: "Browse providers and switch model",
		handler: async () => {
			usePuxStore.getState().toggleProvidersOverlay();
			return { type: "handled" };
		},
	},
	{
		name: "status",
		description: "Show session status",
		handler: async (_, ctx) => {
			const store = usePuxStore.getState();
			const agents = [...store.agents.values()];
			const lines = [
				`  Project:   ${ctx.project}`,
				`  Model:     ${ctx.model}`,
				`  Conversation: ${store.activeConversationId || "(new)"}`,
				`  View:      ${store.activeTuiView}`,
			];
			if (store.lastUsage) {
				lines.push(`  Tokens:    in:${store.lastUsage.input} out:${store.lastUsage.output}`);
			}
			if (store.contextMetrics) {
				lines.push(`  Context:   ${Math.round(store.contextMetrics.contextUtil * 100)}%`);
			}
			if (agents.length > 0) {
				const running = agents.filter((a) => a.status === "running").length;
				lines.push(`  Agents:    ${running} running, ${agents.length} total`);
			}
			return { type: "handled", message: lines.join("\n") };
		},
	},
	// ── View switching commands ──
	{
		name: "chat",
		description: "Switch to chat view",
		handler: async () => {
			usePuxStore.getState().setTuiView("chat");
			return { type: "handled" };
		},
	},
	{
		name: "agents",
		description: "Switch to agents view",
		handler: async () => {
			usePuxStore.getState().setTuiView("agents");
			return { type: "handled" };
		},
	},
	{
		name: "tools",
		description: "Switch to tools view",
		handler: async () => {
			usePuxStore.getState().setTuiView("tools");
			return { type: "handled" };
		},
	},
	{
		name: "conversations",
		description: "Switch to conversations view",
		handler: async () => {
			usePuxStore.getState().setTuiView("conversations");
			return { type: "handled" };
		},
	},
];

// ── Parser ──

export function parseCommand(input: string): { command: string; args: string } | null {
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

export async function executeCommand(
	input: string,
	ctx: CommandContext,
): Promise<CommandResult> {
	const parsed = parseCommand(input);
	if (!parsed) return { type: "passthrough" };

	const cmd = commands.find((c) => c.name === parsed.command);
	if (!cmd) {
		return {
			type: "handled",
			message: `Unknown command: /${parsed.command}\nType /help for available commands.`,
		};
	}

	return cmd.handler(parsed.args, ctx);
}

export function getCommandNames(): string[] {
	return commands.map((c) => c.name);
}

export function getCommands(): Command[] {
	return commands;
}
