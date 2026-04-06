/**
 * MCP Bridge Extension
 *
 * Exposes MCP (Model Context Protocol) server tools as native Pi tools.
 *
 * MCP servers are configured in .pi/mcp-servers.json:
 * {
 *   "mcpServers": {
 *     "github": {
 *       "transport": "stdio",
 *       "command": "npx",
 *       "args": ["-y", "@modelcontextprotocol/server-github"],
 *       "env": { "GITHUB_TOKEN": "..." }
 *     },
 *     "fetch": {
 *       "transport": "sse",
 *       "url": "https://example.com/mcp"
 *     }
 *   }
 * }
 *
 * This extension reads the config, starts stdio MCP servers as subprocesses,
 * calls tools/list to discover their tools, and registers each one as a Pi tool.
 */

import type { ExtensionAPI, ExtensionContext, Theme } from "@mariozechner/pi-coding-agent";
import { Text } from "@mariozechner/pi-tui";
import { Type } from "@sinclair/typebox";
import { spawn, ChildProcess } from "node:child_process";
import * as fs from "node:fs";
import * as path from "node:path";

// ─── MCP Config Types ──────────────────────────────────────────

interface MCPServerConfig {
	transport: "stdio" | "sse";
	command?: string;
	args?: string[];
	env?: Record<string, string>;
	url?: string;
}

interface MCPConfig {
	mcpServers: Record<string, MCPServerConfig>;
}

interface MCPTool {
	name: string;
	description: string;
	inputSchema?: Record<string, unknown>;
	serverName: string;
}

// ─── Stdio MCP Client ──────────────────────────────────────────

class StdioMCPClient {
	private proc: ChildProcess;
	private buffer = "";
	private requestId = 0;
	private pendingRequests = new Map<number, { resolve: (v: any) => void; reject: (e: Error) => void }>();
	private tools: MCPTool[] = [];
	ready = false;

	constructor(
		private serverName: string,
		command: string,
		args: string[],
		env: Record<string, string>,
	) {
		this.proc = spawn(command, args, {
			env: { ...process.env, ...env },
			stdio: ["pipe", "pipe", "pipe"],
		});

		this.proc.stdout?.on("data", (data) => this.handleData(data.toString()));
		this.proc.stderr?.on("data", (data) => {
			// Log MCP server stderr
		});

		// Initialize
		this.initialize();
	}

	private handleData(chunk: string): void {
		this.buffer += chunk;
		const lines = this.buffer.split("\n");
		this.buffer = lines.pop() || "";

		for (const line of lines) {
			if (!line.trim()) continue;
			try {
				const msg = JSON.parse(line);
				if (msg.id !== undefined) {
					const pending = this.pendingRequests.get(msg.id);
					if (pending) {
						this.pendingRequests.delete(msg.id);
						if (msg.error) pending.reject(new Error(msg.error.message));
						else pending.resolve(msg.result);
					}
				}
			} catch {
				// ignore parse errors
			}
		}
	}

	private async send(method: string, params: Record<string, unknown> = {}): Promise<any> {
		const id = ++this.requestId;
		const msg = { jsonrpc: "2.0", id, method, params };
		return new Promise((resolve, reject) => {
			this.pendingRequests.set(id, { resolve, reject });
			this.proc.stdin?.write(JSON.stringify(msg) + "\n");
		});
	}

	private async initialize(): Promise<void> {
		try {
			await this.send("initialize", {
				protocolVersion: "2024-11-05",
				capabilities: {},
				clientInfo: { name: "auto-developer-orchestrator", version: "1.0.0" },
			});
			// Send initialized notification
			this.proc.stdin?.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
			// List tools
			const result = await this.send("tools/list");
			if (result?.tools) {
				this.tools = result.tools.map((t: any) => ({
					name: t.name,
					description: t.description,
					inputSchema: t.inputSchema,
					serverName: this.serverName,
				}));
			}
			this.ready = true;
		} catch {
			this.ready = false;
		}
	}

	getTools(): MCPTool[] {
		return this.tools;
	}

	async callTool(name: string, args: Record<string, unknown>): Promise<string> {
		const result = await this.send("tools/call", { name, arguments: args });
		if (result?.content) {
			return result.content
				.filter((c: any) => c.type === "text")
				.map((c: any) => c.text)
				.join("\n");
		}
		if (result?.isError) {
			throw new Error("MCP tool returned error");
		}
		return "";
	}

	close(): void {
		this.proc.kill();
	}
}

// ─── Extension ─────────────────────────────────────────────────

export default function (pi: ExtensionAPI) {
	const mcpClients = new Map<string, StdioMCPClient>();

	// Load MCP config on session start
	pi.on("session_start", async (_event, ctx) => {
		const configPath = path.join(ctx.cwd, ".pi", "mcp-servers.json");
		if (!fs.existsSync(configPath)) return;

		let config: MCPConfig;
		try {
			config = JSON.parse(fs.readFileSync(configPath, "utf-8"));
		} catch {
			return;
		}

		if (!config.mcpServers) return;

		for (const [name, serverConfig] of Object.entries(config.mcpServers)) {
			try {
				if (serverConfig.transport === "stdio" && serverConfig.command) {
					const client = new StdioMCPClient(
						name,
						serverConfig.command,
						serverConfig.args || [],
						serverConfig.env || {},
					);

					// Wait a bit for initialization
					await new Promise((r) => setTimeout(r, 2000));

					if (client.ready && client.getTools().length > 0) {
						mcpClients.set(name, client);

						// Register each MCP tool as a Pi tool
						for (const mcpTool of client.getTools()) {
							const toolName = `mcp_${name}_${mcpTool.name}`;

							// Build TypeBox schema from MCP inputSchema
							const paramsSchema = mcpTool.inputSchema
								? Type.Object({}) // Simplified - MCP schemas are JSON Schema
								: Type.Object({});

							pi.registerTool({
								name: toolName,
								label: `MCP: ${name}/${mcpTool.name}`,
								description: `[MCP: ${name}] ${mcpTool.description}`,
								parameters: paramsSchema,

								async execute(_toolCallId, params, _signal, _onUpdate, _ctx) {
									try {
										const output = await client.callTool(mcpTool.name, params as Record<string, unknown>);
										return {
											content: [{ type: "text", text: output || "(no output)" }],
										};
									} catch (e: any) {
										return {
											content: [{ type: "text", text: `MCP tool failed: ${e.message}` }],
											isError: true,
										};
									}
								},

								renderCall(args, theme) {
									return new Text(
										theme.fg("toolTitle", theme.bold(toolName)),
										0,
										0,
									);
								},

								renderResult(result, _opts, theme) {
									const text = result.content[0];
									return new Text(
										theme.fg("success", "✓ ") +
										theme.fg("muted", text?.type === "text" ? (text.text as string).slice(0, 100) : ""),
										0,
										0,
									);
								},
							});
						}
					} else {
						client.close();
					}
				}
			} catch {
				// Skip failed servers
			}
		}
	});

	// Close MCP clients on shutdown
	pi.on("session_shutdown", async () => {
		for (const client of mcpClients.values()) {
			client.close();
		}
		mcpClients.clear();
	});
}
