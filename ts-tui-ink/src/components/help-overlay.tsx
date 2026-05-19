import React, { useCallback } from "react";
import { Box, Text, useInput } from "ink";
import { usePuxStore } from "@pux/shared";
import { getCommands, type Command } from "../commands.js";
import { useColors, symbols } from "../theme.js";
import { useTerminalSize } from "../use-terminal-size.js";

// ── Command groups for organized display ──

const GROUPS = [
	{
		label: "General",
		names: ["help", "quit", "clear", "compact", "new", "status", "history"],
	},
	{
		label: "Panels",
		names: ["search", "logs", "sessions", "settings", "mcp", "model"],
	},
	{
		label: "Views",
		names: ["chat", "agents", "tools", "files", "conversations"],
	},
];

// ── Shared command row ──

export function CommandRow({
	name,
	description,
	selected,
	width,
}: {
	name: string;
	description: string;
	selected?: boolean;
	width?: number;
}) {
	const colors = useColors();
	const paddedName = `/${name}`.padEnd(width ?? 14);
	return (
		<Text backgroundColor={selected ? "gray" : undefined}>
			{"  "}
			<Text bold={selected} color={selected ? colors.brand : undefined}>
				{paddedName}
			</Text>
			<Text dimColor={!selected}> {symbols.dot} {description}</Text>
		</Text>
	);
}

// ── Help overlay ──

export function HelpOverlay() {
	const show = usePuxStore((s) => s.showHelpOverlay);
	const closeHelp = usePuxStore((s) => s.closeHelpOverlay);
	const { cols } = useTerminalSize();
	const colors = useColors();

	useInput(
		useCallback(
			(_input: string, key: any) => {
				if (!show) return;
				if (key.escape || key.return) closeHelp();
			},
			[show, closeHelp],
		),
	);

	if (!show) return null;

	const allCommands = getCommands();
	const cmdMap = new Map(allCommands.map((c) => [c.name, c]));

	return (
		<Box flexDirection="column" flexGrow={1}>
			<Box paddingX={1}>
				<Text backgroundColor="cyan" bold> Commands </Text>
			</Box>
			<Text color="cyan">{"═".repeat(cols)}</Text>

			<Box flexDirection="column" paddingX={2} flexGrow={1}>
				{GROUPS.map((group) => {
					const cmds = group.names
						.map((n) => cmdMap.get(n))
						.filter(Boolean) as Command[];
					if (cmds.length === 0) return null;
					return (
						<Box key={group.label} flexDirection="column" marginTop={group !== GROUPS[0] ? 1 : 0}>
							<Text bold color={colors.brand}>
								{group.label}
							</Text>
							{cmds.map((c) => (
								<CommandRow key={c.name} name={c.name} description={c.description} />
							))}
						</Box>
					);
				})}
			</Box>

			<Text color="cyan">{"═".repeat(cols)}</Text>
			<Box paddingX={2}>
				<Text dimColor>Esc to close</Text>
			</Box>
		</Box>
	);
}
