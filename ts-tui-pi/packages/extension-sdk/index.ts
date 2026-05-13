/**
 * Pux Extension SDK — minimal MCP server for Bun extensions.
 *
 * Usage:
 *   import { createExtension } from "@pux/extension-sdk";
 *   const ext = createExtension("my-extension");
 *   ext.tool("greet", { description: "...", parameters: {...} }, async (params) => {...});
 *   ext.start();
 *
 * The SDK starts an HTTP server on port 0 (OS-assigned), handles MCP protocol
 * (initialize, tools/list, tools/call), and prints PUX_EXT_PORT:<port> to stdout.
 */

// JSON-RPC 2.0 types
interface JSONRPCRequest {
	jsonrpc: "2.0";
	id?: number;
	method: string;
	params?: any;
}

interface JSONRPCResponse {
	jsonrpc: "2.0";
	id: number;
	result?: any;
	error?: { code: number; message: string; data?: any };
}

// Tool definition
interface ToolDef {
	name: string;
	description: string;
	parameters: Record<string, any>;
	handler: (params: any) => Promise<{ content: Array<{ type: string; text?: string; data?: string }> }>;
}

export interface Extension {
	tool(
		name: string,
		opts: { description: string; parameters: Record<string, any> },
		handler: (params: any) => Promise<{ content: Array<{ type: string; text?: string }> }>,
	): void;
	start(): void;
}

/**
 * Create an extension with the given name.
 */
export function createExtension(name: string): Extension {
	const tools = new Map<string, ToolDef>();

	return {
		tool(name, opts, handler) {
			tools.set(name, {
				name,
				description: opts.description,
				parameters: opts.parameters,
				handler,
			});
		},

		start() {
			const server = Bun.serve({
				port: 0,
				async fetch(req) {
					if (req.method !== "POST") {
						return new Response("Method not allowed", { status: 405 });
					}

					const body = (await req.json()) as JSONRPCRequest;
					const response = handleRequest(body, tools);
					const headers = new Headers({
						"Content-Type": "application/json",
						"Mcp-Session-Id": `ext-${name}`,
					});
					return new Response(JSON.stringify(response), { headers });
				},
			});

			// Signal the Go backend with our port
			console.log(`PUX_EXT_PORT:${server.port}`);
		},
	};
}

function handleRequest(req: JSONRPCRequest, tools: Map<string, ToolDef>): JSONRPCResponse {
	const id = req.id ?? 0;

	switch (req.method) {
		case "initialize":
			return {
				jsonrpc: "2.0",
				id,
				result: {
					protocolVersion: "2025-03-26",
					capabilities: { tools: {} },
					serverInfo: { name: "pux-extension", version: "0.1.0" },
				},
			};

		case "notifications/initialized":
			// Fire-and-forget notification, no response needed
			// But we return one anyway since our transport is synchronous
			return { jsonrpc: "2.0", id, result: {} };

		case "tools/list":
			return {
				jsonrpc: "2.0",
				id,
				result: {
					tools: Array.from(tools.values()).map((t) => ({
						name: t.name,
						description: t.description,
						inputSchema: t.parameters,
					})),
				},
			};

		case "tools/call": {
			const toolName = req.params?.name as string;
			const args = req.params?.arguments ?? {};

			const tool = tools.get(toolName);
			if (!tool) {
				return {
					jsonrpc: "2.0",
					id,
					error: { code: -32601, message: `Unknown tool: ${toolName}` },
				};
			}

			// Run handler asynchronously but we're in sync fetch
			// Bun's fetch is async so this is fine
			return tool
				.handler(args)
				.then((result) => ({
					jsonrpc: "2.0" as const,
					id,
					result,
				}))
				.catch((err) => ({
					jsonrpc: "2.0" as const,
					id,
					error: { code: -32000, message: String(err.message ?? err) },
				})) as any;
		}

		default:
			return {
				jsonrpc: "2.0",
				id,
				error: { code: -32601, message: `Unknown method: ${req.method}` },
			};
	}
}
