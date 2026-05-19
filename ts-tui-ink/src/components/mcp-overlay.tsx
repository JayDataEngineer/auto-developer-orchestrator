import React, { useState, useCallback, useMemo } from "react";
import { Box, Text, useInput, type Key } from "ink";
import TextInput from "ink-text-input";
import { usePuxStore, type MCPServerInfo } from "@pux/shared";
import { useColors, symbols } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

type Screen = "list" | "detail" | "add";

export function MCPOverlay() {
	// ALL hooks before conditional returns
	const show = usePuxStore((s) => s.showMCPOverlay);
	const servers = usePuxStore((s) => s.mcpServers);
	const closeMCPOverlay = usePuxStore((s) => s.closeMCPOverlay);
	const addMCPServer = usePuxStore((s) => s.addMCPServer);
	const removeMCPServer = usePuxStore((s) => s.removeMCPServer);

	const [screen, setScreen] = useState<Screen>("list");
	const [selectedIdx, setSelectedIdx] = useState(0);
	const [expandedServer, setExpandedServer] = useState<string | null>(null);

	// Add form state
	const [addPrefix, setAddPrefix] = useState("");
	const [addEndpoint, setAddEndpoint] = useState("");
	const [addField, setAddField] = useState(0); // 0=prefix, 1=endpoint
	const [addError, setAddError] = useState<string | null>(null);

	const colors = useColors();
	const { rows, cols } = useTerminalSize();

	const maxVisible = rows - 8;

	const expanded = useMemo(
		() => expandedServer ? servers.find((s) => s.prefix === expandedServer) ?? null : null,
		[expandedServer, servers],
	);

	// Build list items: servers + "add server" row
	const listItems = useMemo(() => {
		const items: Array<{ type: "server"; server: MCPServerInfo } | { type: "add" }> = [];
		for (const server of servers) {
			items.push({ type: "server", server });
		}
		items.push({ type: "add" });
		return items;
	}, [servers]);

	// Keyboard handler
	useInput(
		useCallback(
			(_input: string, key: Key) => {
				if (!show) return;

				if (key.escape) {
					if (screen === "add") { setScreen("list"); setAddError(null); return; }
					if (screen === "detail") {
						const sIdx = servers.findIndex((s) => s.prefix === expandedServer);
						setExpandedServer(null);
						setScreen("list");
						setSelectedIdx(sIdx >= 0 ? sIdx : 0);
						return;
					}
					closeMCPOverlay();
					return;
				}

				// Screen-specific handlers
				if (screen === "list") {
					if (key.upArrow) {
						setSelectedIdx((i) => Math.max(0, i - 1));
						return;
					}
					if (key.downArrow) {
						setSelectedIdx((i) => Math.min(listItems.length - 1, i + 1));
						return;
					}
					if (_input === "a" || _input === "A") {
						setScreen("add");
						setAddPrefix("");
						setAddEndpoint("");
						setAddField(0);
						setAddError(null);
						return;
					}
					if (_input === "d" || _input === "D") {
						const item = listItems[Math.min(selectedIdx, listItems.length - 1)];
						if (item && item.type === "server") {
							removeMCPServer(item.server.prefix);
							if (selectedIdx >= listItems.length - 1) {
								setSelectedIdx(Math.max(0, selectedIdx - 1));
							}
						}
						return;
					}
					if (key.return) {
						const item = listItems[Math.min(selectedIdx, listItems.length - 1)];
						if (!item) return;
						if (item.type === "add") {
							setScreen("add");
							setAddPrefix("");
							setAddEndpoint("");
							setAddField(0);
							setAddError(null);
						} else {
							setExpandedServer(item.server.prefix);
							setScreen("detail");
							setSelectedIdx(0);
						}
					}
				}

				if (screen === "detail") {
					if (key.upArrow) {
						setSelectedIdx((i) => Math.max(0, i - 1));
						return;
					}
					if (key.downArrow) {
						const tools = expanded?.tools ?? [];
						setSelectedIdx((i) => Math.min(tools.length, i + 1));
						return;
					}
				}

				if (screen === "add") {
					if (key.tab) {
						setAddField((f) => (f + 1) % 2);
						return;
					}
					if (key.return) {
						const prefix = addPrefix.trim();
						const endpoint = addEndpoint.trim();
						if (!prefix) { setAddError("Prefix is required"); return; }
						if (!endpoint) { setAddError("Endpoint URL is required"); return; }
						// Check for duplicate prefix
						if (servers.some((s) => s.prefix === prefix)) {
							setAddError("Prefix already exists");
							return;
						}
						addMCPServer(prefix, endpoint).then(() => {
							setScreen("list");
							setSelectedIdx(0);
							setAddError(null);
						});
						return;
					}
				}
			},
			[show, screen, selectedIdx, listItems, expandedServer, expanded, servers, closeMCPOverlay, addMCPServer, removeMCPServer, addPrefix, addEndpoint],
		),
	);

	// NOW safe to return conditionally
	if (!show) return null;

	// ── Screen A: Server List ──
	if (screen === "list") {
		const scrollOffset = Math.max(0, selectedIdx - maxVisible + 3);
		const visible = listItems.slice(scrollOffset, scrollOffset + maxVisible);

		return (
			<Box flexDirection="column" flexGrow={1}>
				<Header cols={cols} label="MCP Servers" />
				<Box flexDirection="column" paddingX={2} flexGrow={1}>
					{visible.map((item, vi) => {
						const globalIdx = scrollOffset + vi;
						const isSelected = globalIdx === selectedIdx;
						if (item.type === "add") {
							return <AddServerRow key="add" isSelected={isSelected} />;
						}
						return (
							<ServerRow
								key={item.server.prefix}
								server={item.server}
								isSelected={isSelected}
								cols={cols}
							/>
						);
					})}
				</Box>
				<Footer cols={cols} hint="↑↓ navigate · Enter expand · a add · d delete · Esc close" />
			</Box>
		);
	}

	// ── Screen B: Server Detail ──
	if (screen === "detail" && expandedServer) {
		if (!expanded) {
			return (
				<Box flexDirection="column" flexGrow={1}>
					<Header cols={cols} label="MCP Servers" />
					<Box paddingX={2} flexGrow={1}>
						<Text dimColor>Server not found.</Text>
					</Box>
					<Footer cols={cols} hint="Esc back" />
				</Box>
			);
		}

		const tools = expanded.tools ?? [];
		const scrollOffset = Math.max(0, selectedIdx - maxVisible + 2);
		const visibleTools = tools.slice(scrollOffset, scrollOffset + maxVisible - 2);

		return (
			<Box flexDirection="column" flexGrow={1}>
				<Header cols={cols} label={`MCP: ${expanded.prefix}`} />
				<Box flexDirection="column" paddingX={2} flexGrow={1}>
					<Box flexDirection="column">
						<Box>
							<Text color={expanded.available ? "green" : "red"} bold>
								{expanded.available ? symbols.toolDone : symbols.toolError}
							</Text>
							<Text> </Text>
							<Text bold>{expanded.prefix}</Text>
							<Text dimColor>  {expanded.endpoint}</Text>
						</Box>
						<Text dimColor>
							{expanded.toolCount} tool{expanded.toolCount !== 1 ? "s" : ""}
							{expanded.available ? " · available" : " · unavailable"}
						</Text>
					</Box>
					{tools.length > 0 && (
						<Box flexDirection="column" marginTop={1}>
							<Text bold underline>Tools:</Text>
							{visibleTools.map((tool, vi) => {
								const globalIdx = scrollOffset + vi;
								const isSelected = globalIdx === selectedIdx;
								return (
									<Text key={tool} backgroundColor={isSelected ? "gray" : undefined} bold={isSelected}>
										{"   "} {tool}
									</Text>
								);
							})}
							{tools.length > maxVisible - 2 && (
								<Text dimColor>... and {tools.length - maxVisible + 2} more</Text>
							)}
						</Box>
					)}
					{tools.length === 0 && (
						<Box marginTop={1}>
							<Text dimColor>No tools registered for this server.</Text>
						</Box>
					)}
				</Box>
				<Footer cols={cols} hint="↑↓ navigate · Esc back" />
			</Box>
		);
	}

	// ── Screen C: Add Server Form ──
	if (screen === "add") {
		const fields = ["Prefix", "Endpoint"];
		const values = [addPrefix, addEndpoint];
		const setters = [setAddPrefix, setAddEndpoint];

		return (
			<Box flexDirection="column" flexGrow={1}>
				<Header cols={cols} label="Add MCP Server" />
				<Box flexDirection="column" paddingX={2} flexGrow={1}>
					<Text dimColor color="gray">Connect to an MCP-compatible HTTP server.</Text>
					{fields.map((label, i) => (
						<Box key={label} marginTop={1}>
							<Box width={12}>
								<Text bold={addField === i} color={addField === i ? colors.brand : undefined}>
									{" "} {label}:
								</Text>
							</Box>
							<TextInput
								value={values[i]}
								onChange={setters[i]}
								focus={addField === i}
								placeholder={label === "Prefix" ? "my-server" : "http://localhost:8080/mcp"}
							/>
						</Box>
					))}
					{addError && (
						<Box marginTop={1}>
							<Text color={colors.error}>{symbols.toolError} {addError}</Text>
						</Box>
					)}
				</Box>
				<Footer cols={cols} hint="Tab next field · Enter confirm · Esc cancel" />
			</Box>
		);
	}

	return null;
}

// ── Sub-components ──

function ServerRow({ server, isSelected, cols }: { server: MCPServerInfo; isSelected: boolean; cols: number }) {
	const statusIcon = server.available ? "●" : "○";
	const statusColor = server.available ? "green" : "red";
	// Fixed: 2 (indent) + 2 (icon+space) + 14 (prefix) = 18
	const endpointMax = cols - 4 - 18 - 4; // 4 for tool count text

	return (
		<Text backgroundColor={isSelected ? "gray" : undefined}>
			{"  "}
			<Text color={statusColor}>{statusIcon} </Text>
			<Text bold={isSelected}>{server.prefix.padEnd(14)}</Text>
			<Text dimColor>{clip(server.endpoint, endpointMax)}</Text>
			<Text dimColor>  ({server.toolCount} tool{server.toolCount !== 1 ? "s" : ""})</Text>
			{!server.available && <Text color="red"> [offline]</Text>}
		</Text>
	);
}

function AddServerRow({ isSelected }: { isSelected: boolean }) {
	const colors = useColors();
	return (
		<Text backgroundColor={isSelected ? "gray" : undefined}>
			{"  "}
			<Text color={colors.brand} bold>+ Add server...</Text>
		</Text>
	);
}

function Header({ cols, label }: { cols: number; label: string }) {
	return (
		<Box flexDirection="column">
			<Box paddingX={1}>
				<Text backgroundColor="cyan" bold> {label} </Text>
			</Box>
			<Text color="cyan">{"═".repeat(cols)}</Text>
		</Box>
	);
}

function Footer({ cols, hint }: { cols: number; hint: string }) {
	return (
		<Box flexDirection="column">
			<Text color="cyan">{"═".repeat(cols)}</Text>
			<Box paddingX={2}>
				<Text dimColor color="gray">{hint}</Text>
			</Box>
		</Box>
	);
}

// ── Helpers ──

function clip(s: string, maxLen: number): string {
	if (maxLen <= 0) return "";
	if (s.length <= maxLen) return s;
	return maxLen <= 1 ? "…" : s.slice(0, maxLen - 1) + "…";
}
